/* ─────────────────────────────────────────────────────────────────────────
 * Defendant Detail Drawer — slide-in panel with tabs.
 * Tabs: Overview · Timeline · Notes · Court · History (audit)
 * Public API:
 *   window.khDrawer.open(idn[, tab])
 *   window.khDrawer.close()
 *   window.khToast(msg, kind)
 * Auto-wires:
 *   - Double-click on any element with [data-idn] opens the drawer
 *   - Esc closes the drawer
 *   - Tracks recently-viewed in localStorage (read by site-chrome)
 * ───────────────────────────────────────────────────────────────────────── */
(function () {
  if (window.khDrawer) return;

  // ───── Toast helper ─────────────────────────────────────────────────
  function ensureToast() {
    let t = document.getElementById('kh-toast');
    if (!t) {
      t = document.createElement('div');
      t.id = 'kh-toast';
      t.className = 'kh-toast';
      document.body.appendChild(t);
    }
    return t;
  }
  window.khToast = function (msg, kind) {
    const t = ensureToast();
    t.textContent = msg;
    t.className = 'kh-toast show ' + (kind || 'ok');
    clearTimeout(t._timer);
    t._timer = setTimeout(() => { t.className = 'kh-toast ' + (kind || 'ok'); }, 3500);
  };

  // ───── Recently viewed (localStorage) ───────────────────────────────
  function pushRecent(idn, name) {
    try {
      const key = 'kh:recent:v1';
      const cur = JSON.parse(localStorage.getItem(key) || '[]');
      const next = [{idn: String(idn), name: name || `IDN ${idn}`, ts: Date.now()},
                    ...cur.filter(r => String(r.idn) !== String(idn))].slice(0, 10);
      localStorage.setItem(key, JSON.stringify(next));
      window.dispatchEvent(new Event('kh:recent-updated'));
    } catch {}
  }

  // ───── Build drawer DOM ─────────────────────────────────────────────
  const scrim = document.createElement('div');
  scrim.className = 'kh-drawer-scrim';

  const drawer = document.createElement('aside');
  drawer.className = 'kh-drawer';
  drawer.setAttribute('role', 'dialog');
  drawer.setAttribute('aria-label', 'Defendant detail');
  drawer.innerHTML = `
    <div class="kh-drawer-head">
      <button class="kh-drawer-pin" id="kh-d-pin" title="Pin defendant"><span>☆</span> Pin</button>
      <button class="kh-drawer-close" aria-label="Close (Esc)">&times;</button>
      <div class="kicker">Defendant</div>
      <h2 class="name" id="kh-d-name">—</h2>
      <div class="meta" id="kh-d-meta">—</div>
    </div>
    <div class="kh-drawer-tabs" id="kh-d-tabs">
      <button class="kh-drawer-tab active" data-tab="overview">Overview</button>
      <button class="kh-drawer-tab" data-tab="timeline">Timeline</button>
      <button class="kh-drawer-tab" data-tab="notes">Notes <span class="count" id="kh-d-cnt-notes">0</span></button>
      <button class="kh-drawer-tab" data-tab="court">Court <span class="count" id="kh-d-cnt-court">0</span></button>
      <button class="kh-drawer-tab" data-tab="history">History</button>
    </div>
    <div class="kh-drawer-body" id="kh-d-body">
      <div class="kh-drawer-loading">Loading defendant…</div>
    </div>
    <div class="kh-drawer-foot">
      <div class="kh-d-actionbar">
        <button class="btn btn-ghost" id="kh-d-add-ci" title="Log a check-in for this defendant (c)">＋ Check-In</button>
        <button class="btn btn-ghost" id="kh-d-add-pm" title="Log a payment for this defendant (p)">＋ Payment</button>
      </div>
      <a class="btn btn-ghost" href="#" id="kh-d-fulledit">Edit ↗</a>
      <button class="btn btn-primary" id="kh-d-close-btn">Done</button>
    </div>
  `;

  document.addEventListener('DOMContentLoaded', () => {
    document.body.appendChild(scrim);
    document.body.appendChild(drawer);
    drawer.querySelector('.kh-drawer-close').addEventListener('click', closeDrawer);
    drawer.querySelector('#kh-d-close-btn').addEventListener('click', closeDrawer);
    drawer.querySelector('#kh-d-pin').addEventListener('click', togglePin);
    drawer.querySelector('#kh-d-add-ci').addEventListener('click', () => quickAdd('checkin'));
    drawer.querySelector('#kh-d-add-pm').addEventListener('click', () => quickAdd('payment'));
    scrim.addEventListener('click', closeDrawer);
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && drawer.classList.contains('open')) closeDrawer();
    });
    document.addEventListener('dblclick', (e) => {
      const el = e.target.closest('[data-idn]');
      if (el) {
        e.preventDefault();
        openDrawer(el.dataset.idn);
      }
    });
    drawer.querySelectorAll('.kh-drawer-tab').forEach(t => {
      t.addEventListener('click', () => switchTab(t.dataset.tab));
    });
  });

  // ───── State ─────────────────────────────────────────────────────────
  let currentIdn = null;
  let currentData = null;
  let activeTab = 'overview';
  let pinned = false;
  let cache = { notes: null, court: null, audit: null, timeline: null };

  // ───── Open / Close ─────────────────────────────────────────────────
  function openDrawer(idn, tab) {
    if (!idn) return;
    currentIdn = String(idn);
    activeTab = tab || 'overview';
    cache = { notes: null, court: null, audit: null, timeline: null };
    drawer.classList.add('open');
    scrim.classList.add('open');
    document.body.style.overflow = 'hidden';
    drawer.querySelectorAll('.kh-drawer-tab').forEach(t =>
      t.classList.toggle('active', t.dataset.tab === activeTab));
    document.getElementById('kh-d-name').textContent = '—';
    document.getElementById('kh-d-meta').textContent = `IDN ${idn}`;
    document.getElementById('kh-d-body').innerHTML =
      '<div class="kh-drawer-loading">Loading defendant…</div>';
    document.getElementById('kh-d-fulledit').href = '/edit_defendant.html?idn=' + encodeURIComponent(idn);

    // Pin status
    fetch('/api/pinned')
      .then(r => r.json())
      .then(j => {
        pinned = (j.pinned || []).some(p => String(p.idn) === currentIdn);
        renderPin();
      }).catch(()=>{});

    // Main details
    fetch('/api/defendants/' + encodeURIComponent(idn) + '/details')
      .then(r => r.ok ? r.json() : Promise.reject('Not found'))
      .then(d => {
        currentData = d;
        pushRecent(d.idn, d.defendant_name);
        document.getElementById('kh-d-name').textContent = d.defendant_name || `IDN ${d.idn}`;
        const isOpen = (d.case_status || '').toLowerCase().startsWith('open');
        document.getElementById('kh-d-meta').innerHTML =
          `<span class="kh-drawer-pill ${isOpen ? 'open' : 'closed'}">${escapeHtml(d.case_status || '—')}</span>` +
          `IDN ${d.idn}` +
          (d.case_numbers ? ` · ${escapeHtml(d.case_numbers)}` : '');
        renderActiveTab();
      })
      .catch(err => {
        document.getElementById('kh-d-body').innerHTML =
          `<div class="kh-empty" style="color:#b03b3b;">Failed to load defendant.<br><small>${err}</small></div>`;
      });

    // Counts (notes, court) — update tab badges
    Promise.all([
      fetch('/api/defendants/' + encodeURIComponent(idn) + '/notes').then(r => r.json()),
      fetch('/api/defendants/' + encodeURIComponent(idn) + '/court_dates').then(r => r.json()),
    ]).then(([n, c]) => {
      cache.notes = n.notes || [];
      cache.court = c.court_dates || [];
      document.getElementById('kh-d-cnt-notes').textContent = cache.notes.length;
      document.getElementById('kh-d-cnt-court').textContent = cache.court.length;
      if (activeTab === 'notes' || activeTab === 'court') renderActiveTab();
    }).catch(()=>{});
  }

  function closeDrawer() {
    drawer.classList.remove('open');
    scrim.classList.remove('open');
    document.body.style.overflow = '';
    currentIdn = null;
    currentData = null;
  }

  function switchTab(tab) {
    activeTab = tab;
    drawer.querySelectorAll('.kh-drawer-tab').forEach(t =>
      t.classList.toggle('active', t.dataset.tab === activeTab));
    renderActiveTab();
  }

  // ───── Quick add (check-in / payment) for the open defendant ─────────
  function quickAdd(mode) {
    if (!currentIdn || !window.PTRQuick) return;
    const name = currentData?.defendant_name || '';
    let caseNum = currentData?.case_numbers || '';
    if (caseNum.includes(',')) caseNum = '';  // multi-case → don't stamp one wrongly
    window.PTRQuick.open({
      idn: currentIdn, name, caseNum, mode,
      onSaved: refreshAfterEntry,
    });
  }

  // Re-pull details after a quick add so the new row + totals show through.
  function refreshAfterEntry() {
    if (!currentIdn) return;
    cache.timeline = null;
    fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/details')
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (!d) return;
        currentData = d;
        if (activeTab === 'overview' || activeTab === 'timeline') renderActiveTab();
      }).catch(() => {});
  }

  // ───── Pin ──────────────────────────────────────────────────────────
  function renderPin() {
    const btn = document.getElementById('kh-d-pin');
    btn.classList.toggle('pinned', pinned);
    btn.innerHTML = (pinned ? '<span>★</span> Pinned' : '<span>☆</span> Pin');
  }

  function togglePin() {
    if (!currentIdn) return;
    fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/pin', { method: 'POST' })
      .then(r => r.json())
      .then(j => {
        pinned = !!j.pinned;
        renderPin();
        window.khToast(pinned ? 'Pinned' : 'Unpinned', 'ok');
        window.dispatchEvent(new Event('kh:pinned-updated'));
      });
  }

  // ───── Tab dispatch ─────────────────────────────────────────────────
  function renderActiveTab() {
    if (!currentData && activeTab !== 'notes' && activeTab !== 'court') return;
    const body = document.getElementById('kh-d-body');
    if      (activeTab === 'overview') body.innerHTML = renderOverview(currentData);
    else if (activeTab === 'timeline') renderTimelineTab(body);
    else if (activeTab === 'notes')    renderNotesTab(body);
    else if (activeTab === 'court')    renderCourtTab(body);
    else if (activeTab === 'history')  renderHistoryTab(body);

    if (activeTab === 'overview') wireInlineEdit();
  }

  // ───── Tab: Overview ────────────────────────────────────────────────
  function renderOverview(d) {
    const totals = d.totals || {};
    const stats = `
      <div class="kh-stat-strip">
        <div class="stat"><div class="n">${totals.check_in_count ?? 0}</div><div class="l">Check-Ins</div></div>
        <div class="stat"><div class="n">${totals.payment_count ?? 0}</div><div class="l">Payments</div></div>
        <div class="stat"><div class="n">$${(totals.paid ?? 0).toLocaleString()}</div><div class="l">Total Paid</div></div>
      </div>`;

    const fields = [
      { col: 'defendant_name',   label: 'Name',         type: 'text' },
      { col: 'birthdate',        label: 'Birthdate',    type: 'date' },
      { col: 'pretrial_level',   label: 'Pretrial Lvl', type: 'select', opts: [['','—'],['1','Level 1'],['2','Level 2'],['3','Level 3']] },
      { col: 'supervision_type', label: 'Supervision',  type: 'text' },
      { col: 'charge_type',      label: 'Charge',       type: 'select', opts: [['','—'],['Misdemeanor','Misdemeanor'],['Felony','Felony']] },
      { col: 'order_from',       label: 'Order From',   type: 'select', opts: [['','—'],['Judge','Judge'],['Magistrate','Magistrate']] },
      { col: 'supervising_officer', label: 'Officer',   type: 'text' },
      { col: 'bond_amount',      label: 'Bond Amt',     type: 'number' },
      { col: 'gps',              label: 'GPS',          type: 'bool' },
      { col: 'dma',              label: 'DMA',          type: 'bool' },
      { col: 'referral_date',    label: 'Referral',     type: 'date' },
      { col: 'closed_date',      label: 'Closed',       type: 'date' },
      { col: 'case_status',      label: 'Status',       type: 'select', opts: [['Open','Open'],['Closed','Closed']] },
    ];

    const kv = `<div class="kh-drawer-section"><h3>Identity &amp; Supervision</h3><div class="kh-kv">${
      fields.map(f => renderKVRow(f, d[f.col])).join('')
    }</div></div>`;

    // Tags inline (read-only here; managed in Notes tab)
    const tagsBlock = `<div class="kh-drawer-section" id="kh-d-tags-section"><h3>Tags</h3>
      <div class="kh-tag-list" id="kh-d-taglist"><span class="kh-empty" style="padding:0;">Loading…</span></div>
      <div class="kh-tag-input-row">
        <input id="kh-d-taginput" type="text" placeholder="Add tag (e.g. Bilingual, High-risk)…" maxlength="60">
        <button onclick="window.khDrawer._addTag()">Add</button>
      </div>
    </div>`;

    let gpsBlock = '';
    if (d.gps_details) {
      const g = d.gps_details;
      gpsBlock = `<div class="kh-drawer-section"><h3>GPS</h3><div class="kh-kv">
        <div class="k">Type</div>     <div class="v">${escapeHtml(g.type) || '—'}</div>
        <div class="k">Status</div>   <div class="v">${escapeHtml(g.status) || '—'}</div>
        <div class="k">Installed</div><div class="v">${escapeHtml(g.install) || '—'}</div>
        <div class="k">Order</div>    <div class="v">${escapeHtml(g.order) || '—'}</div>
        <div class="k">Victim</div>   <div class="v">${escapeHtml(g.victim) || '—'}</div>
        <div class="k">Victim OK</div><div class="v">${escapeHtml(g.accept) || '—'}</div>
        <div class="k">DA Emailed</div><div class="v">${escapeHtml(g.daEmailed) || '—'}</div>
      </div></div>`;
    }

    const ci = `<div class="kh-drawer-section"><h3>Recent Check-Ins</h3>${renderTable(
      ['Date', 'Type', 'Officer'],
      (d.check_ins || []).slice(0, 8).map(r => [r.date || '—', r.type, r.officer]),
      'No check-ins on record.'
    )}</div>`;

    const pm = `<div class="kh-drawer-section"><h3>Recent Payments</h3>${renderTable(
      ['Date', 'Type', 'Officer', 'Amount'],
      (d.payments || []).slice(0, 8).map(r => [r.date || '—', r.type, r.officer, `<span class="amt">$${r.amount.toFixed(2)}</span>`]),
      'No payments on record.'
    )}</div>`;

    const hint = `<div class="kh-drawer-hint">
      Click any field above to edit · <kbd>Esc</kbd> to close · <kbd>Tab</kbd> to switch sections
    </div>`;

    setTimeout(loadAndRenderTags, 0);
    return stats + kv + tagsBlock + gpsBlock + ci + pm + hint;
  }

  function renderKVRow(f, val) {
    let display = '';
    if (f.type === 'bool') {
      display = val ? '✓ Yes' : '— No';
    } else if (f.type === 'select') {
      const match = (f.opts || []).find(o => String(o[0]) === String(val ?? ''));
      display = match ? match[1] : (val ?? '');
    } else {
      display = val ?? '';
    }
    const isEmpty = !display || display === '—' || display === '— No';
    const safeDisplay = display === '' ? '—' : escapeHtml(String(display));
    const optsAttr = f.opts ? ` data-opts='${JSON.stringify(f.opts)}'` : '';
    return `
      <div class="k">${f.label}</div>
      <div class="v ${isEmpty ? 'empty' : ''}" data-editable
           data-col="${f.col}" data-type="${f.type}"${optsAttr}
           data-orig="${escapeHtml(String(val ?? ''))}">${safeDisplay}<span class="save-hint">unsaved</span></div>`;
  }

  function renderTable(cols, rows, emptyMsg) {
    const head = `<thead><tr>${cols.map(c => `<th>${c}</th>`).join('')}</tr></thead>`;
    let body;
    if (!rows.length) {
      body = `<tbody><tr class="empty-row"><td colspan="${cols.length}">${emptyMsg}</td></tr></tbody>`;
    } else {
      body = '<tbody>' + rows.map(r =>
        '<tr>' + r.map(c => `<td>${c == null ? '—' : c}</td>`).join('') + '</tr>'
      ).join('') + '</tbody>';
    }
    return `<table class="kh-mini-table">${head}${body}</table>`;
  }

  // ───── Tags (inside Overview) ───────────────────────────────────────
  function loadAndRenderTags() {
    if (!currentIdn) return;
    fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/tags')
      .then(r => r.json()).then(j => {
        const list = document.getElementById('kh-d-taglist');
        if (!list) return;
        const tags = j.tags || [];
        if (!tags.length) {
          list.innerHTML = '<span class="kh-empty" style="padding:0; font-style:italic;">No tags yet.</span>';
          return;
        }
        list.innerHTML = tags.map(t =>
          `<span class="kh-tag">${escapeHtml(t.label)}<span class="x" onclick="window.khDrawer._delTag(${t.id})" title="Remove">×</span></span>`
        ).join('');
      });
  }
  window.khDrawer = window.khDrawer || {};

  // ───── Tab: Timeline ────────────────────────────────────────────────
  function renderTimelineTab(body) {
    if (cache.timeline) {
      body.innerHTML = renderTimelineHTML(cache.timeline);
      return;
    }
    body.innerHTML = '<div class="kh-drawer-loading">Loading timeline…</div>';
    fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/timeline')
      .then(r => r.json()).then(j => {
        cache.timeline = j.timeline || [];
        body.innerHTML = renderTimelineHTML(cache.timeline);
      });
  }

  function renderTimelineHTML(items) {
    if (!items.length) return '<div class="kh-empty">No history yet.</div>';
    return `<div class="kh-drawer-section"><h3>All Events (${items.length})</h3>
      <div class="kh-timeline">${items.map(i => `
        <div class="kh-timeline-item ${escapeHtml(i.kind)}">
          <div class="ts">${escapeHtml(formatTs(i.ts))} · ${escapeHtml(i.kind)}</div>
          <div class="title">${escapeHtml(i.title || '')}</div>
          <div class="desc">${escapeHtml(i.desc || '')}</div>
        </div>`).join('')}</div></div>`;
  }

  // ───── Tab: Notes ───────────────────────────────────────────────────
  function renderNotesTab(body) {
    body.innerHTML = `
      <div class="kh-drawer-section">
        <h3>Add Note</h3>
        <textarea id="kh-d-noteinput" class="kh-note-input"
          placeholder="Add a note for this defendant (e.g., 'speaks Spanish only', 'court date moved to 5/12')…"></textarea>
        <div class="kh-note-row"><button onclick="window.khDrawer._addNote()">Save Note</button></div>
      </div>
      <div class="kh-drawer-section">
        <h3>Notes (${(cache.notes || []).length})</h3>
        <div id="kh-d-notelist">${renderNotesList(cache.notes)}</div>
      </div>`;
  }

  function renderNotesList(notes) {
    if (!notes || !notes.length)
      return '<div class="kh-empty">No notes yet.</div>';
    return notes.map(n => `
      <div class="kh-note">
        <div class="meta">
          <span>${escapeHtml(n.author || '—')} · ${escapeHtml(formatTs(n.when))}</span>
          <a class="x" onclick="window.khDrawer._delNote(${n.id})" title="Delete">delete</a>
        </div>
        <div class="body">${escapeHtml(n.body)}</div>
      </div>`).join('');
  }

  // ───── Tab: Court ───────────────────────────────────────────────────
  function renderCourtTab(body) {
    body.innerHTML = `
      <div class="kh-drawer-section">
        <h3>Add Court Date</h3>
        <div class="kh-kv">
          <div class="k">Date</div><div class="v" style="padding:0;"><input id="kh-d-cd-date" type="datetime-local" style="width:100%; padding:6px 8px; background:transparent; border:none; outline:none; color:#fff;"></div>
          <div class="k">Court</div><div class="v" style="padding:0;"><input id="kh-d-cd-court" type="text" placeholder="Sessions Court / General / etc." style="width:100%; padding:6px 8px; background:transparent; border:none; outline:none; color:#fff;"></div>
          <div class="k">Notes</div><div class="v" style="padding:0;"><input id="kh-d-cd-notes" type="text" placeholder="Optional" style="width:100%; padding:6px 8px; background:transparent; border:none; outline:none; color:#fff;"></div>
        </div>
        <div class="kh-note-row"><button onclick="window.khDrawer._addCourt()">Add Court Date</button></div>
      </div>
      <div class="kh-drawer-section">
        <h3>Scheduled (${(cache.court || []).length})</h3>
        <div id="kh-d-courtlist">${renderCourtList(cache.court)}</div>
      </div>`;
  }

  function renderCourtList(rows) {
    if (!rows || !rows.length) return '<div class="kh-empty">No court dates scheduled.</div>';
    return rows.map(r => `
      <div class="kh-court-row">
        <div>
          <div class="when">${escapeHtml(formatTs(r.date))}</div>
          <div class="what">${escapeHtml(r.court || '—')}${r.notes ? ' · ' + escapeHtml(r.notes) : ''}</div>
        </div>
        <a class="x" onclick="window.khDrawer._delCourt(${r.id})" title="Remove">×</a>
      </div>`).join('');
  }

  // ───── Tab: History (audit) ─────────────────────────────────────────
  function renderHistoryTab(body) {
    if (cache.audit) {
      body.innerHTML = renderHistoryHTML(cache.audit);
      return;
    }
    body.innerHTML = '<div class="kh-drawer-loading">Loading history…</div>';
    fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/audit')
      .then(r => r.json()).then(j => {
        cache.audit = j.audit || [];
        body.innerHTML = renderHistoryHTML(cache.audit);
      });
  }

  function renderHistoryHTML(rows) {
    if (!rows.length) return '<div class="kh-empty">No edit history yet.</div>';
    return `<div class="kh-drawer-section"><h3>Edit History</h3>${rows.map(r => `
      <div class="kh-audit-row">
        <span class="ts">${escapeHtml(formatTs(r.ts))}</span>
        <div>
          <div class="change"><strong>${escapeHtml(r.col || 'field')}</strong>: ${escapeHtml((r.old || '—').slice(0,80))}<span class="arrow">→</span>${escapeHtml((r.new || '—').slice(0,80))}</div>
          <div class="by">by ${escapeHtml(r.user || 'someone')}</div>
        </div>
      </div>`).join('')}</div>`;
  }

  // ───── Inline edit (Overview only) ──────────────────────────────────
  function wireInlineEdit() {
    document.querySelectorAll('.kh-drawer-body .v[data-editable]').forEach(cell => {
      cell.addEventListener('click', () => beginEdit(cell));
    });
  }

  function beginEdit(cell) {
    if (cell.classList.contains('editing')) return;
    const type = cell.dataset.type;
    const orig = cell.dataset.orig || '';
    cell.classList.add('editing');
    let inputHtml = '';
    if (type === 'select') {
      const opts = JSON.parse(cell.dataset.opts || '[]');
      inputHtml = `<select>${opts.map(o =>
        `<option value="${escapeHtml(o[0])}" ${String(o[0])===String(orig) ? 'selected' : ''}>${escapeHtml(o[1])}</option>`
      ).join('')}</select>`;
    } else if (type === 'bool') {
      inputHtml = `<select>
        <option value="false" ${!orig || orig === 'False' || orig === 'false' ? 'selected' : ''}>— No</option>
        <option value="true"  ${orig === 'True' || orig === 'true' ? 'selected' : ''}>✓ Yes</option>
      </select>`;
    } else if (type === 'date') {
      inputHtml = `<input type="date" value="${toDateInput(orig)}">`;
    } else if (type === 'number') {
      inputHtml = `<input type="number" step="0.01" value="${escapeHtml(orig)}">`;
    } else {
      inputHtml = `<input type="text" value="${escapeHtml(orig)}">`;
    }
    cell.innerHTML = inputHtml + '<span class="save-hint">unsaved</span>';
    const ip = cell.querySelector('input, select');
    ip.focus();
    if (ip.tagName === 'INPUT' && ip.type === 'text') ip.select();
    let committed = false;
    const commit = () => {
      if (committed) return;
      committed = true;
      const newVal = (ip.type === 'checkbox') ? ip.checked : ip.value;
      finishEdit(cell, newVal);
    };
    const cancel = () => {
      if (committed) return;
      committed = true;
      cell.classList.remove('editing');
      restoreCell(cell, orig);
    };
    ip.addEventListener('blur', commit);
    ip.addEventListener('keydown', (e) => {
      if (e.key === 'Enter')  { e.preventDefault(); commit(); }
      if (e.key === 'Escape') { e.preventDefault(); cancel(); }
    });
  }

  function restoreCell(cell, val) {
    const type = cell.dataset.type;
    let display = val;
    if (type === 'bool') {
      display = (val === 'true' || val === true || val === 'True') ? '✓ Yes' : '— No';
    } else if (type === 'select') {
      const opts = JSON.parse(cell.dataset.opts || '[]');
      const match = opts.find(o => String(o[0]) === String(val ?? ''));
      display = match ? match[1] : '';
    }
    const safeDisplay = (display === '' || display == null) ? '—' : escapeHtml(String(display));
    cell.innerHTML = safeDisplay + '<span class="save-hint">unsaved</span>';
    cell.classList.toggle('empty', !display || display === '—' || display === '— No');
  }

  async function finishEdit(cell, newVal) {
    const col = cell.dataset.col;
    const type = cell.dataset.type;
    const orig = cell.dataset.orig || '';
    let payloadVal = newVal;
    if (type === 'bool') payloadVal = (newVal === 'true' || newVal === true);
    if (String(payloadVal) === String(orig)) {
      cell.classList.remove('editing');
      restoreCell(cell, orig);
      return;
    }
    cell.classList.remove('editing');
    cell.classList.add('dirty');
    restoreCell(cell, payloadVal);
    try {
      const r = await fetch('/api/defendants/' + encodeURIComponent(currentIdn), {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ [col]: payloadVal }),
      });
      const j = await r.json();
      if (r.ok && j.ok) {
        cell.dataset.orig = String(payloadVal);
        cell.classList.remove('dirty');
        cache.audit = null;     // bust audit cache
        cache.timeline = null;  // bust timeline cache
        window.khToast(`${col} updated`, 'ok');
      } else {
        cell.classList.remove('dirty');
        restoreCell(cell, orig);
        window.khToast(j.error || 'Save failed', 'err');
      }
    } catch (e) {
      cell.classList.remove('dirty');
      restoreCell(cell, orig);
      window.khToast('Network error', 'err');
    }
  }

  // ───── Side-effects from buttons (notes, tags, court) ───────────────
  window.khDrawer = {
    open: openDrawer,
    close: closeDrawer,
    _addNote: async function () {
      const ta = document.getElementById('kh-d-noteinput');
      const body = (ta?.value || '').trim();
      if (!body) return;
      const r = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/notes', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({body}),
      });
      const j = await r.json();
      if (j.ok) {
        ta.value = '';
        const refr = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/notes').then(r=>r.json());
        cache.notes = refr.notes || [];
        document.getElementById('kh-d-cnt-notes').textContent = cache.notes.length;
        document.getElementById('kh-d-notelist').innerHTML = renderNotesList(cache.notes);
        cache.timeline = null;
        window.khToast('Note saved', 'ok');
      } else {
        window.khToast(j.error || 'Failed', 'err');
      }
    },
    _delNote: async function (id) {
      if (!confirm('Delete this note?')) return;
      await fetch('/api/notes/' + id, { method: 'DELETE' });
      const refr = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/notes').then(r=>r.json());
      cache.notes = refr.notes || [];
      document.getElementById('kh-d-cnt-notes').textContent = cache.notes.length;
      document.getElementById('kh-d-notelist').innerHTML = renderNotesList(cache.notes);
      window.khToast('Deleted', 'ok');
    },
    _addTag: async function () {
      const inp = document.getElementById('kh-d-taginput');
      const label = (inp?.value || '').trim();
      if (!label) return;
      const r = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/tags', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({label}),
      });
      const j = await r.json();
      if (j.ok) {
        inp.value = '';
        loadAndRenderTags();
        if (j.duplicate) window.khToast('Tag already exists', 'err');
        else window.khToast('Tag added', 'ok');
      } else {
        window.khToast(j.error || 'Failed', 'err');
      }
    },
    _delTag: async function (id) {
      await fetch('/api/tags/' + id, { method: 'DELETE' });
      loadAndRenderTags();
    },
    _addCourt: async function () {
      const date = document.getElementById('kh-d-cd-date')?.value;
      const court = document.getElementById('kh-d-cd-court')?.value || '';
      const notes = document.getElementById('kh-d-cd-notes')?.value || '';
      if (!date) { window.khToast('Date required', 'err'); return; }
      const r = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/court_dates', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({court_date: date, court, notes}),
      });
      const j = await r.json();
      if (j.ok) {
        const refr = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/court_dates').then(r=>r.json());
        cache.court = refr.court_dates || [];
        document.getElementById('kh-d-cnt-court').textContent = cache.court.length;
        document.getElementById('kh-d-courtlist').innerHTML = renderCourtList(cache.court);
        document.getElementById('kh-d-cd-date').value = '';
        document.getElementById('kh-d-cd-court').value = '';
        document.getElementById('kh-d-cd-notes').value = '';
        cache.timeline = null;
        window.khToast('Court date added', 'ok');
      } else {
        window.khToast(j.error || 'Failed', 'err');
      }
    },
    _delCourt: async function (id) {
      if (!confirm('Remove this court date?')) return;
      await fetch('/api/court_dates/' + id, { method: 'DELETE' });
      const refr = await fetch('/api/defendants/' + encodeURIComponent(currentIdn) + '/court_dates').then(r=>r.json());
      cache.court = refr.court_dates || [];
      document.getElementById('kh-d-cnt-court').textContent = cache.court.length;
      document.getElementById('kh-d-courtlist').innerHTML = renderCourtList(cache.court);
    },
  };

  // ───── Helpers ──────────────────────────────────────────────────────
  function escapeHtml(s) {
    return String(s ?? '').replace(/[&<>"']/g, c => ({
      '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
    }[c]));
  }
  function toDateInput(v) {
    if (!v) return '';
    const s = String(v).trim();
    let m = s.match(/^(\d{4})-(\d{2})-(\d{2})/);
    if (m) return `${m[1]}-${m[2]}-${m[3]}`;
    m = s.match(/^(\d{1,2})\/(\d{1,2})\/(\d{4})/);
    if (m) return `${m[3]}-${String(m[1]).padStart(2,'0')}-${String(m[2]).padStart(2,'0')}`;
    return '';
  }
  function formatTs(v) {
    if (!v) return '';
    const d = new Date(v);
    if (isNaN(d.getTime())) return v;
    return d.toLocaleString([], { year:'numeric', month:'short', day:'numeric',
                                   hour:'2-digit', minute:'2-digit' });
  }
})();
