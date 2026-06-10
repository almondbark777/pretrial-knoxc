/* Knox County Pre-Trial — site chrome
 * Injects masthead, alerts badge, recently-viewed, theme toggle, shortcuts.
 * Auto-loads drawer.js + palette.js on every page.
 */
(function () {
  if (window.__khSiteChromeLoaded) return;
  window.__khSiteChromeLoaded = true;

  const NAV_LINKS = [
    ["/",                              "Dashboard"],
    ["/my_day.html",                   "My Day"],
    ["/referrals.html",                "Referral"],
    ["/log_activity.html",             "Log Activity"],
    ["/edit_defendant.html",           "Edit"],
    ["/pretrial_app.html",             "Case Mgmt"],
    ["/client_profile.html",           "Profile"],
    ["/court_calendar.html",           "Court"],
    ["/violations.html",               "Violations"],
    ["/analytics.html",                "Analytics"],
    ["/gps_alert_procedures.html",     "GPS"],
  ];

  const SHORTCUTS = [
    ["Ctrl+K  /  ⌘K  /  /",  "Open Quick Search"],
    ["a",                    "Add — open quick entry (search first)"],
    ["c",                    "Add a check-in"],
    ["p",                    "Add a payment"],
    ["e",                    "Edit a defendant's info"],
    ["Esc",                  "Close drawer / palette"],
    ["?",                    "Show this help"],
    ["g d",                  "Go to Dashboard"],
    ["g m",                  "Go to My Day"],
    ["g r",                  "Go to Referrals"],
    ["g l",                  "Go to Log Activity"],
    ["g c",                  "Go to Court Calendar"],
    ["g v",                  "Go to Violations"],
    ["t",                    "Toggle dark/light theme"],
    ["dblclick on defendant", "Open detail drawer"],
  ];

  // ───── Auto-load global assets ─────
  function injectGlobals() {
    const head = document.head;
    // Force the viewport meta tag for mobile (some legacy templates may lack one or
    // have it set wrong). Also disable user-scaling on inputs only via 16px CSS.
    let vp = document.querySelector('meta[name="viewport"]');
    if (!vp) {
      vp = document.createElement('meta');
      vp.name = 'viewport';
      head.appendChild(vp);
    }
    vp.setAttribute('content', 'width=device-width, initial-scale=1.0, viewport-fit=cover');

    // PWA: web app manifest + iOS home-screen tags
    if (!document.querySelector('link[rel="manifest"]')) {
      const m = document.createElement('link');
      m.rel = 'manifest'; m.href = '/static/manifest.json';
      head.appendChild(m);
    }
    if (!document.querySelector('meta[name="apple-mobile-web-app-capable"]')) {
      const ios1 = document.createElement('meta');
      ios1.name = 'apple-mobile-web-app-capable'; ios1.content = 'yes';
      head.appendChild(ios1);
      const ios2 = document.createElement('meta');
      ios2.name = 'apple-mobile-web-app-status-bar-style'; ios2.content = 'black-translucent';
      head.appendChild(ios2);
      const ios3 = document.createElement('meta');
      ios3.name = 'apple-mobile-web-app-title'; ios3.content = 'Pre-Trial';
      head.appendChild(ios3);
      const tc = document.createElement('meta');
      tc.name = 'theme-color'; tc.content = '#0f3a66';
      head.appendChild(tc);
      const ati = document.createElement('link');
      ati.rel = 'apple-touch-icon'; ati.href = '/static/icon-180.png';
      head.appendChild(ati);
      const fav = document.createElement('link');
      fav.rel = 'icon'; fav.type = 'image/png'; fav.href = '/static/icon-192.png';
      head.appendChild(fav);
    }

    ["/static/drawer.css", "/static/palette.css", "/static/chrome-extras.css", "/static/mobile.css", "/static/quickadd.css"]
      .forEach(href => {
        if (!document.querySelector(`link[href="${href}"]`)) {
          const l = document.createElement("link");
          l.rel = "stylesheet";
          l.href = href;
          head.appendChild(l);
        }
      });
    ["/static/drawer.js", "/static/palette.js", "/static/quickadd.js"].forEach(src => {
      if (!document.querySelector(`script[src="${src}"]`)) {
        const s = document.createElement("script");
        s.defer = true;
        s.src = src;
        head.appendChild(s);
      }
    });
  }

  // ───── Masthead ─────
  function injectMasthead() {
    if (document.querySelector(".masthead")) return;
    const path = window.location.pathname.replace(/\/+$/, "") || "/";
    const navHTML = NAV_LINKS.map(([href, label]) => {
      const active = (href === path) ? " active" : "";
      return `<a href="${href}" class="${active.trim()}">${label}</a>`;
    }).join("");
    const isMac = navigator.platform.toUpperCase().includes("MAC");
    const kbd = isMac ? "⌘K" : "Ctrl+K";

    const mast = document.createElement("header");
    mast.className = "masthead";
    mast.innerHTML = `
      <button type="button" class="kh-hamburger" id="kh-hamburger" aria-label="Menu">☰</button>
      <div class="mast-brand">
        <div class="mast-mark">⚖</div>
        <div class="mast-title">
          <div class="mast-kicker">Knox County Sheriff</div>
          <h1 class="mast-h">Pre-<em>Trial</em> Services</h1>
        </div>
      </div>
      <nav class="topnav">${navHTML}</nav>
      <div class="mast-tools">
        <button type="button" class="kh-add-btn" id="kh-add-btn" title="Add check-in / payment / edit (a)">
          <span class="ic">＋</span> Add
        </button>
        <button type="button" class="kh-search-trigger" id="kh-search-trigger" title="Quick search (${kbd} or /)">
          <span>⌕ Search</span>
          <span class="kh-palette-kbd">${kbd}</span>
        </button>
        <button type="button" class="kh-icon-btn" id="kh-alerts-btn" title="Alerts">
          🔔<span class="kh-badge" id="kh-alert-count" style="display:none;">0</span>
        </button>
        <button type="button" class="kh-icon-btn" id="kh-recent-btn" title="Recently viewed">⏱</button>
        <button type="button" class="kh-icon-btn" id="kh-theme-btn" title="Toggle theme (t)">◐</button>
        <button type="button" class="kh-icon-btn" id="kh-help-btn" title="Keyboard shortcuts (?)">?</button>
        <button type="button" class="kh-icon-btn" id="kh-logout-btn" title="Sign out">⎋</button>
      </div>
      <div class="mast-meta">
        <span class="dot"></span>
        <span class="user-tag" id="kh-user-tag">connecting…</span>
      </div>
    `;
    document.body.insertBefore(mast, document.body.firstChild);

    document.getElementById("kh-hamburger").addEventListener("click", toggleMobileMenu);
    document.getElementById("kh-add-btn").addEventListener("click",
      () => window.PTRQuick && window.PTRQuick.open({}));
    document.getElementById("kh-search-trigger").addEventListener("click",
      () => window.khPalette && window.khPalette.open());
    document.getElementById("kh-alerts-btn").addEventListener("click", toggleAlerts);
    document.getElementById("kh-recent-btn").addEventListener("click", toggleRecent);
    document.getElementById("kh-theme-btn").addEventListener("click", toggleTheme);
    document.getElementById("kh-help-btn").addEventListener("click", showHelp);
    document.getElementById("kh-logout-btn").addEventListener("click", doLogout);

    // Build the mobile menu (slide-in from left). Always in the DOM, only
    // visible at ≤760px. Uses the same NAV_LINKS as the desktop top nav.
    const menuScrim = document.createElement("div");
    menuScrim.className = "kh-mobile-menu-scrim";
    menuScrim.id = "kh-mobile-menu-scrim";
    menuScrim.addEventListener("click", () => toggleMobileMenu(false));
    const menu = document.createElement("aside");
    menu.className = "kh-mobile-menu";
    menu.id = "kh-mobile-menu";
    menu.innerHTML = `
      <div class="kh-mobile-menu-h">
        Pre-<em>Trial</em>
        <span class="who" id="kh-mobile-user">—</span>
      </div>
      ${NAV_LINKS.map(([href, label]) => {
        const active = (href === path) ? " active" : "";
        return `<a href="${href}" class="${active.trim()}">${label}</a>`;
      }).join("")}
      <a href="javascript:void(0)" id="kh-mobile-logout" style="margin-top:auto; border-top:1px solid #2a2a2a; color:#b03b3b;">Sign out</a>
    `;
    document.body.appendChild(menuScrim);
    document.body.appendChild(menu);
    document.getElementById("kh-mobile-logout")?.addEventListener("click", doLogout);

    // Whoami → user-tag (desktop + mobile menu header)
    fetch("/api/whoami", { credentials: "same-origin" })
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        const tag = document.getElementById("kh-user-tag");
        const mobileTag = document.getElementById("kh-mobile-user");
        const display = (d && d.user)
          ? d.user.split("@")[0].replace(/\./g, " ")
          : "—";
        if (tag) tag.textContent = display;
        if (mobileTag) mobileTag.textContent = display;
      })
      .catch(() => {
        const tag = document.getElementById("kh-user-tag");
        if (tag) tag.textContent = "—";
      });
  }

  // ───── Sign out ─────
  function doLogout() {
    fetch('/api/logout', { method: 'POST', credentials: 'same-origin' })
      .then(r => r.json())
      .then(j => { window.location.href = j.redirect || '/login'; })
      .catch(() => { window.location.href = '/login'; });
  }

  // ───── Mobile menu toggle ─────
  function toggleMobileMenu(force) {
    const scrim = document.getElementById("kh-mobile-menu-scrim");
    const menu = document.getElementById("kh-mobile-menu");
    if (!scrim || !menu) return;
    const open = (force === true || force === false)
      ? force
      : !menu.classList.contains("open");
    menu.classList.toggle("open", open);
    scrim.classList.toggle("open", open);
    document.body.style.overflow = open ? "hidden" : "";
  }

  // ───── Alerts dropdown ─────
  let alertsOpen = false;
  function toggleAlerts() {
    let pop = document.getElementById("kh-alerts-pop");
    if (alertsOpen && pop) { pop.remove(); alertsOpen = false; return; }
    alertsOpen = true;
    pop = document.createElement("div");
    pop.id = "kh-alerts-pop";
    pop.className = "kh-pop";
    pop.innerHTML = '<div class="kh-pop-loading">Loading…</div>';
    document.body.appendChild(pop);
    positionPop(pop, document.getElementById("kh-alerts-btn"));
    fetch("/api/alerts?mine=true").then(r => r.json()).then(a => {
      pop.innerHTML = `
        <div class="kh-pop-h">Alerts <small>(your caseload)</small></div>
        <a class="kh-pop-row" href="/my_day.html#overdue">
          <span class="kh-pop-num">${a.overdue_checkins}</span>
          <span>Overdue check-ins (14+ days)</span>
        </a>
        <a class="kh-pop-row" href="/my_day.html#violations">
          <span class="kh-pop-num">${a.pending_violations}</span>
          <span>Pending violations to act on</span>
        </a>
        <a class="kh-pop-row" href="/court_calendar.html">
          <span class="kh-pop-num">${a.court_this_week}</span>
          <span>Court appearances this week</span>
        </a>
        <a class="kh-pop-row" href="/my_day.html#reminders">
          <span class="kh-pop-num">${a.my_reminders}</span>
          <span>Open reminders for you</span>
        </a>
        <div class="kh-pop-foot">Showing only items in your caseload</div>`;
    });
  }

  function refreshAlertCount() {
    fetch("/api/alerts?mine=true").then(r => r.json()).then(a => {
      const b = document.getElementById("kh-alert-count");
      if (!b) return;
      const total = (a.overdue_checkins || 0) + (a.pending_violations || 0) + (a.my_reminders || 0);
      if (total > 0) {
        b.textContent = total > 99 ? "99+" : total;
        b.style.display = "inline-block";
      } else {
        b.style.display = "none";
      }
    }).catch(()=>{});
  }

  // ───── Recently viewed dropdown ─────
  let recentOpen = false;
  function toggleRecent() {
    let pop = document.getElementById("kh-recent-pop");
    if (recentOpen && pop) { pop.remove(); recentOpen = false; return; }
    recentOpen = true;
    pop = document.createElement("div");
    pop.id = "kh-recent-pop";
    pop.className = "kh-pop";
    document.body.appendChild(pop);
    positionPop(pop, document.getElementById("kh-recent-btn"));
    const rows = JSON.parse(localStorage.getItem("kh:recent:v1") || "[]");
    if (!rows.length) {
      pop.innerHTML = '<div class="kh-pop-h">Recently viewed</div><div class="kh-pop-empty">No defendants opened yet.</div>';
      return;
    }
    pop.innerHTML = `<div class="kh-pop-h">Recently viewed</div>` +
      rows.map(r => `
        <a class="kh-pop-row" data-idn="${r.idn}" href="javascript:void(0)" onclick="window.khDrawer && window.khDrawer.open('${r.idn}'); document.getElementById('kh-recent-pop')?.remove(); window.__khRecentOpen=false;">
          <span>${escapeHtml(r.name)}</span>
          <span class="kh-pop-meta">IDN ${r.idn}</span>
        </a>`).join('') +
      `<div class="kh-pop-foot"><a href="javascript:void(0)" onclick="localStorage.removeItem('kh:recent:v1'); document.getElementById('kh-recent-pop')?.remove(); window.__khRecentOpen=false;">Clear list</a></div>`;
  }

  // ───── Theme toggle ─────
  function toggleTheme() {
    const cur = document.documentElement.getAttribute("data-theme") || "dark";
    const next = cur === "dark" ? "light" : "dark";
    applyTheme(next);
    fetch("/api/prefs", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({theme: next}),
    }).catch(()=>{});
    if (window.khToast) window.khToast(`Theme: ${next}`, 'ok');
  }

  function applyTheme(t) {
    document.documentElement.setAttribute("data-theme", t);
    localStorage.setItem("kh:theme", t);
  }

  // Apply theme ASAP — before paint — to avoid flash.
  (function bootTheme() {
    const cached = localStorage.getItem("kh:theme") || "dark";
    document.documentElement.setAttribute("data-theme", cached);
    fetch("/api/prefs").then(r => r.json()).then(p => {
      if (p && p.theme && p.theme !== cached) applyTheme(p.theme);
    }).catch(()=>{});
  })();

  // ───── Help overlay ─────
  function showHelp() {
    let overlay = document.getElementById("kh-help-overlay");
    if (overlay) { overlay.remove(); return; }
    overlay = document.createElement("div");
    overlay.id = "kh-help-overlay";
    overlay.className = "kh-help-scrim";
    overlay.innerHTML = `
      <div class="kh-help-panel">
        <h2>Keyboard Shortcuts</h2>
        <div class="kh-help-list">
          ${SHORTCUTS.map(([k, v]) =>
            `<div class="kh-help-row"><kbd>${k}</kbd><span>${v}</span></div>`
          ).join('')}
        </div>
        <div class="kh-help-foot">Press <kbd>Esc</kbd> or click outside to close</div>
      </div>`;
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) overlay.remove();
    });
    document.body.appendChild(overlay);
  }

  // ───── Click-outside for popovers ─────
  document.addEventListener("click", (e) => {
    ["kh-alerts-pop", "kh-recent-pop"].forEach(id => {
      const p = document.getElementById(id);
      if (p && !p.contains(e.target) &&
          !document.getElementById(id.replace("-pop", "-btn"))?.contains(e.target)) {
        p.remove();
        if (id === "kh-alerts-pop") alertsOpen = false;
        if (id === "kh-recent-pop") recentOpen = false;
      }
    });
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      document.getElementById("kh-alerts-pop")?.remove(); alertsOpen = false;
      document.getElementById("kh-recent-pop")?.remove(); recentOpen = false;
      document.getElementById("kh-help-overlay")?.remove();
      const menu = document.getElementById("kh-mobile-menu");
      if (menu && menu.classList.contains("open")) toggleMobileMenu(false);
    }
  });

  // ───── Keyboard shortcuts ─────
  let chordBuffer = "";
  let chordTimer = null;
  document.addEventListener("keydown", (e) => {
    // Skip when typing in inputs
    const tag = (document.activeElement?.tagName || "").toUpperCase();
    if (["INPUT","TEXTAREA","SELECT"].includes(tag)) return;

    if (e.key === "?") { e.preventDefault(); showHelp(); return; }
    if (e.key === "t") { e.preventDefault(); toggleTheme(); return; }
    // Quick-entry single-key hotkeys (skip while mid "g _" chord so g c / g e
    // still resolve to Court / Edit-page navigation).
    if (chordBuffer !== "g" && window.PTRQuick) {
      if (e.key === "a") { e.preventDefault(); window.PTRQuick.open({}); return; }
      if (e.key === "c") { e.preventDefault(); window.PTRQuick.open({ mode: "checkin" }); return; }
      if (e.key === "p") { e.preventDefault(); window.PTRQuick.open({ mode: "payment" }); return; }
      if (e.key === "e") { e.preventDefault(); window.PTRQuick.open({ mode: "edit" }); return; }
    }
    // chords like g d, g m, g r, etc.
    if (e.key === "g") {
      chordBuffer = "g";
      clearTimeout(chordTimer);
      chordTimer = setTimeout(() => { chordBuffer = ""; }, 1200);
      return;
    }
    if (chordBuffer === "g") {
      const map = {
        d: "/", m: "/my_day.html", r: "/referrals.html",
        l: "/log_activity.html", c: "/court_calendar.html",
        v: "/violations.html",
        a: "/analytics.html", p: "/client_profile.html",
        e: "/edit_defendant.html",
      };
      const dest = map[e.key.toLowerCase()];
      if (dest) {
        e.preventDefault();
        window.location.href = dest;
        chordBuffer = "";
      }
    }
  });

  // ───── Helpers ─────
  function positionPop(pop, anchor) {
    if (!anchor) return;
    const r = anchor.getBoundingClientRect();
    pop.style.position = "fixed";
    pop.style.top  = (r.bottom + 8) + "px";
    pop.style.right = (window.innerWidth - r.right) + "px";
    pop.style.zIndex = 7500;
  }
  function escapeHtml(s) {
    return String(s ?? '').replace(/[&<>"']/g, c => ({
      '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
    }[c]));
  }

  function init() {
    injectGlobals();
    injectMasthead();
    refreshAlertCount();
    setInterval(refreshAlertCount, 60000);
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
