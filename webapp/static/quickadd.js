/* ─────────────────────────────────────────────────────────────────────────
 * Quick-Entry Modal — one shared dialog for logging a check-in or a payment
 * (and jumping to the full edit page) from ANYWHERE in the app.
 *
 * Public API:
 *   window.PTRQuick.open({ idn, name, caseNum, mode, onSaved })
 *     idn      — defendant IDN. If omitted, the modal shows a search step first.
 *     name     — display name (optional; fetched/labelled if omitted).
 *     caseNum  — case number to stamp on the record (optional).
 *     mode     — 'checkin' (default) | 'payment' | 'edit'.
 *                'edit' with an idn jumps straight to /edit_defendant.html.
 *     onSaved  — callback(kind, payload) fired after each successful save
 *                (used by the drawer to refresh itself).
 *   window.PTRQuick.close()
 *
 * Reuses the same POST endpoints as the Log Activity page:
 *   POST /api/check_ins  { idn, defendant, check_in_date, type_of_check_in, supervising_officer }
 *   POST /api/payments   { idn, defendant, payment_date, payment_amount, payment_type, officer }
 * ───────────────────────────────────────────────────────────────────────── */
(function () {
  if (window.PTRQuick) return;

  // ── local toast fallback (drawer.js usually provides window.khToast) ──
  function toast(msg, kind) {
    if (window.khToast) return window.khToast(msg, kind);
    let t = document.getElementById('kh-toast');
    if (!t) {
      t = document.createElement('div');
      t.id = 'kh-toast'; t.className = 'kh-toast';
      document.body.appendChild(t);
    }
    t.textContent = msg;
    t.className = 'kh-toast show ' + (kind || 'ok');
    clearTimeout(t._timer);
    t._timer = setTimeout(() => { t.className = 'kh-toast ' + (kind || 'ok'); }, 3500);
  }

  function escapeHtml(s) {
    return String(s ?? '').replace(/[&<>"']/g, c => ({
      '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
    }[c]));
  }
  function nowLocal() {
    const n = new Date();
    return new Date(n.getTime() - n.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  }

  // ── State ──
  let target = null;        // { idn, name, caseNum, status }
  let pendingMode = 'checkin';
  let onSavedCb = null;
  let me = '';              // logged-in officer (whoami)
  const sessionLog = [];

  // ── Build DOM ──
  const scrim = document.createElement('div');
  scrim.className = 'pq-scrim';

  const modal = document.createElement('div');
  modal.className = 'pq-modal';
  modal.setAttribute('role', 'dialog');
  modal.setAttribute('aria-label', 'Quick entry');
  modal.innerHTML = `
    <div class="pq-head">
      <div class="pq-who">
        <div class="pq-kicker">Quick Entry</div>
        <div class="pq-name" id="pq-name">Find a defendant</div>
        <div class="pq-meta" id="pq-meta"></div>
      </div>
      <button class="pq-x" id="pq-x" aria-label="Close (Esc)">&times;</button>
    </div>

    <!-- SEARCH STEP -->
    <div class="pq-body" id="pq-search-step">
      <div class="pq-search-label">Find defendant by name or IDN</div>
      <input id="pq-search" class="pq-search-input" type="text" autocomplete="off"
             placeholder="Start typing a name or IDN…">
      <div id="pq-results" class="pq-results"></div>
    </div>

    <!-- ENTRY STEP -->
    <div id="pq-entry-step" style="display:none; flex-direction:column; min-height:0;">
      <div class="pq-tabs">
        <button class="pq-tab" data-pane="checkin">Check-In</button>
        <button class="pq-tab" data-pane="payment">Payment</button>
      </div>
      <div class="pq-body">
        <!-- CHECK-IN -->
        <div class="pq-pane" data-pane="checkin">
          <form id="pq-ci-form">
            <div class="pq-grid">
              <div class="pq-field">
                <label for="pq-ci-date">Date / Time</label>
                <input id="pq-ci-date" name="check_in_date" type="datetime-local" required>
              </div>
              <div class="pq-field">
                <label for="pq-ci-type">Type</label>
                <select id="pq-ci-type" name="type_of_check_in" required>
                  <option value="In Person">In Person</option>
                  <option value="Phone">Phone</option>
                  <option value="Office Visit">Office Visit</option>
                  <option value="Field">Field</option>
                  <option value="Virtual">Virtual</option>
                </select>
              </div>
            </div>
            <div class="pq-field">
              <label for="pq-ci-officer">Supervising Officer</label>
              <input id="pq-ci-officer" name="supervising_officer" type="text"
                     placeholder="firstname.lastname@knoxsheriff.org">
            </div>
          </form>
        </div>
        <!-- PAYMENT -->
        <div class="pq-pane" data-pane="payment">
          <form id="pq-pm-form">
            <div class="pq-grid">
              <div class="pq-field">
                <label for="pq-pm-date">Date / Time</label>
                <input id="pq-pm-date" name="payment_date" type="datetime-local" required>
              </div>
              <div class="pq-field">
                <label for="pq-pm-amount">Amount ($)</label>
                <input id="pq-pm-amount" name="payment_amount" type="number" step="0.01" min="0"
                       placeholder="20.00" required>
              </div>
            </div>
            <div class="pq-grid">
              <div class="pq-field">
                <label for="pq-pm-type">Payment Type</label>
                <select id="pq-pm-type" name="payment_type" required>
                  <option value="Ptr">PTR Fee</option>
                  <option value="Allied">Allied</option>
                  <option value="GPS">GPS</option>
                  <option value="SCRAM">SCRAM</option>
                  <option value="Install Fee">Install Fee</option>
                  <option value="Bond">Bond</option>
                  <option value="Other">Other</option>
                </select>
              </div>
              <div class="pq-field">
                <label for="pq-pm-officer">Officer Collecting</label>
                <input id="pq-pm-officer" name="officer" type="text"
                       placeholder="firstname.lastname@knoxsheriff.org">
              </div>
            </div>
          </form>
        </div>
      </div>
      <div class="pq-session" id="pq-session" style="display:none;">
        <h4>Logged this session</h4>
        <div id="pq-session-list"></div>
      </div>
      <div class="pq-foot">
        <a class="pq-edit-link" id="pq-edit-link" href="#">✏ Edit full record ↗</a>
        <label class="pq-keepopen" title="Stay open to log several in a row">
          <input type="checkbox" id="pq-keepopen" checked> Keep open
        </label>
        <button class="pq-btn" id="pq-save">Save</button>
      </div>
    </div>
  `;

  function mount() {
    if (modal._mounted) return;
    modal._mounted = true;
    document.body.appendChild(scrim);
    document.body.appendChild(modal);
    scrim.addEventListener('click', close);
    modal.querySelector('#pq-x').addEventListener('click', close);
    modal.querySelectorAll('.pq-tab').forEach(t =>
      t.addEventListener('click', () => showPane(t.dataset.pane)));
    modal.querySelector('#pq-save').addEventListener('click', save);
    modal.querySelector('#pq-ci-form').addEventListener('submit', e => { e.preventDefault(); save(); });
    modal.querySelector('#pq-pm-form').addEventListener('submit', e => { e.preventDefault(); save(); });
    const search = modal.querySelector('#pq-search');
    let timer = null, lastQ = '';
    search.addEventListener('input', () => {
      const q = search.value.trim();
      clearTimeout(timer);
      if (q.length < 2) { modal.querySelector('#pq-results').innerHTML = ''; return; }
      timer = setTimeout(async () => {
        lastQ = q;
        try {
          const r = await fetch('/api/lookup?q=' + encodeURIComponent(q) + '&limit=10');
          const j = await r.json();
          if (j.q !== lastQ) return;
          renderResults(j.results || []);
        } catch { renderResults([]); }
      }, 200);
    });
    // Esc closes the modal (and stops the event reaching the drawer underneath)
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && modal.classList.contains('open')) {
        e.stopPropagation();
        close();
      }
    }, true);
  }

  function renderResults(rows) {
    const box = modal.querySelector('#pq-results');
    if (!rows.length) {
      box.innerHTML = '<div class="pq-empty">No matches. (Use the Referral page to add someone new.)</div>';
      return;
    }
    box.innerHTML = rows.map((r, i) => `
      <div class="pq-row" data-i="${i}">
        <div>
          <div class="pq-row-name">${escapeHtml(r.name || ('IDN ' + r.idn))}</div>
          <div class="pq-row-meta">IDN ${escapeHtml(r.idn)}${r.caseNum ? ' · ' + escapeHtml(r.caseNum) : ''}${r.officer ? ' · ' + escapeHtml(r.officer) : ''}</div>
        </div>
        <span class="pq-pill ${r.active ? 'open' : 'closed'}">${escapeHtml(r.status || '—')}</span>
      </div>`).join('');
    [...box.querySelectorAll('.pq-row')].forEach((el, i) => {
      el.addEventListener('click', () => {
        const r = rows[i];
        setTarget({ idn: r.idn, name: r.name, caseNum: r.caseNum, status: r.status });
        if (pendingMode === 'edit') { gotoEdit(); return; }
        showEntry();
      });
    });
  }

  function setTarget(t) {
    target = t;
    modal.querySelector('#pq-name').textContent = t.name || ('IDN ' + t.idn);
    modal.querySelector('#pq-meta').textContent =
      `IDN ${t.idn}` + (t.caseNum ? ' · ' + t.caseNum : '') + (t.status ? ' · ' + t.status : '');
    modal.querySelector('#pq-edit-link').href = '/edit_defendant.html?idn=' + encodeURIComponent(t.idn);
  }

  function gotoEdit() {
    if (!target) return;
    window.location.href = '/edit_defendant.html?idn=' + encodeURIComponent(target.idn);
  }

  function showSearch() {
    modal.querySelector('#pq-search-step').style.display = 'block';
    modal.querySelector('#pq-entry-step').style.display = 'none';
    modal.querySelector('#pq-name').textContent = 'Find a defendant';
    modal.querySelector('#pq-meta').textContent = '';
    const s = modal.querySelector('#pq-search');
    s.value = '';
    modal.querySelector('#pq-results').innerHTML = '';
    setTimeout(() => s.focus(), 60);
  }

  function showEntry() {
    modal.querySelector('#pq-search-step').style.display = 'none';
    const step = modal.querySelector('#pq-entry-step');
    step.style.display = 'flex';
    // default both date fields to now; prefill officer with whoami
    modal.querySelector('#pq-ci-date').value = nowLocal();
    modal.querySelector('#pq-pm-date').value = nowLocal();
    if (me) {
      const ci = modal.querySelector('#pq-ci-officer');
      const pm = modal.querySelector('#pq-pm-officer');
      if (!ci.value) ci.value = me;
      if (!pm.value) pm.value = me;
    }
    showPane(pendingMode === 'payment' ? 'payment' : 'checkin');
  }

  let activePane = 'checkin';
  function showPane(pane) {
    activePane = pane;
    modal.querySelectorAll('.pq-tab').forEach(t =>
      t.classList.toggle('active', t.dataset.pane === pane));
    modal.querySelectorAll('.pq-pane').forEach(p =>
      p.classList.toggle('active', p.dataset.pane === pane));
    const focusId = pane === 'payment' ? '#pq-pm-amount' : '#pq-ci-type';
    setTimeout(() => modal.querySelector(focusId)?.focus(), 60);
  }

  async function save() {
    if (!target) { toast('Select a defendant first', 'err'); return; }
    const saveBtn = modal.querySelector('#pq-save');
    const isPayment = activePane === 'payment';
    const form = modal.querySelector(isPayment ? '#pq-pm-form' : '#pq-ci-form');
    if (!form.reportValidity()) return;

    const body = { idn: target.idn, defendant: target.name || null };
    if (target.caseNum) body.case_number = target.caseNum;
    new FormData(form).forEach((v, k) => { body[k] = v; });

    saveBtn.disabled = true;
    const endpoint = isPayment ? '/api/payments' : '/api/check_ins';
    try {
      const r = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const j = await r.json();
      if (r.ok && j.ok) {
        const kind = isPayment ? 'payment' : 'checkin';
        toast(isPayment ? 'Payment saved' : 'Check-In saved', 'ok');
        addSession(kind, body);
        form.reset();
        if (onSavedCb) { try { onSavedCb(kind, body); } catch {} }
        if (modal.querySelector('#pq-keepopen').checked) {
          // reset transient fields, keep officer + defendant for fast repeat
          modal.querySelector('#pq-ci-date').value = nowLocal();
          modal.querySelector('#pq-pm-date').value = nowLocal();
          if (me) {
            modal.querySelector('#pq-ci-officer').value = me;
            modal.querySelector('#pq-pm-officer').value = me;
          }
          showPane(activePane);
        } else {
          close();
        }
      } else {
        toast(j.error || 'Save failed', 'err');
      }
    } catch {
      toast('Network error', 'err');
    } finally {
      saveBtn.disabled = false;
    }
  }

  function addSession(kind, body) {
    sessionLog.unshift({ kind, body, t: new Date() });
    const box = modal.querySelector('#pq-session');
    box.style.display = 'block';
    modal.querySelector('#pq-session-list').innerHTML = sessionLog.slice(0, 8).map(e => {
      const t = e.t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      const detail = e.kind === 'payment'
        ? '$' + (e.body.payment_amount || '0') + ' · ' + (e.body.payment_type || '')
        : (e.body.type_of_check_in || '');
      return `<div class="pq-session-row">
        <span class="tag ${e.kind}">${e.kind}</span>
        <span>${escapeHtml(e.body.defendant || ('IDN ' + e.body.idn))}</span>
        <span style="color:var(--muted);">${escapeHtml(detail)}</span>
        <span class="t">${t}</span>
      </div>`;
    }).join('');
  }

  function open(opts) {
    opts = opts || {};
    pendingMode = opts.mode || 'checkin';
    onSavedCb = opts.onSaved || null;
    mount();

    // fetch logged-in officer once (best effort)
    if (!me) {
      fetch('/api/whoami').then(r => r.json()).then(d => {
        if (d && d.user) {
          me = d.user;
          if (target) {
            const ci = modal.querySelector('#pq-ci-officer');
            const pm = modal.querySelector('#pq-pm-officer');
            if (ci && !ci.value) ci.value = me;
            if (pm && !pm.value) pm.value = me;
          }
        }
      }).catch(() => {});
    }

    // reset session log per open
    sessionLog.length = 0;
    modal.querySelector('#pq-session').style.display = 'none';
    modal.querySelector('#pq-session-list').innerHTML = '';
    modal.querySelector('#pq-ci-form').reset();
    modal.querySelector('#pq-pm-form').reset();

    scrim.classList.add('open');
    modal.classList.add('open');
    document.body.style.overflow = 'hidden';

    if (opts.idn) {
      if (pendingMode === 'edit') {
        setTarget({ idn: opts.idn, name: opts.name, caseNum: opts.caseNum, status: opts.status });
        gotoEdit();
        return;
      }
      setTarget({ idn: opts.idn, name: opts.name, caseNum: opts.caseNum, status: opts.status });
      showEntry();
    } else {
      target = null;
      showSearch();
    }
  }

  function close() {
    modal.classList.remove('open');
    scrim.classList.remove('open');
    document.body.style.overflow = '';
  }

  window.PTRQuick = { open, close };
})();
