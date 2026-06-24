# Case Console — Audit & Cost-Tiered Fix Plan

**Date:** 2026-06-20 · **Scope:** the live `pretrial-knoxc` Go webapp (Case Console) —
`internal/` (compute, db, handlers, auth, emfees, chat, metrics, models), `cmd/server`,
`templates/console_*.html`, `static/console.css` + `app.css`.
**Excluded:** dead `webapp/` Python WIP, `_tracker_work/` scratch, the 3 MB gzip'd tracker bundles.

**Method:** multi-agent audit — 10 dimension reviewers read the real source, every finding was
adversarially re-checked against the code, then synthesized into this plan. 46 candidates → **40
confirmed** (6 rejected as non-issues).

## Baseline health (verified before the audit)
`go build` ✅ · `go vet` ✅ · `gofmt` ✅ · full test suite ✅ · on `main`, only `importcsv_test.go` dirty.
**No crashing or compile defects.** These are latent correctness, money, security, perf, and hygiene issues.

## Headline
- **0 critical, 3 high, 17 medium, 20 low.**
- Two risk clusters: **(1) money-correctness** in the EM-fee / GPS compute layer (overrides + app-added
  clients silently miss arrears letters; custody credited at the wrong rate across a vendor switch), and
  **(2) auth surface** (login open-redirect, password-derived session secret, no login rate-limit).
- One big perf win: the **most-hit page computes the full roster twice per request.**

## Model policy (the cost lever)
| Tier | Model | Use for | Count |
|---|---|---|---|
| Cheap | **Haiku 4.5** (`claude-haiku-4-5`) | mechanical, low-blast-radius: CSS/copy, dead-code deletion, one-line guards, header wrappers | 9 |
| Mid | **Sonnet 4.6** (`claude-sonnet-4-6`) | standard localized engineering: a contained bug fix, new handler/test, SQL tweak, self-contained perf | 21 |
| Premium | **Opus 4.8** (`claude-opus-4-8`) | high-risk / cross-cutting: canonical compute & EM-fee math (JS parity), security/auth, data-integrity/tombstones, concurrency, migrations | 10 |

Rule: **cheapest model that can safely do the fix.** Pay for Opus only where a mistake corrupts data,
breaks tracker parity, or opens a hole.

---

## P0 — correctness / data-integrity / security (do first)

