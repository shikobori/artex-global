#!/usr/bin/env python3
"""Check whether external HTTP/HTTPS links in tracked Markdown still work.

The companion `check-doc-links.py` checks only internal links and anchors. It
intentionally excludes network-dependent checks from the merge gate because they
can be flaky. This script provides the separate periodic/manual external-link
check for references in the README, defense guides, and detections README:
incident reporting sites boho.or.kr, privacy.go.kr, pipc.go.kr, fsec.or.kr;
CISA KEV, OWASP, SigmaHQ, Suricata, MITRE, MISP; the original demo at
artex-demo.vercel.app; GitHub badges; and other reader-facing references.

URL selection rules:
- Scan all tracked `.md` files, skipping fenced code blocks (``` or ~~~) and
  inline code spans (`` `...` ``). URLs there are command examples, configuration
  values, or quoted external assets (e.g. `id.redhaze.top` in validation notes),
  not reference links for readers to follow.
- Collect HTTP/HTTPS URLs from Markdown links/images, autolinks (`<url>`), and
  bare URLs in the remaining prose.
- Exclude reserved/placeholder hosts: localhost; private/loopback IP ranges
  (127., 10., 192.168., 169.254., 172.16-31., 0.0.0.0, ::1); RFC 2606/6761
  reserved names (example.com/org/net/edu, `*.example.*`, `.test`, `.invalid`,
  `.local`, `.tld`); and bare names without a dot (e.g. `target`).

Availability checks (based on MAINTAINING.md section 8.2):
Korean public-sector/security sites often reject HEAD requests or default user
agents, or redirect repeatedly. Use GET with a browser User-Agent and follow
redirects to avoid misclassifying working links. Classify results as:
- OK: final status is 2xx/3xx; the link works.
- RESTRICTED: 401/403/405/429; the host is live but blocks this check through
  bot protection, method restrictions, or rate limits. Report without failing.
- DOWN: 404/410, persistent 5xx, or persistent DNS/connection/timeout/SSL errors;
  the link is likely broken.

Retry network errors, 5xx, and 429 with a short delay to distinguish transient
failures from outages. Uses only the standard library. Sends GET requests only
to references already linked by this repository's public documentation; it does
not scan or probe targets.

Known exceptions (allowlist):
Classify DOWN URLs in `scripts/external-links-allowlist.txt` as ALLOWED and
exclude them from strict failures. This records unfixable dead links inherited
from preserved upstream documents (e.g. CHANGELOG.zh.md), so recurring strict
checks fail on newly broken links instead of remaining permanently red.

Usage:
- Report only, exit code 0: `python3 -I scripts/check-external-links.py`
- Periodic/release check, not a merge gate: `--strict` exits 1 for any DOWN URL
  outside the allowlist. RESTRICTED and ALLOWED never fail, even in strict mode.
- Preview extracted URLs and sources without network requests: `--list`.
"""
import argparse
import http.client
import os
import re
import socket
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from urllib.parse import urlsplit

# [text](url) body links and ![alt](url) images. Stop paths at whitespace or closing parentheses.
MD_LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)")
MD_IMAGE = re.compile(r"!\[[^\]]*\]\(([^)\s]+)")
# <https://...> autolinks.
AUTOLINK = re.compile(r"<(https?://[^>\s]+)>")
# Bare URLs in prose. Strip trailing punctuation later.
BARE_URL = re.compile(r"https?://[^\s)>\]\"'`]+")
# Inline code spans `...` (single backticks). Replace with spaces before extraction.
INLINE_CODE = re.compile(r"`[^`]*`")
# Punctuation commonly trailing URLs in prose.
TRAILING_PUNCT = ".,;:!?\"'»)]}>"

# Private/loopback/link-local IPv4 ranges (172.16-31. uses a separate pattern).
PRIVATE_IPV4 = re.compile(r"^(127\.|10\.|192\.168\.|169\.254\.|0\.0\.0\.0$)")
PRIVATE_IPV4_172 = re.compile(r"^172\.(1[6-9]|2\d|3[01])\.")

BROWSER_UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"
)

# Status codes for live hosts whose server policy blocks this check (not broken links).
RESTRICTED_CODES = {401, 403, 405, 429}

ALLOWLIST_PATH = os.path.join("scripts", "external-links-allowlist.txt")


def load_allowlist(root: str) -> set:
    """Read known unfixable DOWN URLs, or return an empty set if the file is absent.

    One URL per line. Treat text after `#` as a comment; ignore blank/comment-only lines.
    """
    path = os.path.join(root, ALLOWLIST_PATH)
    allow = set()
    if not os.path.exists(path):
        return allow
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.split("#", 1)[0].strip()
            if line:
                allow.add(line)
    return allow


def strip_code(line: str) -> str:
    """Strip inline code from a line outside a fence; URLs inside code are not references."""
    return INLINE_CODE.sub(" ", line)


def is_checkable(url: str) -> bool:
    """Filter reserved/placeholder hosts, keeping external URLs worth checking."""
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https"):
        return False
    host = parts.hostname
    if not host:
        return False
    host = host.lower()
    if host == "localhost" or host.endswith(".localhost"):
        return False
    if host == "::1":
        return False
    if PRIVATE_IPV4.match(host) or PRIVATE_IPV4_172.match(host):
        return False
    if host in ("example.com", "example.org", "example.net", "example.edu"):
        return False
    if host.endswith((".example.com", ".example.org", ".example.net", ".example")):
        return False
    if host.endswith((".test", ".invalid", ".local", ".localdomain", ".tld")):
        return False
    if "." not in host:  # Bare name, not an FQDN (e.g. target)
        return False
    return True


