/* ─────────────────────────────────────────────────────────────────────────
 * Command Palette — Ctrl+K / Cmd+K to open from anywhere.
 * Type to search defendants by name or IDN, arrow keys + Enter to open.
 * Public API:
 *   window.khPalette.open()    — open programmatically
 *   window.khPalette.close()
 * ───────────────────────────────────────────────────────────────────────── */
(function () {
  if (window.khPalette) return;

  // Build DOM
  const scrim = document.createElement('div');
  scrim.className = 'kh-palette-scrim';
  scrim.innerHTML = `
    <div class="kh-palette" role="dialog" aria-label="Command palette">
      <div class="kh-palette-input-row">
        <input class="kh-palette-input" id="kh-pal-input" type="text"
               placeholder="Search defendants by name or IDN…"
               autocomplete="off" spellcheck="false">
        <span class="kh-palette-kbd">Esc</span>
      </div>
      <div class="kh-palette-results" id="kh-pal-results">
        <div class="kh-palette-empty">Start typing to search the active roster…</div>
      </div>
      <div class="kh-palette-footer">
        <span><span class="kh-palette-kbd">↑↓</span> navigate</span>
        <span><span class="kh-palette-kbd">↵</span> open</span>
        <span><span class="kh-palette-kbd">Esc</span> close</span>
        <span class="spacer"></span>
        <span>Live search · active roster</span>
      </div>
    </div>
  `;

  let installed = false;
  let results = [];
  let activeIdx = -1;
  let queryTimer = null;
  let lastQ = '';

  function install() {
    if (installed) return;
    installed = true;
    document.body.appendChild(scrim);
    scrim.addEventListener('click', (e) => {
      if (e.target === scrim) close();
    });
    const input = document.getElementById('kh-pal-input');
    input.addEventListener('input', onInput);
    input.addEventListener('keydown', onKeyDown);
  }

  function open() {
    if (!installed) install();
    scrim.classList.add('open');
    const input = document.getElementById('kh-pal-input');
    input.value = '';
    results = [];
    activeIdx = -1;
    document.getElementById('kh-pal-results').innerHTML =
      '<div class="kh-palette-empty">Start typing to search the active roster…</div>';
    setTimeout(() => input.focus(), 30);
  }

  function close() {
    scrim.classList.remove('open');
  }

  function onInput(e) {
    const q = e.target.value.trim();
    if (queryTimer) clearTimeout(queryTimer);
    if (q.length < 2) {
      results = [];
      activeIdx = -1;
      document.getElementById('kh-pal-results').innerHTML =
        '<div class="kh-palette-empty">Type at least 2 characters…</div>';
      return;
    }
    queryTimer = setTimeout(() => runSearch(q), 160);
  }

  async function runSearch(q) {
    lastQ = q;
    try {
      const r = await fetch('/api/lookup?q=' + encodeURIComponent(q) + '&limit=15');
      const j = await r.json();
      if (j.q !== lastQ) return;
      results = j.results || [];
      activeIdx = results.length ? 0 : -1;
      render();
    } catch {
      results = [];
      render();
    }
  }

  function render() {
    const box = document.getElementById('kh-pal-results');
    if (!results.length) {
      box.innerHTML = '<div class="kh-palette-empty">No matches.</div>';
      return;
    }
    const html = '<div class="kh-palette-section">Defendants</div>' + results.map((r, i) => `
      <div class="kh-palette-item ${i === activeIdx ? 'active' : ''}" data-i="${i}">
        <div>
          <div class="name">${escapeHtml(r.name || '—')}</div>
          <div class="meta">IDN ${escapeHtml(r.idn)}${r.caseNum ? ' · ' + escapeHtml(r.caseNum) : ''}${r.officer ? ' · ' + escapeHtml(r.officer) : ''}</div>
        </div>
        <span class="pill ${r.active ? 'open' : 'closed'}">${escapeHtml(r.status || '—')}</span>
        <span class="meta">${r.level ? 'L' + escapeHtml(r.level) : ''}</span>
      </div>
    `).join('');
    box.innerHTML = html;
    [...box.querySelectorAll('.kh-palette-item')].forEach(el => {
      el.addEventListener('mouseenter', () => {
        activeIdx = parseInt(el.dataset.i, 10);
        updateActive();
      });
      el.addEventListener('click', () => choose(parseInt(el.dataset.i, 10)));
    });
  }

  function updateActive() {
    const items = document.querySelectorAll('.kh-palette-item');
    items.forEach((el, i) => el.classList.toggle('active', i === activeIdx));
    const active = items[activeIdx];
    if (active) active.scrollIntoView({ block: 'nearest' });
  }

  function choose(i) {
    const r = results[i];
    if (!r) return;
    close();
    if (window.khDrawer) {
      window.khDrawer.open(r.idn);
    } else {
      window.location.href = '/edit_defendant.html?idn=' + encodeURIComponent(r.idn);
    }
  }

  function onKeyDown(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (results.length) {
        activeIdx = Math.min(activeIdx + 1, results.length - 1);
        updateActive();
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (results.length) {
        activeIdx = Math.max(activeIdx - 1, 0);
        updateActive();
      }
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (activeIdx >= 0) choose(activeIdx);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      close();
    }
  }

  // Global hotkey
  document.addEventListener('keydown', (e) => {
    const isMac = navigator.platform.toUpperCase().includes('MAC');
    const trigger = (isMac && e.metaKey && e.key === 'k') ||
                    (!isMac && e.ctrlKey && e.key === 'k');
    if (trigger) {
      e.preventDefault();
      open();
    }
    // / key opens palette unless typing in an input
    if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement?.tagName)) {
      e.preventDefault();
      open();
    }
  });

  function escapeHtml(s) {
    return String(s ?? '').replace(/[&<>"']/g, c => ({
      '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
    }[c]));
  }

  // Public API
  window.khPalette = { open, close };
})();
