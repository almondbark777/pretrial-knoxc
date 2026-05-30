"""Extract assets from a Claude artifact bundle HTML file."""
from __future__ import annotations
import base64
import gzip
import json
import re
import sys
from pathlib import Path

if len(sys.argv) < 2:
    sys.exit("Usage: python _extract_bundle.py <path-to-bundle.html> [out-dir]")

src = Path(sys.argv[1])
out = Path(sys.argv[2] if len(sys.argv) >= 3 else "_bundle_out")
out.mkdir(exist_ok=True)

raw = src.read_text(encoding="utf-8")

def extract(tag_type: str) -> str | None:
    m = re.search(
        rf'<script\s+type="__bundler/{tag_type}">(.*?)</script>',
        raw, re.DOTALL,
    )
    return m.group(1) if m else None

manifest_text = extract("manifest")
template_text = extract("template")
ext_text = extract("ext_resources")

if not manifest_text or not template_text:
    sys.exit("Missing manifest or template script tag")

manifest = json.loads(manifest_text)
template = json.loads(template_text)
print(f"Manifest entries: {len(manifest)}")
print(f"Template type: {type(template).__name__}")

# Save the template (this is usually the actual page structure)
(out / "_template.json").write_text(json.dumps(template, indent=2), encoding="utf-8")

if ext_text:
    (out / "_ext_resources.json").write_text(ext_text, encoding="utf-8")

# Save each asset. Try to give it a sensible name from mime type.
mime_ext = {
    "text/html": "html", "text/css": "css", "application/javascript": "js",
    "text/javascript": "js", "application/json": "json", "image/png": "png",
    "image/jpeg": "jpg", "image/svg+xml": "svg", "image/gif": "gif",
    "image/webp": "webp", "font/woff": "woff", "font/woff2": "woff2",
    "application/octet-stream": "bin",
}
for uuid, entry in manifest.items():
    mime = entry.get("mime", "application/octet-stream")
    ext = mime_ext.get(mime, "bin")
    data_b64 = entry.get("data", "")
    raw_bytes = base64.b64decode(data_b64)
    if entry.get("compressed"):
        raw_bytes = gzip.decompress(raw_bytes)
    fname = f"{uuid}.{ext}"
    (out / fname).write_bytes(raw_bytes)

# Print a manifest summary
summary_lines = []
for uuid, entry in manifest.items():
    summary_lines.append(f"{uuid}  {entry.get('mime'):30s}  compressed={entry.get('compressed')}  size={len(entry.get('data',''))}")
(out / "_manifest_summary.txt").write_text("\n".join(summary_lines), encoding="utf-8")
print(f"Extracted to: {out}")
print(f"See {out}/_manifest_summary.txt for index")