def extract(root: str, md_files: list) -> dict:
    """Map external URLs in tracked .md files to [(file, line), ...] source locations."""
    found = defaultdict(list)
    for md in md_files:
        with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
            lines = fh.readlines()
        in_fence = False
        for lineno, raw in enumerate(lines, 1):
            stripped = raw.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            line = strip_code(raw)
            candidates = []
            for pattern in (MD_LINK, MD_IMAGE, AUTOLINK):
                candidates.extend(m.group(1) for m in pattern.finditer(line))
            for m in BARE_URL.finditer(line):
                candidates.append(m.group(0).rstrip(TRAILING_PUNCT))
            for url in candidates:
                url = url.strip().rstrip(TRAILING_PUNCT)
                if is_checkable(url):
                    where = (md, lineno)
                    if where not in found[url]:
                        found[url].append(where)
    return found


def probe(url: str, timeout: float, retries: int, delay: float):
    """GET a URL with a browser UA and return (classification, detail).

    Classifications are "OK", "RESTRICTED", and "DOWN". Retry network errors, 5xx, and 429.
    """
    req = urllib.request.Request(
        url,
        method="GET",
        headers={
            "User-Agent": BROWSER_UA,
            "Accept": "*/*",
            "Accept-Language": "ko,en;q=0.8",
        },
    )
    last = ""
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                code = resp.getcode()
                if code in RESTRICTED_CODES:
                    return "RESTRICTED", f"HTTP {code}"
                return "OK", f"HTTP {code}"
        except urllib.error.HTTPError as exc:
            code = exc.code
            if code in RESTRICTED_CODES:
                return "RESTRICTED", f"HTTP {code}"
            if code >= 500 or code == 408:
                last = f"HTTP {code}"  # Retry potentially transient failures
            else:
                return "DOWN", f"HTTP {code}"  # Definitive failures such as 404/410 need no retry
        except (
            urllib.error.URLError,
            http.client.HTTPException,
            ssl.SSLError,
            socket.timeout,
            ConnectionError,
            TimeoutError,
            OSError,
        ) as exc:
            reason = getattr(exc, "reason", exc)
            last = f"{type(exc).__name__}: {reason}"
        if attempt < retries:
            time.sleep(delay * (attempt + 1))
    return "DOWN", last


def main() -> int:
    ap = argparse.ArgumentParser(description="Check external link availability in tracked Markdown")
    ap.add_argument("--strict", action="store_true",
                    help="Exit 1 for any DOWN result (for periodic/release checks)")
    ap.add_argument("--list", action="store_true",
                    help="List URLs and sources without network requests")
    ap.add_argument("--timeout", type=float, default=15.0, help="Request timeout in seconds")
    ap.add_argument("--retries", type=int, default=2, help="Retry count for network errors, 5xx, and 429")
    ap.add_argument("--delay", type=float, default=0.5, help="Base delay between requests/retries in seconds")
    args = ap.parse_args()

    root = subprocess.check_output(
        ["git", "rev-parse", "--show-toplevel"], text=True
    ).strip()
    tracked = subprocess.check_output(["git", "ls-files"], cwd=root, text=True).splitlines()
    md_files = [f for f in tracked if f.endswith(".md")]

    allow = load_allowlist(root)
    found = extract(root, md_files)
    urls = sorted(found)
    print(f"Collected {len(urls)} external URLs from {len(md_files)} tracked Markdown files")

    if args.list:
        for url in urls:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            tag = " [allowlist]" if url in allow else ""
            print(f"  {url}{tag}  ({where})")
        return 0

    results = {"OK": [], "RESTRICTED": [], "ALLOWED": [], "DOWN": []}
    for i, url in enumerate(urls):
        if i:
            time.sleep(args.delay)  # Brief delay between requests to avoid sandbox throttling
        verdict, detail = probe(url, args.timeout, args.retries, args.delay)
        if verdict == "DOWN" and url in allow:
            verdict = "ALLOWED"  # Known unfixable DOWN link inherited from upstream
        results[verdict].append((url, detail))
        print(f"  [{verdict:10}] {url} — {detail}")

    print(
        f"\nSummary: OK {len(results['OK'])} · RESTRICTED {len(results['RESTRICTED'])} · "
        f"ALLOWED {len(results['ALLOWED'])} · DOWN {len(results['DOWN'])}"
    )
    if results["RESTRICTED"]:
        print("RESTRICTED (live host blocks this check; not a broken link):")
        for url, detail in results["RESTRICTED"]:
            print(f"  {url} — {detail}")
    if results["ALLOWED"]:
        print("ALLOWED (known unfixable DOWN links on the allowlist; excluded from strict failures):")
        for url, detail in results["ALLOWED"]:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            print(f"  {url} — {detail}  (source: {where})")
    if results["DOWN"]:
        print("DOWN (likely broken; investigate):")
        for url, detail in results["DOWN"]:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            print(f"  {url} — {detail}  (source: {where})")

    if args.strict and results["DOWN"]:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
