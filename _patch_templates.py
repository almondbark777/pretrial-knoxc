"""Inject shared theme.css + site-chrome.js into every template.

Idempotent: re-running won't duplicate the tags."""
from __future__ import annotations
from pathlib import Path

TPL_DIR = Path("webapp/templates")

INJECT = (
    '\n  <!-- KH-THEME-START — shared chrome injected by _patch_templates.py -->\n'
    '  <link rel="preconnect" href="https://fonts.googleapis.com">\n'
    '  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>\n'
    '  <link href="https://fonts.googleapis.com/css2?'
    'family=Inter+Tight:wght@300;400;500;600;700'
    '&family=Fraunces:ital,wght@0,400;0,500;1,400;1,500'
    '&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">\n'
    '  <link rel="stylesheet" href="/static/theme.css">\n'
    '  <script defer src="/static/site-chrome.js"></script>\n'
    '  <!-- KH-THEME-END -->\n'
)

START = "<!-- KH-THEME-START"
END   = "<!-- KH-THEME-END -->"


def patch(path: Path) -> bool:
    src = path.read_text(encoding="utf-8")
    if START in src:
        # Replace existing block (re-runs)
        before, _, rest = src.partition(START)
        _, _, after = rest.partition(END)
        new = before.rstrip() + INJECT.lstrip() + after.lstrip("\n")
    else:
        # Inject just before </head>
        idx = src.lower().rfind("</head>")
        if idx < 0:
            print(f"  SKIP (no </head>): {path.name}")
            return False
        new = src[:idx] + INJECT + src[idx:]
    path.write_text(new, encoding="utf-8")
    return True


count = 0
for f in sorted(TPL_DIR.glob("*.html")):
    if patch(f):
        count += 1
        print(f"  patched: {f.name}")
print(f"\nDone. {count} templates updated.")
