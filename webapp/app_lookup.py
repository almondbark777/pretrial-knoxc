"""
Knox County Pre-Trial Services — LOOKUP-ONLY deployment.

A trimmed FastAPI app that serves ONLY the bundled "PTR Client Lookup"
single-page app, fed live from Azure SQL (no CSV uploads). Same login as the
full app (allow-list + shared password); intended to sit behind a Cloudflare
Tunnel + Cloudflare Access.

Run:  uvicorn app_lookup:app --host 127.0.0.1 --port 8000

This deliberately exposes no other pages. To run the full multi-page app
instead, use app.py.
"""
from __future__ import annotations

import base64
import hashlib
import os
import secrets
from pathlib import Path

from dotenv import load_dotenv
from fastapi import FastAPI, Request
from fastapi.responses import FileResponse, HTMLResponse, JSONResponse, RedirectResponse, Response
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from starlette.middleware.sessions import SessionMiddleware

load_dotenv(Path(__file__).parent / ".env")

import queries
import queries_ext as qx
from users import ALLOWED_USERS

BASE = Path(__file__).parent
templates = Jinja2Templates(directory=str(BASE / "templates"))
LOOKUP_HTML = BASE / "lookup" / "PTR_Client_Lookup.html"

CACHE_TTL = float(os.getenv("CACHE_TTL_SECONDS", "60"))

app = FastAPI(title="Knox County Pre-Trial — Client Lookup")
app.mount("/static", StaticFiles(directory=str(BASE / "static")), name="static")

# ─── Auth (mirrors app.py) ──────────────────────────────────────────────────

_REALM = 'Basic realm="Knox County Pre-Trial Services"'
_EXPECTED_PW = os.getenv("APP_PASSWORD", "pretrialtestsite")
_SESSION_SECRET = os.getenv(
    "APP_SESSION_SECRET",
    hashlib.sha256(("kh-session::" + _EXPECTED_PW).encode()).hexdigest(),
)

_PUBLIC_PATHS = (
    "/health", "/favicon.ico", "/static/",
    "/login", "/api/login", "/api/logout",
)


def _check_basic_auth(request: Request) -> str | None:
    auth = request.headers.get("Authorization", "")
    if not auth.lower().startswith("basic "):
        return None
    try:
        decoded = base64.b64decode(auth.split(" ", 1)[1]).decode("utf-8", errors="replace")
        username, _, password = decoded.partition(":")
    except Exception:
        return None
    username_norm = username.strip().lower()
    if username_norm in ALLOWED_USERS and secrets.compare_digest(password, _EXPECTED_PW):
        return username_norm
    return None


def _wants_html(request: Request) -> bool:
    if request.url.path.startswith("/api/"):
        return False
    accept = request.headers.get("accept", "")
    return "text/html" in accept or accept == "*/*" or accept == ""


@app.middleware("http")
async def auth_middleware(request: Request, call_next):
    path = request.url.path
    if any(path == p or path.startswith(p) for p in _PUBLIC_PATHS):
        return await call_next(request)

    sess = request.scope.get("session") or {}
    sess_user = sess.get("user")
    if sess_user and sess_user in ALLOWED_USERS:
        request.state.user = sess_user
        return await call_next(request)

    basic_user = _check_basic_auth(request)
    if basic_user:
        request.state.user = basic_user
        return await call_next(request)

    if _wants_html(request):
        next_path = request.url.path
        if request.url.query:
            next_path += "?" + request.url.query
        return RedirectResponse(url=f"/login?next={next_path}", status_code=303)
    return Response(
        content='{"error":"Authentication required"}',
        status_code=401,
        headers={"WWW-Authenticate": _REALM, "Content-Type": "application/json"},
    )


# ─── Login / logout ─────────────────────────────────────────────────────────

@app.get("/login", response_class=HTMLResponse)
def login_page(request: Request, next: str = "/", err: str = ""):
    return templates.TemplateResponse(request, "login.html", {
        "next_path": next or "/",
        "error":     err,
    })


@app.post("/api/login", response_class=JSONResponse)
async def api_login(request: Request):
    ctype = request.headers.get("content-type", "")
    body = await request.json() if "application/json" in ctype else dict(await request.form())
    email = (body.get("email") or "").strip().lower()
    password = body.get("password") or ""
    nxt = body.get("next") or "/"
    if not (email in ALLOWED_USERS and secrets.compare_digest(password, _EXPECTED_PW)):
        return JSONResponse({"ok": False, "error": "Invalid email or password"}, status_code=401)
    request.session["user"] = email
    return JSONResponse({"ok": True, "redirect": nxt})


@app.post("/api/logout", response_class=JSONResponse)
def api_logout(request: Request):
    request.session.clear()
    return {"ok": True, "redirect": "/login"}


# ─── The lookup app + its data ──────────────────────────────────────────────

@app.get("/", response_class=HTMLResponse)
def lookup_home(request: Request):
    if not LOOKUP_HTML.exists():
        return HTMLResponse("<h1>Lookup app not installed</h1>"
                            "<p>Expected file at webapp/lookup/PTR_Client_Lookup.html</p>",
                            status_code=500)
    # no-store so officers always get the current build; data is fetched live anyway
    return FileResponse(str(LOOKUP_HTML), media_type="text/html",
                        headers={"Cache-Control": "no-store"})


@app.get("/api/lookup_data", response_class=JSONResponse)
def api_lookup_data():
    """The four datasets (bb/ci/pm/gp) the lookup app consumes, live from SQL."""
    return queries.cached("lookup_datasets", CACHE_TTL, qx.lookup_datasets)


@app.get("/api/refresh", response_class=JSONResponse)
def api_refresh():
    queries._cache.clear()
    return {"ok": True}


# ─── Health (auth-free, for tunnel/uptime probes) ───────────────────────────

@app.get("/health", response_class=JSONResponse)
def health():
    try:
        queries.get_conn().cursor().execute("SELECT 1")
        return {"ok": True, "db": "up"}
    except Exception as e:
        return JSONResponse({"ok": False, "error": str(e)}, status_code=503)


# Any other path → send back to the one page we serve.
@app.get("/{rest:path}", response_class=HTMLResponse)
def catch_all(rest: str):
    return RedirectResponse("/", status_code=307)


# ─── Session middleware MUST be added last (outermost). ─────────────────────
app.add_middleware(
    SessionMiddleware,
    secret_key=_SESSION_SECRET,
    session_cookie="kh_sess",
    max_age=60 * 60 * 12,
    same_site="lax",
    https_only=False,   # TLS terminated upstream by Cloudflare
)
