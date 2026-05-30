"""
Knox County Pre-Trial Services — FastAPI web app.

Authentication: a session-cookie login at /login (preferred for browsers)
plus HTTP Basic auth as a fallback (preferred for curl / scripts /
uptime monitors). Both validate against the same allow-list of usernames
in users.py and the same shared APP_PASSWORD env var.
"""
from __future__ import annotations

import base64
import hashlib
import os
import secrets
from pathlib import Path

from dotenv import load_dotenv
from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse, JSONResponse, RedirectResponse, Response
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from starlette.middleware.sessions import SessionMiddleware

load_dotenv(Path(__file__).parent / ".env")

import queries
import queries_ext as qx
from users import ALLOWED_USERS


def _user(request: Request) -> str:
    """Convenience accessor for the authenticated user (set by auth middleware)."""
    return getattr(request.state, "user", "") or ""

BASE = Path(__file__).parent
templates = Jinja2Templates(directory=str(BASE / "templates"))

app = FastAPI(title="Knox County Pre-Trial Services")
app.mount("/static", StaticFiles(directory=str(BASE / "static")), name="static")

CACHE_TTL = float(os.getenv("CACHE_TTL_SECONDS", "60"))

# ─── Auth ─────────────────────────────────────────────────────────────────

_REALM = 'Basic realm="Knox County Pre-Trial Services"'
_EXPECTED_PW = os.getenv("APP_PASSWORD", "pretrialtestsite")

# Session secret for signing the cookie. Use APP_SESSION_SECRET if set;
# otherwise derive a deterministic value from APP_PASSWORD so existing
# sessions survive restarts as long as the password doesn't rotate.
_SESSION_SECRET = os.getenv(
    "APP_SESSION_SECRET",
    hashlib.sha256(("kh-session::" + _EXPECTED_PW).encode()).hexdigest(),
)
# NOTE: SessionMiddleware is added AT THE BOTTOM OF THIS FILE (after the
# auth middleware decorator) so it wraps the auth middleware. In FastAPI,
# the LAST-added middleware is the OUTERMOST layer.

# Paths that skip auth entirely (health probes, static, login page itself).
_PUBLIC_PATHS = (
    "/health", "/static/", "/favicon.ico",
    "/login", "/api/login", "/api/logout",
    "/static/manifest.json",
)


def _check_basic_auth(request: Request) -> str | None:
    """If the request has a valid HTTP Basic Auth header, return the
    normalized username; otherwise return None."""
    auth = request.headers.get("Authorization", "")
    if not auth.lower().startswith("basic "):
        return None
    try:
        decoded = base64.b64decode(auth.split(" ", 1)[1]).decode("utf-8", errors="replace")
        username, _, password = decoded.partition(":")
    except Exception:
        return None
    username_norm = username.strip().lower()
    user_ok = username_norm in ALLOWED_USERS
    pw_ok   = secrets.compare_digest(password, _EXPECTED_PW)
    if user_ok and pw_ok:
        return username_norm
    return None


def _wants_html(request: Request) -> bool:
    """Best-effort: should we redirect (HTML page) or 401 (API call)?"""
    if request.url.path.startswith("/api/"):
        return False
    accept = request.headers.get("accept", "")
    return "text/html" in accept or accept == "*/*" or accept == ""


@app.middleware("http")
async def auth_middleware(request: Request, call_next):
    path = request.url.path
    if any(path == p or path.startswith(p) for p in _PUBLIC_PATHS):
        return await call_next(request)

    # Try cookie session first (browser flow).
    # Read from scope directly — BaseHTTPMiddleware doesn't propagate
    # request.session through to the wrapped Request object.
    sess = request.scope.get("session") or {}
    sess_user = sess.get("user")
    if sess_user and sess_user in ALLOWED_USERS:
        request.state.user = sess_user
        return await call_next(request)

    # Fall back to HTTP Basic for API clients / scripts / uptime monitors.
    basic_user = _check_basic_auth(request)
    if basic_user:
        request.state.user = basic_user
        return await call_next(request)

    # Not authenticated.
    if _wants_html(request):
        # Preserve where they were trying to go so we can send them back after login.
        next_path = request.url.path
        if request.url.query:
            next_path += "?" + request.url.query
        return RedirectResponse(
            url=f"/login?next={next_path}",
            status_code=303,
        )
    return Response(
        content='{"error":"Authentication required"}',
        status_code=401,
        headers={"WWW-Authenticate": _REALM, "Content-Type": "application/json"},
    )


# ─── Login page + login/logout endpoints ──────────────────────────────────

@app.get("/login", response_class=HTMLResponse)
def login_page(request: Request, next: str = "/", err: str = ""):
    return templates.TemplateResponse(request, "login.html", {
        "next_path": next or "/",
        "error":     err,
    })


