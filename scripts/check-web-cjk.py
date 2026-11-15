#!/usr/bin/env python3
"""Check exported web UI HTML (`web/out`) for leaked Chinese (Han) characters.

This fork's user-facing UI should contain no unintended Chinese text (the zero-Han
property established for `out` HTML in backlog B4). Manual review alone can miss
hardcoded strings or missing translations. This script catches those regressions
at the merge gate.

How it works
------------
Scan the raw contents of every `.html` file under `web/out`, including inline
scripts and React Server Components flight data, and exit 1 if any Han character
is present. User-facing text can arrive in JSON-like flight data within the HTML;
stripping tags alone would miss that path.

Limitations
-----------
This gate checks only static export artifacts. It cannot inspect content rendered
in the browser at runtime in MOCK mode (`NEXT_PUBLIC_MOCK=1`, used by the public
demo). Data in `web/src/lib/mock/data.ts`, `handler.ts`, and hardcoded page samples
is fetched by `mockHandle()` through `useEffect` after hydration and rendered from
React state. It exists in runtime JS bundles, not `out` HTML. Chinese text along
that path passes this gate; backlog G114 fixed such sample logs in `/system/logs`
by translating them into Korean.

Source review covers that surface. Chinese retained in rendered `.tsx` files or
client data under `web/src` must be an intentional language/protocol resource:
an unused i18n fallback protected by `t.has(...)`, a backend wire-format token
(chat mention category labels or the model decision-source sentinel), a literal
shown only in the `zh` locale, or a preserved mirror of the agent's section [A]
reasoning prompt. Translate any ordinary user-facing text instead. Backlog G115
originally audited these categories.

Allowed and detected ranges
---------------------------
- Hangul is allowed: its Unicode blocks do not overlap Han blocks.
- Detect only CJK Han characters: unified ideographs and Extension A
  (U+3400-U+9FFF), compatibility ideographs (U+F900-U+FAFF), and supplementary
  extensions B onward (U+20000-U+2FFFF). Kana and full-width punctuation are
  excluded to avoid false positives in preserved code or quoted evidence.

Intentional upstream characters
------------------------------
The allowlist is empty because the checked `out` HTML contains no Han characters.
If an original Chinese label must deliberately remain (e.g. a language switcher),
add its characters to `ALLOWED_HAN` and explain why in a comment, keeping
exceptions narrow and the gate strict.

Uses only the standard library and no network.
Run: `python3 -I scripts/check-web-cjk.py [out directory]`
Defaults to `web/out` at the repository root. CI runs
`python3 -I ../scripts/check-web-cjk.py` from the `web` working directory.
"""
import os
import re
import subprocess
import sys

# Only CJK Han blocks; exclude Hangul (AC00-D7A3), kana, and full-width punctuation.
# Ranges: Extension A + unified ideographs (U+3400-U+9FFF), compatibility ideographs (U+F900-U+FAFF),
#         supplementary extensions B onward (U+20000-U+2FFFF).
HAN = re.compile(r"[㐀-鿿豈-﫿\U00020000-\U0002ffff]")

# Han characters intentionally shown in the UI (e.g. a Chinese language switcher label).
# Empty means every Han character fails the gate. Document each exception and its reason.
# Example: ALLOWED_HAN = {"中", "文"}  # Original Chinese language switcher label
ALLOWED_HAN: set = set()


def find_out_dir() -> str:
    """Resolve the `out` directory to check.

    Use the supplied path, or default to `web/out` under the Git repository root.
    """
    if len(sys.argv) > 1:
        return os.path.abspath(sys.argv[1])
    root = subprocess.check_output(
        ["git", "rev-parse", "--show-toplevel"], text=True
    ).strip()
    return os.path.join(root, "web", "out")


def main() -> int:
    out_dir = find_out_dir()
    if not os.path.isdir(out_dir):
        print(
            f"Error: directory to check does not exist — {out_dir}\n"
            "Run `npm run build:static` to create the static export first."
        )
        return 2

    checked = 0
    offenders = []  # (relative path, Han character count, preview)
    for dirpath, _dirnames, filenames in os.walk(out_dir):
        for name in filenames:
            if not name.endswith(".html"):
                continue
            checked += 1
            path = os.path.join(dirpath, name)
            with open(path, encoding="utf-8", errors="ignore") as fh:
                text = fh.read()
            found = [ch for ch in HAN.findall(text) if ch not in ALLOWED_HAN]
            if found:
                rel = os.path.relpath(path, out_dir)
                kinds = "".join(sorted(set(found)))
                offenders.append((rel, len(found), kinds))

    print(f"Checked {checked} HTML files (directory: {out_dir})")
    if offenders:
        print(f"Chinese (Han) character leaks in {len(offenders)} files — user-facing HTML contains Han characters:")
        for rel, count, kinds in offenders:
            preview = kinds if len(kinds) <= 30 else kinds[:30] + "…"
            print(f"  out/{rel}: {count} Han characters · distinct characters: {preview}")
        print(
            "\nTranslate the text, or if these are intentional Han characters that must be preserved, "
            "add them to ALLOWED_HAN in scripts/check-web-cjk.py with a reason."
        )
        return 1
    if checked == 0:
        print("Warning: no HTML files checked — verify that the build completed correctly.")
        return 2
    print("0 Chinese (Han) character leaks — user-facing HTML contains no Han characters.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
