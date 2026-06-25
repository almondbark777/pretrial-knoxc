# Model Automation Playbook — pretrial-knoxc

> How to run Claude models in loops and routines to improve and upkeep the
> website, with the right model on the right job at the right cadence.
> Written 2026-06-11. Companion to `CLAUDE.md` (hard rules), `STATUS.md`
> (what's open), `CONSOLE_DASHBOARD.md` / `PROJECT_HISTORY.md` (paper trails).
>
> **Every scheduled prompt below tells the session to read this file first.**
> That makes this document the single place to tune the rules — edit here,
> every future run picks it up.

---

## 1. Where things stand (snapshot, 2026-06-11)

- The Go rewrite is **live on ptr1** (`/health` = `d267ead-20260610`), behind
  cloudflared + Cloudflare Access. Console-only UI; tracker landing at `/`.
- `main` is **2 commits ahead of origin** (`8f8dc77` CSV upload page,
  `d932a41` data-freshness stamp) — push and the next deploy are Alex's calls.
- Ops automation that already exists on ptr1 (do **not** duplicate it in
  Claude routines): daily 11:45 UTC WAL-safe backups + freshness alarm,
  Netdata dashboards, ntfy phone alerts (`ptr-alerts-kc2847xq`), daily
  ~7:10-ET SharePoint import.
- Five autonomous "Routine Runs" already shipped real features (undo-delete,
  EM-fees officer rollup, fee waivers, scheduled check-ins, record row UI).
  The pipeline they used is proven; this playbook formalizes it.
- One review item outstanding: branch `feature/emfees-by-officer`
  (worktree `..\ptr-wt-emfee`) awaits Alex's review.

---

## 2. The constitution — rules every autonomous run obeys

These are non-negotiable regardless of model, mechanism, or cadence.

**Authority boundaries**
1. **Never `git push`.** Commit locally only. Push authority is Alex's alone
   (a past auto-mode push attempt was correctly denied — that denial stands).
2. **Never deploy.** Deploy = Alex runs `scp` + `install-on-ptr1.sh`.
   Routines may run `deploy/build-bundle.sh` and report "bundle ready".
3. **No SSH to ptr1** from tools (key has a passphrase, no agent). Anything
   needing the box goes on the report for Alex.
4. **Respect the skip-list** (§9). Items there need Alex's decision — do not
   "helpfully" build them.

**Code rules (from CLAUDE.md / Brief 5.4)**
5. Native SQLite only; no T-SQL shim; no Azure anything.
6. Never write `raw_*` tables from the app (app-owned `added_*` + extension
   tables only). `/health` stays auth-free.
7. **Parity is sacred**: check-in/GPS/PTR math lives in `internal/compute`
   AND the bundled tracker (`static/lookup/PTR_Client_Lookup.html`, v0.83).
   Any rule change must land in BOTH, and the tracker bundle is only editable
   via the `ptr-client-lookup` skill (gzip+base64 bundle — direct edits will
   corrupt it). When in doubt, don't touch compute in an unattended run.
8. Reuse `compute.CaseTokens`, `FmtOfficer`, ET timestamps (`compute.NowET`).
9. Never commit `.env`, `*.db`, CSVs, or anything with PII.
   `git add <explicit paths>` only — **never `git add -A`**.

**Quality gate (all four, every run that touches code)**
10. `gofmt -l .` (empty) · `go vet ./...` · `go build ./cmd/server` ·
    `go test ./...` (all green). Commit message via a scratch file +
    `git commit -F` (the PowerShell sandbox chokes on path-like strings in
    inline command text).

**Verification (any UI-visible change)**
11. Live-verify in the preview before committing — see gotcha library §11
    for the exact workflow (login endpoint, DOM-eval not screenshots,
    cookie-jar traps, smoke-DB cleanup).

**Paper trail (every run)**
12. Append a dated entry to `CONSOLE_DASHBOARD.md` (console work) or
    `PROJECT_HISTORY.md` (everything else); update `STATUS.md` if the open
    list changed; never recreate `PHASE_*.md` files. Update auto-memory at
    the end of substantive runs.

**Coexistence with Alex**
13. If `git status` shows a dirty tree (Alex's WIP), do NOT commit to `main`.
    Work in an isolated worktree + feature branch
    (`git worktree add ..\ptr-wt-<topic> -b feature/<topic>`), and say so in
    the report. Clean tree → local commits on `main` are the established
    pattern. Remove worktrees after merge/discard.

---

## 3. The machinery — four ways to run, and what each can reach

| Mechanism | Persists? | Can reach | Use for |
|---|---|---|---|
| **Desktop scheduled task** (`/schedule` → local scheduled task) | ✅ survives restarts | Full local machine: repo, Go toolchain, preview server, scratch DB | **The backbone.** All recurring routines R1–R4 |
| **`/loop`** (in-session) | ❌ dies with the session | Everything the session can | Burst work while Alex has a session open ("keep improving every 3h tonight") |
| **`CronCreate`** (session cron) | ❌ session-only, 7-day cap | Same as session | Fallback only; this is what silently died last time — prefer scheduled tasks |
| **Cloud routines** (`/schedule` cloud agents) | ✅ cloud-side | GitHub repo only — **no local preview, no scratch DB, no ptr1** | Narrow: docs-only sweeps or review-style tasks. Most tests pass without the DB (golden tests self-skip), but UI verification is impossible — so no UI work here |

**Inside any single run**, the Agent tool mixes models per subtask
(`model: "haiku" | "sonnet" | "opus" | "fable"`) — see §7. The Workflow tool
(multi-agent orchestration) needs explicit opt-in each time: say
**"use a workflow"** or **"ultracode"**. `/code-review ultra` is the cloud
multi-agent review — user-triggered and billed, never launched by a routine.

---

## 4. Model tier guide — which model for which job

| Model | Character | Assign to |
|---|---|---|
| **Haiku 4.5** | Fast, cheap, literal | Watchdog/report-only sweeps (R1), file searches, log scans, inventory checks. Never lets it *change* code |
| **Sonnet 4.6** | Capable mid-tier | Docs sync, dependency/vuln sweeps, test triage, mechanical refactors, gofmt/lint fixes (R2, R4). Small well-bounded code fixes are fine |
| **Fable 5** | Strong judgment, current daily driver | **Default for the improvement routine (R3)** — picking the right item, full code+test+verify+docs pipeline, orchestrating subagents. Ran Routine Runs 3–5 successfully |
| **Opus 4.8** (+1M ctx, fast mode available) | Heaviest hitter | Parity-critical compute changes, large features, cross-cutting migrations, Phase 6/8 work (R5, R6). Built most of the rewrite. Use 1M context when the task spans many files |

Rules of thumb:
- **Report-only → Haiku. Bounded edits → Sonnet. Judgment + full pipeline →
  Fable. Parity math / big surface area → Opus.**
- Cost scales with both model and cadence: Haiku daily is cheap; Fable/Opus
  daily improvement runs are substantial — weekday-only is the sane default,
  dial up to 3-hourly only during active push periods (via `/loop`, not a
  standing schedule).
- The scheduled task runs on whatever model the session defaults to; the
  prompt itself states its intended tier so the run can delegate down
  (e.g., a Fable session spawning Haiku subagents for the scans).

---

## 5. The routine roster

| # | Name | Cadence | Model | Mechanism | Writes code? |
|---|---|---|---|---|---|
| R1 | Health & hygiene sweep | Daily, 8:00 AM | Haiku 4.5 | Scheduled task | No — report only |
| R2 | Quality & security sweep | Weekly, Mon 8:30 AM | Sonnet 4.6 | Scheduled task | Mechanical fixes only |
| R3 | Improvement run | Weekdays, 9:00 PM | Fable 5 (Opus for heavy picks) | Scheduled task | Yes — full pipeline |
| R4 | Docs & memory sync | Weekly, Fri 5:00 PM | Sonnet 4.6 | Scheduled task | Docs only |
| R5 | Pre-push review gate | On demand | Fable 5 + `/code-review` | Manual | Fix findings |
| R6 | Phase 6 live-data validation | On demand (needs fresh CSVs) | Opus 4.8 | Manual | No |
| R7 | Deep audit | Monthly-ish, opt-in | Fable 5 + Workflow | Manual ("use a workflow") | Findings → R3 backlog |

### R1 — Daily health & hygiene sweep (Haiku, report-only)

Catches drift before it compounds: broken builds, unpushed stacks, PII near
the index, stale bundles, dead worktrees. **Changes nothing.**

**Prompt** (create with: */schedule a daily task at 8:00 AM named "PTR health
sweep" with this prompt*):

```
Read AUTOMATION_PLAYBOOK.md §2 and §11 in C:\Users\alexa\Projects\pretrial-knoxc
first. This is the R1 health sweep: REPORT ONLY — change nothing, commit
nothing, push nothing.

In C:\Users\alexa\Projects\pretrial-knoxc check and report, in this order:
1. Toolchain gate: gofmt -l . ; go vet ./... ; go build ./cmd/server ;
   go test ./... — report pass/fail per step (don't fix).
2. Git: is the tree dirty (whose WIP?); how many commits is main ahead of
   origin/main (list them); any branches besides main/feature/emfees-by-officer;
   any worktrees besides the main checkout and ..\ptr-wt-emfee.
3. PII guard: any tracked or staged *.db / *.csv / .env files
   (git ls-files + git status); flag anything suspicious.
4. Deploy currency: newest file under deploy/dist/ vs HEAD sha — does the
   bundle stamp match HEAD? Last known ptr1 version is in STATUS.md/memory;
   note the gap (you cannot probe ptr1 — never try to SSH).
5. Docs drift: STATUS.md "Last updated" date vs the date of the newest commit.
End with a 5-line TLDR: BUILD ok/broken · TESTS n pass/fail · UNPUSHED n ·
PII clean/flagged · BUNDLE current/stale. If anything is red, lead with it.
```

### R2 — Weekly quality & security sweep (Sonnet, mechanical fixes allowed)

**Prompt** (*/schedule weekly, Mondays 8:30 AM, "PTR quality sweep"*):

```
Read AUTOMATION_PLAYBOOK.md §2 and §11 in C:\Users\alexa\Projects\pretrial-knoxc
first. This is the R2 quality/security sweep. You may apply MECHANICAL fixes
only (gofmt, obvious dead code, doc comments, test hygiene) — no behavior
changes, no compute/ changes, no template/UI changes. Commit locally per §2
rule 10 if you fixed anything; NEVER push.

1. Vulnerabilities: run govulncheck ./... (install
   golang.org/x/vuln/cmd/govulncheck@latest if missing). Report findings with
   exploitability context for THIS app (self-hosted, behind Cloudflare Access).
2. Dependencies: go list -m -u all — report available updates for
   modernc.org/sqlite, go-chi/chi, gorilla/sessions. DO NOT update them
   (deps changes are an R5/Alex decision); just report changelogs/risk.
3. Staleness: grep for TODO/FIXME/XXX in *.go and templates/ — list real ones
   (skip the bundled tracker HTML, it's generated).
4. Test gaps: name the 3 least-tested recently-changed areas
   (git log --since=2.weeks --stat) and suggest, don't write, missing tests.
5. Run the full quality gate (§2 rule 10) and report.
End with a TLDR and, if you committed fixes, the commit sha + one-line diffstat.
```

### R3 — The improvement run (Fable, full pipeline — the proven one)

This is the formalized version of Routine Runs 1–5, which shipped fee
waivers, scheduled check-ins, undo-delete, and the record row-UI restoration.

**Prompt** (*/schedule weekdays 9:00 PM, "PTR improvement run"*):

```
Read CLAUDE.md, AUTOMATION_PLAYBOOK.md (all of §2, §9, §11), and STATUS.md in
C:\Users\alexa\Projects\pretrial-knoxc first. This is the R3 improvement run.

Pick exactly ONE improvement that is autonomously doable end-to-end tonight.
Sources, in priority order:
  a) STATUS.md "What still needs to be done" / nice-to-haves not on the
     skip-list (§9 of the playbook)
  b) "Remaining candidates" noted at the end of recent CONSOLE_DASHBOARD.md
     entries
  c) Something you find yourself: a UX rough edge, missing test, perf win,
     or a11y gap — verified real by reading the code, not guessed.
HARD CONSTRAINTS on the pick: not on the skip-list; no ptr1 access needed;
no compute/-vs-tracker parity change (that's attended-only work); shippable
with tests + live preview verification in one run.

Pipeline (all steps mandatory):
1. If the git tree is dirty, work in a worktree + feature branch (§2 rule 13);
   if clean, work on main.
2. Implement, following existing patterns (look at how pins/waivers/scheduled
   check-ins were built before inventing anything).
3. Tests: unit tests for new logic; extend the pinned-contract tests
   (e.g. TestConsoleRecordRowIDs) when adding row types.
4. Quality gate: gofmt -l / go vet / go build / go test ./... all green.
5. Live-verify in the preview per §11 (login POST /api/login, DOM-eval checks,
   clean up smoke-DB writes or delete db/_smoke_test.db after).
6. Docs: dated entry in CONSOLE_DASHBOARD.md or PROJECT_HISTORY.md; STATUS.md
   if the open list changed.
7. Commit locally (scratch COMMIT_MSG file + git commit -F, explicit paths).
   NEVER push. NEVER deploy.
8. Update auto-memory with what shipped + any new gotchas + candidates you
   spotted for future runs.
Report: what you picked and why, what shipped, commit sha, how you verified
it, and the next 2-3 candidates you'd pick from.
```

**Cadence dial:** weekdays 9 PM is the sustainable default. During an active
push (like the 2026-06-10 marathon), open a session and run
`/loop 3h <the same prompt>` instead — the loop dies with the session, which
is exactly the right blast radius for burst mode.

### R4 — Weekly docs & memory sync (Sonnet, docs only)

**Prompt** (*/schedule weekly, Fridays 5:00 PM, "PTR docs sync"*):

```
Read AUTOMATION_PLAYBOOK.md §2 in C:\Users\alexa\Projects\pretrial-knoxc.
This is the R4 docs sync — documentation files only, no code.

1. STATUS.md: verify every ⬜ item is still real (check the code/git log) and
   every recent ✅ is recorded; fix drift; bump "Last updated".
2. README.md / deploy/DEPLOY_GO.md: spot-check commands and paths still match
   reality (e.g. the deploy bundle contents, env vars in webapp/.env.example).
3. CONSOLE_DASHBOARD.md + PROJECT_HISTORY.md: no action unless cross-refs
   broke; never recreate PHASE_*.md files.
4. AUTOMATION_PLAYBOOK.md §1 snapshot + §9 skip-list/queue: refresh to current
   reality (resolved items out, new decisions in).
5. Run the consolidate-memory skill over auto-memory if the index has grown
   stale or contradictory entries (the project file is huge — merge superseded
   "uncommitted/unpushed" notes into current truth).
Commit docs changes locally per §2 rule 10. NEVER push. Report a diffstat.
```

### R5 — Pre-push review gate (on demand, Fable + /code-review)

Not scheduled. When Alex says "let's push / let's deploy":

1. `/code-review` at **high** effort over `origin/main..main` (or `--fix` to
   apply findings directly).
2. Re-run the full quality gate.
3. `bash deploy/build-bundle.sh` → confirm bundle stamp == HEAD.
4. Hand Alex: the push command, the scp+ssh deploy pair, and the post-deploy
   checklist (`/health` version == HEAD sha · `/console/help` 200 · spot-check
   the feature(s) shipping · roster counts sane vs SharePoint).
5. For a big batch, `/code-review ultra` is available — Alex triggers it
   himself (billed, cloud multi-agent).

### R6 — Phase 6 live-data validation (on demand, Opus 4.8)

The last box on the original plan. Needs fresh SharePoint CSV exports in
Downloads (can't probe ptr1 directly). Run: import the four exports into a
scratch copy, compare roster counts / Behind / Missed / EM-fee totals against
both the Go compute and the offline `past-due-em-fees` skill numbers, and
report discrepancies with named examples. Opus because cross-checking three
implementations of the same math is exactly its lane.

### R7 — Monthly deep audit (opt-in Workflow run)

Once a month or before a milestone, in a live session say:

> "Use a workflow to audit the pretrial-knoxc repo: parallel reviewers over
> security (auth/CSRF/headers/PII paths), correctness (compute vs canonical
> rules), perf (weak-hardware budget), and a11y; adversarially verify every
> finding; output a ranked confirmed-findings list."

Confirmed findings feed the R3 backlog. This is the only routine that uses
multi-agent fan-out, and it requires those explicit words each time.

---

## 6. Setting it up / tearing it down

- **Create**: in any session, `/schedule` + describe the task ("daily at 8am,
  named PTR health sweep, prompt: …" — paste from §5). The schedule skill
  writes a persistent local scheduled task.
- **Inspect**: "list my scheduled tasks" (each task's SKILL.md holds the
  live prompt — edit there or via `/schedule` to tune).
- **Pause/stop**: "disable the PTR improvement run task" / delete it.
- **Burst mode**: open a session, `/loop 3h <R3 prompt>` — auto-stops when
  the session closes.
- Results land in each scheduled run's own session (review in the session
  list). R1's TLDR line is designed to be readable in ten seconds.
- **Deliberately NOT wired**: ntfy pushes from Claude runs. Alex scrapped
  "broadcast every time Claude touches it" on 2026-05-31 — don't resurrect
  without him asking. ptr1's Netdata→ntfy alerts already cover ops.

---

## 7. Mixing models inside one run

The scheduled session's model does the judgment; subagents do tiered labor
via the Agent tool's `model` parameter:

- **Scans down to Haiku**: "spawn an Explore agent (model: haiku) to inventory
  every template that renders payment rows" — cheap breadth.
- **Bounded edits down to Sonnet**: test-file scaffolding, doc edits, lint
  fixes inside a bigger R3 run.
- **Hard cores up to Opus**: if an R3 pick turns out to touch
  `internal/compute` (it shouldn't — skip-listed unattended), or a migration
  spans many files, the run should STOP and report "this needs an attended
  Opus session" rather than downgrade quality.
- Parallel independent subagents go in one message (they run concurrently);
  results come back to the orchestrator, which owns the commit and the gate.

---

## 8. Human checkpoints — what stays with Alex, always

| Checkpoint | Why |
|---|---|
| `git push` | Standing rule; auto-mode denial on record |
| Deploy to ptr1 (scp + install-on-ptr1.sh) | Production box; SSH is his |
| Merging routine branches (`feature/emfees-by-officer`) | His review queue |
| Dependency upgrades | Behavior risk; R2 reports only |
| Anything on the skip-list (§9) | Needs his design/policy decision |
| `/code-review ultra`, Workflow runs | Billed / heavyweight — explicit opt-in |
| Compute-math changes | Parity-critical; attended sessions only |

Weekly rhythm that makes this work: R1 keeps him informed daily; he reviews
the R3 commit stack whenever convenient; push + deploy when the stack looks
good (R5 gates it); decision items get answered when they get answered.

---

## 9. Skip-list & decision queue (refresh via R4)

**Skip-list — routines must NOT attempt (need Alex's decision):**
- Document upload (file-storage decision)
- DB-backed allow-list (design sign-off; currently `ALLOWED_EMAILS` env)
- Reminder sending channel (provider decision; reminders are log-only by design)
- Structured intake capture (currently one packed intake_summary note)
- Roles/conditions/templates admin config tables (console Admin placeholders)
- Import cadence bump on ptr1 (infra, his hands)
- DB rename `kh222.db → pretrial_release.db` (coordinated unit edits on ptr1)
- Phase 8 HA (rqlite plan in `deploy/HA_PLAN.md`, end of testing phase)
- `feature/emfees-by-officer` (awaiting his review — don't rebase/merge/redo)
- Any `internal/compute` ⇄ tracker-bundle parity change (attended only)

**Open decision queue for Alex (surfaced, not blocking):**
- Push + deploy the current 2-commit stack (`8f8dc77`, `d932a41`)
- "Payments collected since go-live" additive metric — wanted?
- Phase 6 validation pass on live data (R6 — needs fresh CSV exports)

---

## 10. Current improvement backlog (R3 feedstock, 2026-06-11)

Autonomously doable, in rough value order — R3 may also find its own:
- EM-fees report: xlsx export option (noted optional follow-up in Phase 9)
- EM-fees: CSV-upload path for a fresher export (picks up Switched-To/COURT
  columns the daily import lacks)
- Roster: true server-side paging/windowing follow-up for the ~3,300-row live
  case (client windowing shipped; server paging parked)
- ~~Letters: surface letter history beyond the record timeline (e.g. an Admin
  report of recent generation events from `letter_log`)~~ **DONE 2026-06-25**:
  `/reports/letters` + `/export/letters.csv` (cross-client show-cause-letter
  history). See `CONSOLE_DASHBOARD.md` 2026-06-25.
- Help page: keep `/console/help` in sync as features land (cheap, recurring)
- Test depth: handlers still under-pinned vs db/compute (R2 names candidates)
- A11y sweep of newer modals (waiver/schedule/import) against the older ones

---

## 11. Gotcha library (hard-won — read before any run)

**Preview / verification**
- Launch: `.claude/preview-server.cmd` — port 8099, scratch `db/_smoke_test.db`
  (regenerates from kh222.db when deleted), `APP_PASSWORD=test`.
- Login is `POST /api/login` (NOT /login).
- Big pages (console dashboard/roster) **time out screenshots** — rasterization
  only; the page is fine. Verify with DOM-eval (`preview_eval`) instead.
- Cookie jars are **per-host**: localhost vs 127.0.0.1 differ. A stale
  `ptc_asof` cookie silently time-travels every number — check/clear
  `document.cookie` before trusting KPIs.
- Setting a cookie + `window.location.href` in one `preview_eval` does NOT
  persist the cookie — set it, then `location.reload()` in the same context.
  Also `location.reload()` can land on `/` — navigate explicitly.
- The preview panel's native width hides the `.cfresh` chip <860px — resize
  to ~1380px before visual checks of the topbar.
- E2E writes land in the smoke DB — clean them up via the UI or delete
  `db/_smoke_test.db` to regenerate clean.

**Git / shell**
- Long commit messages: write to a scratch file, `git commit -F` — the
  sandbox blocks commands whose TEXT contains path-like strings (a
  "/dashboard" inside an inline message triggered it).
- `git add` explicit paths only (a `git add -A` once swept a scratch
  COMMIT_MSG.txt into history).
- Stale zero-byte `.git/index.lock` with no git process running → safe to
  delete.
- ssh to ptr1 needs `-t` for sudo prompts; stale extracted bundles on the box
  have nearly caused old-code deploys — `rm -rf ptr1-deploy` + re-scp, and
  verify `ls ptr1-deploy/sharepoint_import.py` (only post-d267ead bundles
  carry it). [Alex-run steps — routines never SSH.]

**Code patterns**
- Tracker bundle = gzip+base64; **cannot grep the HTML for source**. Edit only
  via the `ptr-client-lookup` skill; verify by loading in-browser
  (cache-busts hard) and checking `window._ciKind` / version.
- Go `html/template` pads JS-interpolated values: `cmOutcome({{.ID}})` renders
  `cmOutcome( 1 )` — regexes must allow `\s*`.
- Bare `Open()+EnsureSchema` test DBs lack migration-001-only tables
  (`defendant_documents`) → purge tests must use the `openEnsured` fixture.
- New extension tables: add to migration file AND `ensureSchemaSQL` AND
  `extensionTablesByIDN` (purge on person delete) — the established triple.
- JS: `form.name` returns the form's name attribute, not the input named
  "name" — the intake wizard uses `name="defendant"` for this reason.