@app.post("/api/login", response_class=JSONResponse)
async def api_login(request: Request):
    """JSON or form-data login. Body: {email, password, [next]}.
    Returns {ok, redirect} on success or {ok:false, error} on failure."""
    ctype = request.headers.get("content-type", "")
    if "application/json" in ctype:
        body = await request.json()
    else:
        form = await request.form()
        body = dict(form)
    email = (body.get("email") or "").strip().lower()
    password = body.get("password") or ""
    nxt = body.get("next") or "/"

    user_ok = email in ALLOWED_USERS
    pw_ok   = secrets.compare_digest(password, _EXPECTED_PW)
    if not (user_ok and pw_ok):
        return JSONResponse({"ok": False, "error": "Invalid email or password"}, status_code=401)

    request.session["user"] = email
    return JSONResponse({"ok": True, "redirect": nxt})


@app.post("/api/logout", response_class=JSONResponse)
def api_logout(request: Request):
    request.session.clear()
    return {"ok": True, "redirect": "/login"}


# ─── Pages ────────────────────────────────────────────────────────────────

@app.get("/", response_class=HTMLResponse)
def dashboard(request: Request):
    stats    = queries.cached("dash_stats",         CACHE_TTL, queries.dashboard_stats)
    officers = queries.cached("officer_caseloads",  CACHE_TTL, queries.officer_caseloads)
    letters  = queries.cached("caseload_by_letter", CACHE_TTL, queries.caseload_by_letter)
    return templates.TemplateResponse(request, "index.html", {
        "stats": stats, "officers": officers, "letters": letters,
    })


@app.get("/pretrial_app.html", response_class=HTMLResponse)
def case_management(request: Request):
    raw = queries.cached("case_mgmt_bundle", CACHE_TTL, queries.case_management_bundle)
    return templates.TemplateResponse(request, "pretrial_app.html", {"raw_data": raw})


@app.get("/analytics.html", response_class=HTMLResponse)
def analytics(request: Request):
    bundle = queries.cached("analytics",  CACHE_TTL, queries.analytics_bundle)
    stats  = queries.cached("dash_stats", CACHE_TTL, queries.dashboard_stats)
    return templates.TemplateResponse(request, "analytics.html", {
        "analytics": bundle, "stats": stats,
    })


@app.get("/client_profile.html", response_class=HTMLResponse)
def client_profile(request: Request):
    data = queries.cached("client_profiles", CACHE_TTL, queries.client_profiles_bundle)
    return templates.TemplateResponse(request, "client_profile.html", {"data": data})


# Static pages (no DB context).
for page in ("gps_alert_procedures", "system_comparison_mockup", "referrals",
             "log_activity", "edit_defendant", "my_day", "court_calendar",
             "violations", "audit_log_view"):
    def _handler(req: Request, _p=page):
        return templates.TemplateResponse(req, f"{_p}.html", {})
    app.get(f"/{page}.html", response_class=HTMLResponse)(_handler)


# ─── JSON API ─────────────────────────────────────────────────────────────

@app.get("/api/stats",     response_class=JSONResponse)
def api_stats():     return queries.cached("dash_stats",        CACHE_TTL, queries.dashboard_stats)

@app.get("/api/defendants", response_class=JSONResponse)
def api_defendants(): return queries.cached("case_mgmt_bundle", CACHE_TTL, queries.case_management_bundle)

@app.get("/api/analytics",  response_class=JSONResponse)
def api_analytics():  return queries.cached("analytics",        CACHE_TTL, queries.analytics_bundle)

@app.get("/api/officers",   response_class=JSONResponse)
def api_officers():   return queries.cached("officer_caseloads", CACHE_TTL, queries.officer_caseloads)

@app.get("/api/activity",   response_class=JSONResponse)
def api_activity():   return queries.cached("recent_activity",  CACHE_TTL, lambda: queries.recent_activity(12))

@app.get("/api/clients",    response_class=JSONResponse)
def api_clients():    return queries.cached("client_profiles",  CACHE_TTL, queries.client_profiles_bundle)

@app.get("/api/refresh",    response_class=JSONResponse)
def api_refresh():
    queries._cache.clear()
    return {"ok": True, "cleared": "cache"}


@app.get("/api/lookup",     response_class=JSONResponse)
def api_lookup(q: str = "", limit: int = 10):
    """Live-search for the data-entry pages — find existing defendants by
    name or IDN to prevent duplicate entries. Not cached (must be fresh)."""
    return {"q": q, "results": queries.defendant_lookup(q, limit=limit)}


# ─── Write endpoints (data entry) ────────────────────────────────────────

