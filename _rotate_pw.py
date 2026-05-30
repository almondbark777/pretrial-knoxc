"""
Rotate the Azure SQL admin password.

Generates a 32-char random password, updates the Azure SQL server admin
password, then updates App Service DB_PASSWORD setting and the local .env.
Password is NEVER printed to stdout or stderr.

Run from repo root.  Requires `az` on PATH and an authenticated session.
"""
from __future__ import annotations
import os
import secrets
import string
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent
ENV_PATH = REPO / "webapp" / ".env"

RG = "myself"
SQL_SERVER = "ptrknoxc"
WEBAPP = "knox-pretrial"


def gen_password() -> str:
    """32 chars, [A-Za-z0-9] + safe specials. Guarantees Azure SQL complexity."""
    safe_specials = "-_.!@#%+="  # nothing the shell or .env reader cares about
    alphabet = string.ascii_letters + string.digits + safe_specials
    while True:
        pw = "".join(secrets.choice(alphabet) for _ in range(32))
        if (
            any(c.islower() for c in pw)
            and any(c.isupper() for c in pw)
            and any(c.isdigit() for c in pw)
            and any(c in safe_specials for c in pw)
            and "abentley777" not in pw.lower()  # rule: cannot contain login name
        ):
            return pw


def run(args, **kw):
    """subprocess.run that fails loudly but never echoes the args to stdout."""
    proc = subprocess.run(args, capture_output=True, text=True, **kw)
    if proc.returncode != 0:
        # Redact: assume any string >= 16 chars in args is a secret; replace.
        safe_args = []
        for a in args:
            if isinstance(a, str) and len(a) >= 16 and any(c.isdigit() for c in a):
                safe_args.append("<REDACTED>")
            else:
                safe_args.append(a)
        sys.stderr.write(f"Command failed: {safe_args}\n")
        sys.stderr.write(f"stdout: {proc.stdout[-500:]}\n")
        sys.stderr.write(f"stderr: {proc.stderr[-500:]}\n")
        sys.exit(proc.returncode)
    return proc


def update_env_file(new_pw: str) -> None:
    lines = ENV_PATH.read_text(encoding="utf-8").splitlines()
    out = []
    found = False
    for line in lines:
        if line.startswith("DB_PASSWORD="):
            out.append(f"DB_PASSWORD={new_pw}")
            found = True
        else:
            out.append(line)
    if not found:
        out.append(f"DB_PASSWORD={new_pw}")
    ENV_PATH.write_text("\n".join(out) + "\n", encoding="utf-8")


def main() -> None:
    if not ENV_PATH.exists():
        sys.exit(f"Missing {ENV_PATH}")

    new_pw = gen_password()
    print(f"Generated new password (32 chars, complexity OK). Length: {len(new_pw)}")

    az = "/c/Program Files/Microsoft SDKs/Azure/CLI2/wbin/az.cmd"
    if not Path(az.replace("/c/", "C:/")).exists():
        az = "az"  # let PATH resolve

    print("Step 1/3: updating Azure SQL admin password...")
    run([az, "sql", "server", "update",
         "-g", RG, "-n", SQL_SERVER,
         "--admin-password", new_pw])

    print("Step 2/3: updating App Service DB_PASSWORD setting...")
    run([az, "webapp", "config", "appsettings", "set",
         "-g", RG, "-n", WEBAPP,
         "--settings", f"DB_PASSWORD={new_pw}",
         "-o", "none"])

    print("Step 3/3: updating local webapp/.env...")
    update_env_file(new_pw)

    print("DONE. Password is now set on Azure SQL, App Service, and webapp/.env.")
    print("App Service will restart on the appsettings change; expect ~30s of 503.")


if __name__ == "__main__":
    main()
