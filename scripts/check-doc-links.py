#!/usr/bin/env python3
"""Check internal links, image references, and heading anchors in repository Markdown.

Scan all tracked `.md` documents and verify two properties:

1. Relative links (`[text](path)`) and images (`![alt](path)` or `<img src="path">`)
   point to existing files in the repository.
2. Anchor links (`[text](#heading)` or `[text](other.md#heading)`) refer to actual
   headings in the target `.md` document. Reproduce GitHub's slug rules using the
   standard library: lowercase, retain only `\\p{Word}`, hyphens, and spaces,
   replace spaces with hyphens, and append `-1`, `-2`, etc. for duplicate slugs.

Exit with code 1 if any reference is broken, for use as the CI merge gate
(`.github/workflows/docs.yml`) or as a contributor's local check.

Excluded from this check:
- External URLs (`http://`, `https://`, `mailto:`, `tel:`, `data:`): network-dependent
  checks can be flaky and run separately from this deterministic gate.
- Fragments after `#` in links to non-`.md` files (e.g. source line numbers):
  check only file existence, since these are not heading anchors.
- Image fragments: heading anchors do not apply to images.
- Example links and heading-like comments in fenced code blocks (``` or ~~~):
  these are code examples, not actual references or headings.

Uses only the standard library and no network.
Run: `python3 -I scripts/check-doc-links.py`
"""
import os
import re
import subprocess
import sys
import unicodedata
from collections import defaultdict

# [text](path) links, excluding images (`![...]`). Stop paths at whitespace (exclude titles).
MD_LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)")
# ![alt](path) image links.
MD_IMAGE = re.compile(r"!\[[^\]]*\]\(([^)\s]+)")
# <img ... src="path" ...> HTML images.
HTML_IMAGE = re.compile(r"<img[^>]*\bsrc=[\"']([^\"']+)[\"']", re.IGNORECASE)

# Network/non-file schemes. Exclude anchors (`#`), which reference the same document.
EXTERNAL_PREFIXES = ("http://", "https://", "mailto:", "tel:", "data:")

ATX_HEADING = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")
# Strip inline Markdown from heading text, leaving plain text.
MD_INLINE_LINK = re.compile(r"\[([^\]]*)\]\([^)]*\)")


def is_external(target: str) -> bool:
    return target.startswith(EXTERNAL_PREFIXES)


def _is_word_char(ch: str) -> bool:
    """Check whether a character belongs to GitHub's retained `\\p{Word}` set.

    Ruby regex `\\p{Word}` = Letter(L*) + Mark(M*) + Decimal_Number(Nd)
    + Connector_Punctuation(Pc) + Join_Control(U+200C, U+200D). This preserves
    Hangul, Han characters, underscores, and emoji variation selectors (Mn).
    """
    if ch == "_":
        return True
    if ch in ("‌", "‍"):
        return True
    category = unicodedata.category(ch)
    if category[0] in ("L", "M"):
        return True
    return category in ("Nd", "Pc")


def slugify(text: str) -> str:
    """Convert heading text to a GitHub anchor slug, excluding duplicate suffixes."""
    text = text.strip().lower()
    out = []
    for ch in text:
        if ch == " ":
            out.append("-")
        elif ch == "-" or _is_word_char(ch):
            out.append(ch)
    return "".join(out)


def heading_slugs(root: str, md: str) -> set:
    """Build the set of GitHub anchor slugs for all ATX headings in a file.

    Append `-1`, `-2`, etc. to duplicate slugs in occurrence order, as GitHub does.
    """
    slugs = set()
    counts = defaultdict(int)
    in_fence = False
    with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
        for line in fh:
            stripped = line.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            match = ATX_HEADING.match(line.rstrip("\n"))
            if not match:
                continue
            raw = match.group(2)
            raw = MD_INLINE_LINK.sub(r"\1", raw)
            raw = raw.replace("`", "").replace("*", "").replace("_", "")
            base = slugify(raw)
            seen = counts[base]
            counts[base] += 1
            slugs.add(base if seen == 0 else f"{base}-{seen}")
    return slugs


def main() -> int:
    root = subprocess.check_output(
        ["git", "rev-parse", "--show-toplevel"], text=True
    ).strip()
    tracked = subprocess.check_output(
        ["git", "ls-files"], cwd=root, text=True
    ).splitlines()
    md_files = [f for f in tracked if f.endswith(".md")]

    slug_cache: dict = {}

    def slugs_of(md_path: str) -> set:
        if md_path not in slug_cache:
            slug_cache[md_path] = heading_slugs(root, md_path)
        return slug_cache[md_path]

    checked_files = 0
    checked_anchors = 0
    broken_files = []
    broken_anchors = []
    for md in md_files:
        with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
            lines = fh.readlines()
        base_dir = os.path.dirname(md)
        in_fence = False
        for lineno, line in enumerate(lines, 1):
            stripped = line.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            for pattern in (MD_LINK, MD_IMAGE, HTML_IMAGE):
                for match in pattern.finditer(line):
                    target = match.group(1).strip()
                    if is_external(target):
                        continue

                    if target.startswith("#"):
                        file_part, anchor = "", target[1:]
                    elif "#" in target:
                        file_part, anchor = target.split("#", 1)
                    else:
                        file_part, anchor = target, ""
                    file_part = file_part.split("?", 1)[0]
                    anchor = anchor.strip()

                    # Resolve the target .md document for anchor checks; other file links
                    # target their file, while a bare `#anchor` targets the current document.
                    target_md = None
                    if file_part:
                        checked_files += 1
                        resolved = os.path.normpath(
                            os.path.join(root, base_dir, file_part)
                        )
                        if not os.path.exists(resolved):
                            broken_files.append((md, lineno, target))
                            continue
                        if file_part.endswith(".md"):
                            target_md = os.path.normpath(
                                os.path.join(base_dir, file_part)
                            )
                    else:
                        target_md = md

                    # Anchors apply only to body links (MD_LINK). Skip fragments
                    # after `#` in image references.
                    if anchor and target_md is not None and pattern is MD_LINK:
                        checked_anchors += 1
                        if anchor not in slugs_of(target_md):
                            broken_anchors.append((md, lineno, target, target_md))

    print(
        f"Checked {len(md_files)} tracked Markdown files · {checked_files} file references · "
        f"{checked_anchors} anchor references"
    )
    if broken_files:
        print(f"{len(broken_files)} broken file references — target files are missing from the repository:")
        for md, lineno, target in broken_files:
            print(f"  {md}:{lineno} -> {target}")
    if broken_anchors:
        print(f"{len(broken_anchors)} broken anchor references — target documents have no matching heading:")
        for md, lineno, target, target_md in broken_anchors:
            print(f"  {md}:{lineno} -> {target}  (target: {target_md})")
    if broken_files or broken_anchors:
        return 1
    print("0 broken references — all internal links, images, and anchors resolve to existing targets.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