@app.post("/api/referrals", response_class=JSONResponse)
async def api_post_referral(request: Request):
    body = await request.json()
    result = queries.insert_referral(body)
    return JSONResponse(result, status_code=200 if result.get("ok") else 400)


@app.post("/api/check_ins", response_class=JSONResponse)
async def api_post_check_in(request: Request):
    body = await request.json()
    result = queries.insert_check_in(body)
    return JSONResponse(result, status_code=200 if result.get("ok") else 400)


@app.post("/api/payments",  response_class=JSONResponse)
async def api_post_payment(request: Request):
    body = await request.json()
    result = queries.insert_payment(body)
    return JSONResponse(result, status_code=200 if result.get("ok") else 400)


@app.get("/api/defendants/{idn}", response_class=JSONResponse)
def api_get_defendant(idn: int):
    d = queries.get_defendant_full(idn)
    if not d:
        return JSONResponse({"error": "not found"}, status_code=404)
    return d


@app.get("/api/defendants/{idn}/details", response_class=JSONResponse)
def api_get_defendant_details(idn: int):
    """Bundle for the slide-in detail drawer."""
    d = queries.get_defendant_details(idn)
    if not d:
        return JSONResponse({"error": "not found"}, status_code=404)
    return d


@app.patch("/api/defendants/{idn}", response_class=JSONResponse)
async def api_patch_defendant(idn: int, request: Request):
    body = await request.json()
    # Snapshot original values before update for audit log.
    before = queries.get_defendant_full(idn) or {}
    result = queries.update_defendant(idn, body)
    if result.get("ok"):
        user = _user(request)
        for col in result.get("changed_cols", []):
            qx.write_audit(
                user=user, action="update", table_name="defendants",
                row_id=idn, col_name=col,
                old_value=before.get(col),
                new_value=body.get(col),
            )
    return JSONResponse(result, status_code=200 if result.get("ok") else 400)


# ─── Notes ──────────────────────────────────────────────────────────────

@app.get("/api/defendants/{idn}/notes", response_class=JSONResponse)
def api_list_notes(idn: int):
    return {"notes": qx.list_notes(idn)}

@app.post("/api/defendants/{idn}/notes", response_class=JSONResponse)
async def api_add_note(idn: int, request: Request):
    body = await request.json()
    return qx.add_note(idn, _user(request), body.get("body", ""))

@app.delete("/api/notes/{note_id}", response_class=JSONResponse)
def api_delete_note(note_id: int, request: Request):
    return qx.delete_note(note_id, _user(request))


# ─── Tags ───────────────────────────────────────────────────────────────

@app.get("/api/defendants/{idn}/tags", response_class=JSONResponse)
def api_list_tags(idn: int):
    return {"tags": qx.list_tags(idn)}

@app.post("/api/defendants/{idn}/tags", response_class=JSONResponse)
async def api_add_tag(idn: int, request: Request):
    body = await request.json()
    return qx.add_tag(idn, body.get("label", ""), _user(request))

@app.delete("/api/tags/{tag_id}", response_class=JSONResponse)
def api_delete_tag(tag_id: int):
    return qx.delete_tag(tag_id)

@app.get("/api/tags", response_class=JSONResponse)
def api_all_tags():
    return {"tags": qx.all_tag_labels()}


# ─── Court Dates ────────────────────────────────────────────────────────

@app.get("/api/defendants/{idn}/court_dates", response_class=JSONResponse)
def api_list_court_dates(idn: int):
    return {"court_dates": qx.list_court_dates(idn)}

@app.post("/api/defendants/{idn}/court_dates", response_class=JSONResponse)
async def api_add_court_date(idn: int, request: Request):
    body = await request.json()
    return qx.add_court_date(
        idn, body.get("court_date"), body.get("court", ""),
        body.get("notes", ""), _user(request),
    )

@app.delete("/api/court_dates/{cdid}", response_class=JSONResponse)
def api_delete_court_date(cdid: int):
    return qx.delete_court_date(cdid)

@app.get("/api/court_dates", response_class=JSONResponse)
def api_upcoming_court_dates(days: int = 30):
    return {"court_dates": qx.upcoming_court_dates(days=days)}


# ─── Audit log ──────────────────────────────────────────────────────────

@app.get("/api/defendants/{idn}/audit", response_class=JSONResponse)
def api_audit_for_defendant(idn: int):
    return {"audit": qx.audit_for_defendant(idn)}


# ─── Violations ─────────────────────────────────────────────────────────

@app.get("/api/violations", response_class=JSONResponse)
def api_violations(idn: int = None):
    return {"violations": qx.list_violations(idn=idn)}

@app.post("/api/violations", response_class=JSONResponse)
async def api_add_violation(request: Request):
    body = await request.json()
    body.setdefault("officer", _user(request))
    return qx.add_violation(body)


