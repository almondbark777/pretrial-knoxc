# Production HA plan (two-server failover)

> Formerly `PHASE_8_HA.md` (moved here 2026-06-10 when the phase paper trails
> were consolidated into `PROJECT_HISTORY.md` — this one is a future plan, not
> history). **Design + runbook only — do NOT implement until the testing phase
> ends** (the single-file SQLite setup is correct and ideal for testing).
> Decision locked with Alex 2026-05-30; see Brief Part 4.9.

---

## Goal

Two servers up at all times. If one fails, work continues on the other while it's
fixed or replaced — **without manual steps, without losing writes, and without
making the website's correction features (Phase 7) any harder to use.**

This is **active-passive / warm-standby** HA. Not active-active.

## Decision: rqlite (3 voting nodes) + Cloudflare Load Balancing

| Concern | How it's handled |
|---|---|
| Shared state | rqlite replicates SQLite across nodes via **Raft** (no shared/NFS file — that corrupts). |
| Automatic failover | 3 voting nodes tolerate 1 failure; survivors elect a new leader in seconds. |
| Split-brain | Impossible with an odd node count + quorum (2-of-3). The **witness** node is what makes 2 servers safe. |
| Data loss | **Zero** — a write is committed only once a quorum has it (Raft). |
| "Where do writes go?" | rqlite **forwards every write to the leader**, so neither the app nor the importer needs to know who's primary. Nothing moves on failover. |
| Traffic routing | Cloudflare Load Balancing health-checks both origins on `/health`, fails over automatically. |

### Why not the alternatives
- **Litestream** — async backup + **manual/scripted** promotion on failure. Not painless.
- **LiteFS** — automatic, but needs FUSE (a mount that can wedge) + a lease service,
  and a direct file-writer like `sharepoint_import.py` must run on the primary and
  **move on failover**. rqlite removes that headache entirely.

## Topology

```
            Cloudflare Load Balancer  (health-check /health, auto-failover)
                        │
            ┌───────────┴───────────┐
        Server A                 Server B                Witness C (tiny)
   ptr-webapp (Go)          ptr-webapp (Go)         rqlite only (vote-only,
   + rqlite node            + rqlite node            no app, no real load)
        └──────── Raft consensus (3 voting members) ────────┘
   importer (cron)  ──writes──►  any rqlite node ──forwarded──►  leader
```

- A and B each run the Go binary **and** a local rqlite node; the app connects to
  `localhost` rqlite. C runs only rqlite for quorum.
- If A dies: B+C keep quorum → B's node leads → Cloudflare routes users to B →
  reads and **corrections** keep working. Fix A, restart it, it rejoins and catches
  up automatically.

## Code & ops changes (bounded, one-time)

1. **`internal/db/db.go` `Open()`** — swap `modernc.org/sqlite` file-open for
   rqlite's `database/sql` driver (e.g. `gorqlite`/its stdlib driver) pointed at the
   local node. **Queries are already native SQLite, so they port unchanged**; only
   the connection string + driver import move. `EnsureSchema`, `BuildClients`, the
   admin writes, and the 60s cache all keep working as-is.
   - Set read-consistency to `weak`/`none` for the hot read paths (lookup, rosters,
     grid) since they're already behind a 60s cache and a tiny staleness is fine;
     keep writes at the default (leader, strong).
2. **`sharepoint_import.py`** — the **one writer that must change**. It currently
   writes the SQLite file directly; under rqlite it must write through the cluster
   (HTTP API via `pyrqlite`, or the rqlite CLI). It can target *any* node (forwarded
   to the leader), so it doesn't need to know who's primary. The full Sunday reload
   becomes a batch of statements through rqlite. **This is the highest-risk piece —
   test it in isolation against a scratch cluster before cutover.**
3. **Cloudflare** — add a Load Balancer with the two tunnel origins, health monitor
   on `/health` (already auth-free), automatic failover, low TTL.
4. **Backups (Phase 5)** — repoint at rqlite's `/db/backup` endpoint, which returns
   a plain SQLite file; the existing online-backup script logic still applies.
5. **systemd** — add an `rqlite.service` per node (the unit style mirrors
   `ptr-webapp`/`ptr-import`); `ptr-webapp` depends on the local rqlite being up.

## What does NOT change

- The Phase 7 admin/data-entry UX — Delete / restore / override / notes all behave
  identically for officers and become durable across a node failure.
- The business math, templates, auth, routes — untouched.
- The `IMPORTER_RETIRED` flag still works (physical delete just runs through rqlite).

## Risks / watch-items

- **Importer rewrite** is the real work and the main risk — gate cutover on a clean
  isolated test of the daily incremental **and** the Sunday full reload via rqlite.
- **rqlite feature limits** (no `ATTACH`, statement-based writes, some pragmas) —
  none used by this app at ~3,500 rows/day; re-confirm during the spike.
- **Witness availability** — if the witness *and* one server are both down, the
  cluster loses quorum (read-only). Keep the witness boringly reliable.
- **Clock skew / network partition** between A, B, C — Raft handles it, but keep the
  three nodes on the same LAN/low-latency link where possible.

## Failover runbook (one page)

**Normal:** all three nodes up; one leader; Cloudflare → whichever app server is healthy.

**A server dies (expected case):**
1. Cloudflare health check fails → traffic auto-routes to the surviving app server. *(no action)*
2. rqlite re-elects a leader among the survivors → writes continue. *(no action)*
3. Officers keep working — lookups, deletes, overrides, notes all function.
4. **You:** fix/replace the dead box at leisure. Start its `ptr-webapp` + `rqlite`
   services; the node **rejoins and catches up automatically**. Confirm with
   `rqlite status`/`/status` showing 3 healthy members and `curl /health` on the
   rejoined app.

**Witness dies:** cluster still has quorum (2 of 3) — no impact. Replace the witness
when convenient.

**Two of three down:** cluster goes read-only (no quorum) to protect consistency —
the site still *serves reads*; corrections pause until a second node returns. This is
the safe behavior, by design.

## Done when

Killing either app server leaves the site **fully usable — reads and corrections —
with no manual step and no lost writes**, the dead node rejoins cleanly when
restarted, the importer runs through rqlite, Cloudflare fails traffic over
automatically, and a recorded **failure drill** proves it. Capture the drill output +
final config here.

---

*Status: planned. Not started — production-cutover task, after testing. No code
changed yet.*
