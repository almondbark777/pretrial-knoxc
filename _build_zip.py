import zipfile
from pathlib import Path

src = Path("webapp")
exclude_dirs = {".venv", "__pycache__"}
exclude_files = {".env", ".env.example"}
out = Path("webapp.zip")
if out.exists():
    out.unlink()

count = 0
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zf:
    for p in src.rglob("*"):
        if p.is_dir():
            continue
        if any(part in exclude_dirs for part in p.parts):
            continue
        if p.name in exclude_files:
            continue
        rel = p.relative_to(src).as_posix()
        zf.write(p, arcname=rel)
        count += 1
print(f"wrote {count} files to {out}")