# ─── Pinned defendants ──────────────────────────────────────────────────

@app.get("/api/pinned", response_class=JSONResponse)
def api_pinned(request: Request):
    return {"pinned": qx.list_pinned(_user(request))}

@app.post("/api/defendants/{idn}/pin", response_class=JSONResponse)
def api_toggle_pin(idn: int, request: Request):
    return qx.toggle_pin(_user(request), idn)


# ─── User preferences ───────────────────────────────────────────────────

@app.get("/api/prefs", response_class=JSONResponse)
def api_get_prefs(request: Request):
    return qx.get_prefs(_user(request))

@app.post("/api/prefs", response_class=JSONResponse)
async def api_set_prefs(request: Request):
    body = await request.json()
    return qx.set_prefs(
        _user(request),
        theme=body.get("theme"),
        default_landing=body.get("default_landing"),
        prefs=body.get("prefs"),
    )


# ─── Reminders ──────────────────────────────────────────────────────────

@app.get("/api/reminders", response_class=JSONResponse)
def api_reminders(request: Request, mine: bool = True, idn: int = None,
                  include_completed: bool = False):
    assigned = _user(request) if mine and idn is None else None
    return {"reminders": qx.list_reminders(idn=idn, assigned_to=assigned,
                                           include_completed=include_completed)}

@app.post("/api/reminders", response_class=JSONResponse)
async def api_add_reminder(request: Request):
    body = await request.json()
    body.setdefault("created_by", _user(request))
    body.setdefault("assigned_to", _user(request))
    return qx.add_reminder(body)

@app.post("/api/reminders/{rid}/complete", response_class=JSONResponse)
def api_complete_reminder(rid: int, request: Request):
    return qx.complete_reminder(rid, _user(request))

@app.delete("/api/reminders/{rid}", response_class=JSONResponse)
def api_delete_reminder(rid: int):
    return qx.delete_reminder(rid)


# ─── Compliance / alerts ────────────────────────────────────────────────

@app.get("/api/alerts", response_class=JSONResponse)
def api_alerts(request: Request, mine: bool = True):
    user = _user(request) if mine else None
    return qx.alerts_summary(user)

@app.get("/api/overdue", response_class=JSONResponse)
def api_overdue(days: int = 14, limit: int = 200):
    return {"overdue": qx.overdue_check_ins(days=days, limit=limit)}


# ─── My Day ─────────────────────────────────────────────────────────────

@app.get("/api/my_day", response_class=JSONResponse)
def api_my_day(request: Request):
    return qx.my_day_bundle(_user(request))


# ─── Defendant timeline ─────────────────────────────────────────────────

@app.get("/api/defendants/{idn}/timeline", response_class=JSONResponse)
def api_timeline(idn: int):
    return {"timeline": qx.defendant_timeline(idn)}


# ─── Saved searches ─────────────────────────────────────────────────────

@app.get("/api/saved_searches", response_class=JSONResponse)
def api_saved_searches(request: Request):
    return {"searches": qx.list_saved_searches(_user(request))}

@app.post("/api/saved_searches", response_class=JSONResponse)
async def api_add_saved_search(request: Request):
    body = await request.json()
    return qx.add_saved_search(_user(request),
        name=body.get("name", ""), spec=body.get("spec", {}),
        page=body.get("page"), pinned=body.get("pinned", False))

@app.delete("/api/saved_searches/{sid}", response_class=JSONResponse)
def api_delete_saved_search(sid: int, request: Request):
    return qx.delete_saved_search(sid, _user(request))


@app.get("/api/whoami",     response_class=JSONResponse)
def api_whoami(request: Request):
    return {"user": getattr(request.state, "user", None)}


# Health probe - auth-free, for App Service uptime checks.
@app.get("/health", response_class=JSONResponse)
def health():
    try:
        queries.get_conn().cursor().execute("SELECT 1")
        return {"ok": True, "db": "up"}
    except Exception as e:
        return JSONResponse({"ok": False, "error": str(e)}, status_code=503)


@app.get("/dashboard")
def r_dashboard(): return RedirectResponse("/")
@app.get("/case_management")
def r_case():      return RedirectResponse("/pretrial_app.html")
@app.get("/analytics")
def r_ana():       return RedirectResponse("/analytics.html")


# ─── Session middleware MUST be added last so it wraps the auth middleware. ───
# (FastAPI: last-added middleware is the outermost layer.)
app.add_middleware(
    SessionMiddleware,
    secret_key=_SESSION_SECRET,
    session_cookie="kh_sess",
    max_age=60 * 60 * 12,    # 12 hours
    same_site="lax",
    https_only=False,         # Cloudflare Tunnel terminates TLS upstream
)
