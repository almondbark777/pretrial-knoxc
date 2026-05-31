"""Unpack a Claude-artifact-bundle HTML file into its constituent assets.

Usage:
    python scripts/unpack.py <bundle.html> [out-dir]

Extracts every asset embedded in the <script type="__bundler/manifest"> tag,
decodes base64, decompresses gzip if needed, and writes each file as
    <uuid>.<ext>
in the output directory.  Also writes _manifest_summary.txt as an index.
"""
from __future__ import annotations
import base64
import gzip
import json
import re
import sys
from pathlib import Path

if len(sys.argv) < 2:
    sys.exit("Usage: python scripts/unpack.py <bundle.html> [out-dir]")

src = Path(sys.argv[1])
out = Path(sys.argv[2] if len(sys.argv) >= 3 else "assets")
out.mkdir(parents=True, exist_ok=True)

raw = src.read_text(encoding="utf-8")


def _extract_script(tag_type: str) -> str | None:
    m = re.search(
        rf'<script\s+type="__bundler/{tag_type}">(.*?)</script>',
        raw, re.DOTALL,
    )
    return m.group(1).strip() if m else None


manifest_text = _extract_script("manifest")
if not manifest_text:
    sys.exit(f"ERROR: No <script type=\"__bundler/manifest\"> found in {src}")

manifest: dict = json.loads(manifest_text)
print(f"Manifest entries: {len(manifest)}")

MIME_EXT = {
    "text/html":               "html",
    "text/css":                "css",
    "application/javascript":  "js",
    "text/javascript":         "js",
    "application/json":        "json",
    "image/png":               "png",
    "image/jpeg":              "jpg",
    "image/svg+xml":           "svg",
    "image/gif":               "gif",
    "image/webp":              "webp",
    "font/woff":               "woff",
    "font/woff2":              "woff2",
    "application/octet-stream": "bin",
}

summary_lines: list[str] = []
written: list[str] = []

for uuid, entry in manifest.items():
    mime = entry.get("mime", "application/octet-stream")
    ext  = MIME_EXT.get(mime, "bin")
    raw_bytes = base64.b64decode(entry.get("data", ""))
    if entry.get("compressed"):
        raw_bytes = gzip.decompress(raw_bytes)
    fname = f"{uuid}.{ext}"
    (out / fname).write_bytes(raw_bytes)
    written.append(fname)
    summary_lines.append(
        f"{uuid}  {mime:35s}  compressed={entry.get('compressed')}  "
        f"decoded_bytes={len(raw_bytes)}"
    )
    print(f"  wrote {fname}  ({len(raw_bytes):,} bytes)")

(out / "_manifest_summary.txt").write_text("\n".join(summary_lines), encoding="utf-8")
print(f"\nExtracted {len(written)} asset(s) to: {out}/")
print(f"Index: {out}/_manifest_summary.txt")