| # | Issue | File(s) | How to fix | Model |
|---|---|---|---|---|
| 1 | **EM-fee overrides ignored for the entire Open list** — supervisor type/status/date corrections only splice into blue-book rows, never the GPS 48-hr rows Pass 1 reads, so a corrected SCRAM→ALLIED rate is silently discarded on legal letters. | `internal/db/emfees.go:16,58` | After `loadOverrides`, before the tombstone filter, add a splice loop applying overrides into `gps48` rows by IDN, limited to `{gps_type, case_status, closed_date}` (mirror the blue-book splice; do NOT touch payment/switch cols). Add a Pass-1 override test. | **opus** |
| 2 | **App-entered OPEN GPS clients never appear in arrears / show-cause letters.** | `internal/db/emfees.go:43`, `internal/emfees/emfees.go:384` | Build a set of IDNs already in `gps48`; for each `added_defendants` row with a GPS install date NOT in that set, synthesize a `gps48`-shaped row (map `warrant_case_num`→`case_number`) and append before the tombstone filter. Existing-set guard prevents double-billing. Tests for once-only. | **opus** |
| 3 | **GPS custody credit applied at one rate across a vendor switch (compute).** Billed $15/day pre-switch but credited $8 → overstates owed. | `internal/compute/compute.go:903,915` | When `hasSwitch`, split custody credit at `switchD` via `CustodyDaysInWindow`: `[start,switchD-1]@rate1 + [switchD,end]@rate2`. Keep flat path otherwise; preserve `v<0` clamp; tiles must not double-count `switchD`. Tests before/after/spanning. Go-only, no JS port. | **opus** |
| 4 | **EM-fee custody credit at single `effRate` across device switch** (engine mirror of #3). | `internal/emfees/emfees.go:449,455,461` | Replace flat `owed -= custodyDays*effRate` with a switch-aware split aligned to `computeOwed` boundaries (pre@`rt`, post@`switchRate`, switch-day @`rt+switchRate`). Assert segments sum to current `custodyDays`. Hand-computed before/after/straddle tests. | **sonnet** |
| 5 | **Login open-redirect** via unvalidated `?next=` (form + JSON body + reflected page). *Merges 3 reports.* | `handlers/handlers.go:106-133`, `admin.go:299-304`, `templates/login.html` | Extract `sanitizeNext(n, def)` (single leading `/`, reject `//` and `/\`) from `safeNext`; apply in `LoginPage` (reflected) and `APILogin` (after the JSON/form value is populated, so both branches are covered). Table test + `TestLoginRedirectSafeNext`. | **sonnet** |
| 6 | **Predictable session secret** derived from (default) `APP_PASSWORD` → forgeable cookies. | `cmd/server/main.go:77-84`, `internal/auth/auth.go:101-110,192-202` | Fail-fast at startup (behind `ALLOW_INSECURE_DEFAULTS`) when password is default / `APP_SESSION_SECRET` empty / <32 bytes; keep password-derived key dev-only. **OPS DEPENDENCY: set `APP_SESSION_SECRET` in ptr1's systemd EnvironmentFile BEFORE this ships or the live service won't start.** Test: cookie minted under key A rejected under key B. | **opus** |
| 7 | **Tombstoned people leak back onto the dashboard** via app-entered court dates / violations / scheduled check-ins. | `handlers/console.go:107-110`, `db/extension.go:222,247`, `db/schedcheckins.go:52` | Add `*Live` db variants filtering by `tomb.whole[idn]` (whole-person granularity); swap the three `Console` calls. Fixes Schedule feed + KPI tiles in one place. Test mirroring `TestAddedDefendantRespectsTombstone`. | **sonnet** |
| 8 | **ClientLedger ignores per-case tombstones** — deleted-case payments leak on the record page. *Merges fix+test.* | `db/ledger.go:72,109-131`, `db/admin.go:384` | In the raw + added payment loops add `if tomb.caseSuppressed(idn, case_number){continue}`. Do NOT filter check-ins (person-scoped; note in header). `TestClientLedgerTombstones` for whole-person + per-case. | **opus** |
| 9 | **L1 missed-initial returns `nextDue=nil`** instead of the initial window (JS parity divergence) — "next due" goes blank for the L1 clients who most need a visit. | `internal/compute/compute.go:532,639` | In the L1 branch: `r := result(level); if !initialMade { r.NextDue = &windows[0] }`. Don't touch L2/L3 or `nextDue()`. Test L1 missed→initial window, L1 made→nil. Re-run `parity_ref` if used. | **opus** |
| 10 | **Re-adding a tombstoned IDN** silently creates an invisible row (no Restore hint). | `db/dataentry.go:91,120` | Before the dup check in `AddDefendant`, `loadTombstones`; if `ts.whole[IDN]` return `errTombstonedIDN` guiding to `/admin/deleted` Restore. No `ON CONFLICT`. Test tombstoned→hint, real dup→`errExistingIDN`. | **sonnet** |

---

## P1 — high-value performance + real bugs + security hardening

| # | Issue | File(s) | How to fix | Model |
|---|---|---|---|---|
| 11 | **Caseload page computes the full behind/missed rosters TWICE per request** (the app's most-hit page). | `handlers/console.go:132-135`, `console_view.go:296-353`, `service.go:263-283` | Have `consoleClientRows` also return the behind/missed sets it already builds; in `ConsoleClients` build `Stats` from `rosterStateCounts` + `len()` and drop the second `computeStats`. Leave shared funcs untouched. Optional parity test. | **sonnet** |
| 12 | **`clients()` cache holds the mutex across the whole rebuild** → stampede/convoy every 60 s TTL miss. | `handlers/handlers.go:67-80`, `db/db.go:120-296` | Serve-stale-while-refresh: fire ONE background rebuild on stale, return stale snapshot immediately, keep old on error; synchronous build only on cold start (or `singleflight`). `clearCache` resets the flag. `-race` test: concurrent stale calls, `BuildClients` runs ≤1×. | **sonnet** |
| 13 | **Static assets served with no `Cache-Control`** → per-navigation 304 revalidation storm on the flaky office WiFi. | `cmd/server/main.go:149-150` | Wrap the `/static/*` FileServer to set `Cache-Control` by extension (immutable 7 d for png/ico/svg/manifest, 1 d json, 1 h css/js). Verify with `curl -sI`. | **haiku** |
| 14 | **Reports page recomputes GPS twice** (computeStats then analytics loop). | `handlers/service.go:330-356,263-268` | In `analyticsData` build behind/missed once, build `Stats` from `rosterStateCounts`+`len()`, keep the single `ComputeGPS` distribution pass. Test counts match the rosters. | **sonnet** |
| 15 | **No rate-limit / lockout on login + HTTP Basic** (shared-password brute force). | `handlers/handlers.go:114-133`, `auth/auth.go:193-202,257-259` | In-memory token bucket (~10/min/key, mutex+prune) keyed by true client IP (CF-Connecting-IP; per-email if shared NAT), as a shared `Authenticator` method called from **both** `APILogin` and `basicAuth`. 429 on exceed. Test (N+1)th rejected even with right password. | **sonnet** |
| 16 | **State-changing GET:** `EMFeeMemo` writes `letter_log` on GET (CSRF-able audit pollution). | `handlers/emfees.go:117-155`, `cmd/server/main.go:218` | Mirror the `memos.zip` pattern: `csrfGuard`'d POST + GET redirect-back. Convert per-row download links to CSRF POST forms. Keep `LogLetters` (no-letter-unlogged invariant). Test GET no-write, POST one row. | **sonnet** |
| 17 | **`Cf-Access-Authenticated-User-Email` header trusted unconditionally** (defense-in-depth gap). | `auth/auth.go:313-319`, `cmd/server/main.go:75` | Gate the header branch on `TRUST_CF_ACCESS_HEADER` (default ON); `log.Fatal` if `LISTEN_ADDR` non-loopback while trust on. Document loopback-only safety. JWT verify deferred. Test trust-off → `""`. | **sonnet** |
| 18 | **Wedged/slow SSE chat client lingers** until TCP timeout, drops messages, leaks a goroutine. | `chat/hub.go:74`, `handlers/chat.go:105,121` | `http.NewResponseController(w).SetWriteDeadline(~5s)` re-armed per write batch; `writeSSE` returns its error; any write/Flush error returns the handler so `Unsubscribe` frees the goroutine (EventSource resyncs via Last-Event-ID). No server-wide WriteTimeout; hub-side eviction must route through `run()`. Tests: fanout non-blocking, deadline returns. | **opus** |

---

## P2 — frontend / UX correctness + quality

| # | Issue | File(s) | How to fix | Model |
|---|---|---|---|---|
| 19 | **Memo FORMTEXT mapping has no validation** — a Word re-save silently misaligns every field (dollar arrearage could land in the GPS-type blank). | `internal/emfees/memo.go:28,55,106` | `fillClusters` returns `(string,error)`; require `totalRuns == 5*len(values)` and exactly `len(values)` full clusters consumed; else descriptive error (blank values still consume a 5-run cluster). Propagate through `fillTemplate`. Regression test on corrupted count. | **sonnet** |
| 20 | **`paidFor` falls back to whole-IDN total on an exactly-net-zero case** (refund/reversal mis-attribution). | `internal/emfees/emfees.go:255` | FIRST resolve parity vs the `past-due-em-fees` skill's `generate_memos.py` truthiness. If intent is "use case sum when the case appears," add a `seenByCase` set and test presence not value. Multi-case net-zero test. | **opus** |
| 21 | **Intake wizard cadence hint drops the mandatory phone/virtual requirement** — teaches officers the wrong (in-person-only) rule. | `templates/console_intake.html:218-223`, `console_help.html:35` | Edit `cadenceText()` L2/L3 strings to mirror the Help page ("one in-person AND one phone/virtual per period"). No backend change. | **haiku** |
| 22 | **Dashboard "Open Violations" KPI** implies a status filter the data model lacks. | `templates/console_dashboard.html:21`, `console_compliance.html:45-56` | Relabel "Open Violations" → "Violations" to match the compliance header. No logic change. Optional `#violations` anchor. | **haiku** |
| 23 | **Compliance roster rows triple-bound for nav** (inline onclick + two keydown loops). | `console_compliance.html:22`, `console_partials.html:200-207,355-358` | Delete the redundant second `tr[data-href]` loop; enhance the survivor (tabindex+role=link, Enter/Space, click); strip inline onclick (compliance + calendar). Don't touch `console_clients.html` (delegated). | **sonnet** |
| 24 | **Bulk check-in modal never restores focus on close** (a11y). | `console_clients.html:313-338`, `console_partials.html:314-328` | Mirror the record-page `cmLastFocus` pattern: capture `activeElement` on open, restore on close. | **haiku** |
| 25 | **Chat fan-out order not tied to `msg_id`** — concurrent sends can render out of order. | `handlers/chat.go:141,146`, `console_partials.html:384-394` | Client-only: in `addMsg` set `dataset.mid` and `insertBefore` the first child with greater mid (else append), preserving dedupe/scroll. No permanent-loss bug exists — don't "fix" one. Assert `addMsg([6,5])`→`[5,6]`. | **sonnet** |
| 26 | **`APILogin` silently ignores JSON decode errors** (poor 400-vs-401 feedback). | `handlers/handlers.go:114-133` | Capture the decode error, return 400 `{"ok":false,"error":"malformed request body"}`. Test bad JSON→400, good login→200/401. | **haiku** |
| 27 | **Open redirect on the 403 "Back" link** via unvalidated `backOr`. | `handlers/admin.go:69-74`, `templates/message.html:15` | One-liner: `backOr` delegates to `safeNext`/`sanitizeNext` (from #5). Sequence after #5 lands. | **haiku** |
| 28 | **`ExportAllData` can emit a corrupt/partial ZIP** after the 200 header is committed. | `handlers/export.go:137-166` | Log on deferred `zw.Close()` error; on per-table `Create` error log+continue; check `cw.Error()` after flush. Don't switch status (header sent). | **haiku** |

---

## P3 — test-coverage guards + hygiene

| # | Issue | File(s) | How to fix | Model |
|---|---|---|---|---|
| 29 | **No test that app writes NEVER touch `raw_*`** (the #1 importer-proofing invariant). | `db/dataentry.go:111-202`, `dataentry_test.go` | `TestAppWritesNeverTouchRaw`: snapshot raw counts, exercise Add/Delete/override/DeletePerson with `importerRetired=false`, assert raw counts unchanged (and added/tombstone counts changed). | **sonnet** |
| 30 | **No direct unit test for `CheckInKind`** (the JS `_ciKind` parity classifier). | `compute.go:584-599`, `compute_test.go` | Table test across the full vocabulary cross-checked vs `tools/parity_ref.py:136-144` (in-person / phone / neither). | **sonnet** |
| 31 | **No relief-switch boundary regression tests** at equal-date edges. | `emfees.go:429-433`, `compute.go:849-853` | Boundary tests only (engines already correct): relief==install / ==asOf; `GpSwitchedDate`==install / ==end. `-run Relief`. | **sonnet** |
| 32 | **Chat retention prune compares offset-bearing RFC3339 strings lexicographically** (DST-fragile; correct today). | `db/chat.go:79,28` | Optional hardening: `strftime('%Y-%m-%dT%H:%M:%SZ', created_at) < ?` with `before.UTC()`. DST-boundary test. | **sonnet** |
| 33 | **Commit the ready `importcsv_test.go` Windows python-discovery fix** (currently dirty). | `handlers/importcsv_test.go:236` | Commit as-is (verifies `<py> --version` before `LookPath`, defeats the Windows Store alias stub). Branch first; Co-Authored-By trailer. | **haiku** |
| 34 | **Delete two stale 3 MB duplicate tracker HTML blobs.** | `PTR Client Lookup v0.82 (SQL-connected).html`, `webapp/lookup/PTR_Client_Lookup.html` | `git rm` both; KEEP `static/lookup/PTR_Client_Lookup.html` (served). Grep for refs, confirm build + tracker load. | **haiku** |
| 35 | **Remove abandoned Flask/FastAPI WIP under `webapp/`** and the dead `ptr-webapp.service` unit. | `webapp/app*.py`, `webapp/queries*.py`, `deploy/ptr-webapp.service` | `git rm` the dead Python app + unit. **KEEP `sharepoint_import.py`, `reconcile_import.py`, `ptr-import.service` (live).** Fix stale docs. **Don't mask ptr1's live Go unit (installed as `ptr-webapp.service`).** | **sonnet** |
| 36 | **Prune untracked scratch/build artifacts** (`_tracker_work/`, `__pycache__`, `deploy/dist`, `server.exe`, `assets`). | working tree | Optional local-only deletion of regenerable, already-ignored scratch. Skip if disk isn't a concern. | **haiku** |

---

## Cost strategy — run in 3 batched sessions

**Session A — one Haiku pass (all mechanical, low blast radius):** #13, #21, #22, #24, #26, #28, #27
(after #5 lands), #33, #34, #36. One `go build && go vet && gofmt -l` at the end covers them all.

**Session B — Sonnet (localized engineering + perf + test guards, nothing parity-critical):** #11, #14,
#12 (+`-race`), #15, #17, #16, #7, #10, #4, #19, #23, #25, and the test guards #29, #30, #31, #32.

**Session C — Opus only, the genuinely high-risk parity/security/data-integrity items:** #1, #2, #3,
#9, #20 (gated on the Python reference), #6 (with the ops env step), #18, #8. The login open-redirect
(#5) is Sonnet because the guard already exists in-repo.

**Verify-after-fix (mandatory, cheap):** every item ships with its own test — after each fix run the
narrowest scope (`go test ./internal/compute/...` etc.); run full `go build && go vet && gofmt -l` once
per session. Baseline is green, so any red is from the change.

**Shared-context groupings that cut setup cost:** the EM-fee items (#1, #2, #4, #19, #20) share
`internal/emfees`+`internal/db`; the two custody rate-splits (#3 compute, #4 emfees) are the same mental
model — do them back-to-back; the auth items (#5, #6, #15, #17) share auth context; the perf dedups
(#11, #14) share the handlers roster context.

## Sequencing / hard dependencies
1. **Land the login open-redirect helper (#5) FIRST** — it extracts `sanitizeNext`; the 403 `backOr` fix
   (#27) depends on it.
2. **#6 has a HARD OPS DEPENDENCY:** `APP_SESSION_SECRET` must be in ptr1's systemd EnvironmentFile
   **before** the fail-fast guard deploys, or the live service won't start. Coordinate with deploy.
3. **#3 + #4 back-to-back** (same custody-rate mental model).
4. **#1 + #2 in one sitting** — both edit `db.EMFees` around the same lines, both insert work before the
   tombstone filter; re-run `./internal/emfees` + `./internal/db` once.
5. **#20 is BLOCKED** on locating + deciding parity vs the `past-due-em-fees` skill's `generate_memos.py`.
6. **#8** = write the per-case-suppression test red first, then the fix.
7. Test-only guards (#29–#32) and hygiene (#33–#36) have no code deps — do them last to validate final state.
8. **#18** (Opus, SSE write-deadline) carefully and alone given the hub's lock-free `run()`-owns-state invariant.

## Quick wins (highest value per token, do today)
- **#22** relabel "Open Violations"→"Violations" — one word, removes a triage-misleading label.
- **#13** static `Cache-Control` — one wrapper, kills the 304 storm on flaky office WiFi.
- **#21** intake cadence copy — stops teaching officers the wrong check-in rule.
- **#5** login open-redirect — reuse the existing `safeNext` predicate; closes a pre-auth phishing pivot.
- **#11** caseload double-roster dedup — halves the most expensive work on the most-hit page.
- **#34 + #33** delete the 3 MB duplicate blobs + commit the ready test fix — instant hygiene, zero risk.
- **#29 + #30** add the `raw_*` guard test + `CheckInKind` parity test — cheap locks on the top invariants.
