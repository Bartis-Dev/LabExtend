# labextend — Implementation Plan

Phases ending with a working/testable deliverable. Effort estimates are working days for one developer using Claude as pair.

---

## Phase 1 — Skeleton & dev loop — DONE

Repo bootstrapped, builds, `docker build` succeeds.

## Phase 2 — Config, DB, migrations — DONE

Leader opens SQLite, applies migrations via goose, auto-generates secrets in `${DATA_DIR}/.env.generated`.

## Phase 3 — Proto + gRPC scaffold — DONE

Agent connects, Hello/Welcome/Heartbeat exchanged. `node_metrics` updated on every heartbeat.

## Phase 4 — Auth + Setup wizard — DONE

Argon2id, opaque sessions, CSRF middleware, setup wizard, login/logout, /api/me.

---

## Phase 5 — Frontend shell + monitoring UI (NOW)

**Goal:** clickable shell with setup wizard, login, dashboard listing all nodes with live CPU/RAM/Disk bars, sidebar nav.

Files:
- `frontend/app/globals.css` — Tailwind directives + design tokens.
- `frontend/app/(authed)/layout.tsx` — sidebar shell, auth guard, SSE provider.
- `frontend/app/setup/page.tsx`, `frontend/app/login/page.tsx` — fully wired.
- `frontend/app/page.tsx` — boot router (decides setup/login/dashboard based on /api/setup/status + /api/me).
- `frontend/app/(authed)/dashboard/page.tsx` — node grid with bars.
- `frontend/components/ui/*` — minimal shadcn-style primitives (button, input, card, bar, badge).
- `frontend/lib/api.ts`, `frontend/lib/sse.ts` — already started, wired to setup token + SseProvider.

Acceptance:
- Fresh boot → `/` redirects to `/setup`; submitting wizard → `/dashboard` with 1+ online node.
- Dashboard shows live CPU/RAM/Disk percentage bars and network rx/tx per node, updating ~5s via SSE.
- Logout returns to `/login`; re-login resumes at `/dashboard`.

---

## Phase 6 — Node metrics (Beszel replacement) + alert rules

**Goal:** retain per-node metrics for short window (last 60min @ 5s = 720 samples, in-memory ringbuffer) + alert rules with hysteresis + Discord webhook on trigger/recover.

Files:
- `proto/manager.proto` — extend `Heartbeat` with `cpu_percent`, `net_rx_bytes`, `net_tx_bytes`, `disk_read_bytes`, `disk_write_bytes`; counters are cumulative since boot.
- `internal/agent/heartbeat_linux.go` — read `/proc/stat`, `/proc/net/dev`, `/proc/diskstats`; deltas against previous sample for CPU %.
- `internal/leader/metrics_store.go` — ringbuffer per node, recent-sample query, simple rolling averages.
- `internal/leader/alert_engine.go` — rule evaluator with state (over-threshold-since-ts) per rule; emits trigger/recover events.
- `internal/leader/handlers_alerts.go` — CRUD `/api/alert-rules`, `/api/webhooks`, `/api/alert-history`.
- `internal/discord/webhook.go` — implement Post() with HTTP retry on 429.
- Migration `0002_monitoring.sql` — `alert_rules`, `alert_history`, `metric_snapshots_1m` (downsampled minute buckets retained 24h), extend `node_metrics` with new cols.

Acceptance:
- Dashboard bars update from new metrics within 5s.
- Add rule "CPU > 80% for 60s on any node" → cap CPU on a node with `stress` → Discord fires within ~65s; release stress → recover post within 30s.
- `/api/alert-history` shows trigger + recover rows.

Effort: 2d.

---

## Phase 7 — Container discovery + metrics + state (Portainer-light)

**Goal:** discover all containers per node, sample metrics every 5s, classify state (active/restarting/crashed/stopped), expose in UI.

Files:
- `proto/manager.proto` — `ContainerSnapshot`, `ContainerMetric`, new `Heartbeat.containers` field OR a separate `ContainerReport` event.
- `internal/agent/docker_client.go` — talk to `/var/run/docker.sock` via the Docker Engine API over a unix-socket http.Client (no SDK dependency to keep image lean).
- `internal/agent/container_sampler.go` — poll `GET /containers/json?all=1`, `GET /containers/{id}/stats?stream=0` every 5s, plus track restart history per container (sliding 60s window) to flag "restart-loop".
- `internal/leader/container_store.go` — in-memory cluster-wide map keyed by `node_id::container_id`.
- `internal/leader/handlers_containers.go` — `GET /api/containers` (all), `GET /api/containers/{node}/{id}` (single).
- `frontend/app/(authed)/containers/page.tsx` — sortable table.
- `frontend/app/(authed)/containers/[node]/[id]/page.tsx` — detail with metric bars and logs tab.

Acceptance:
- Containers page lists every container across the swarm, hot-updating state column.
- A crashing container (exit + restart 4× in 30s) shows `crashed` badge with restart count.
- Detail page shows CPU%, MEM%, NET rx/tx for one container, refreshing ~5s.

Effort: 2d.

---

## Phase 8 — Container logs (Dozzle replacement) — persisted ringbuffer + WebSocket

**Goal:** see logs from any container on any node; user enters a container and sees the last N lines immediately (not just lines from "now"), then live-tails over WebSocket.

Files:
- `proto/manager.proto` — `OpenLogsReq`, `LogLine` event, `CloseLogsReq`.
- `internal/agent/log_streamer.go` — `GET /containers/{id}/logs?follow=1&since=...&tail=...` (Docker raw multiplex stream demuxer). One goroutine per `(container, subscription)` pair; the agent always keeps the last N lines per container in memory and ships them to leader periodically.
- Migration `0002_monitoring.sql` — `container_log_lines` (node_id, container_id, ts, stream, line) with TRIGGER to keep at most `BPM_LOG_MAX_LINES_PER_CONTAINER` rows per container.
- `internal/leader/log_store.go` — persists incoming agent log batches into SQLite; tail query `SELECT … ORDER BY id DESC LIMIT N`.
- `internal/leader/ws_logs.go` — `/api/containers/{node}/{id}/logs/stream` upgrades to WebSocket, returns persisted tail first, then forwards live lines.
- `internal/leader/grpc_server.go` — handle `OpenLogsReq` push toward agent and `LogLine` event coming back.
- `frontend/components/log-viewer.tsx` — buffered list, pause/follow toggle, search, download.

Acceptance:
- Enter a container detail page: see last 500 lines instantly, then new lines appended as they happen.
- Pause → buffer holds, no new render; resume → catches up.
- Across reload, the persisted lines reappear (because they came from SQLite, not the WebSocket).

Effort: 2d.

---

## Phase 9 — Filebrowser (per-node FS via agent gRPC)

Browse, view, edit, rename, delete, chown across managed paths. (Original phase 6.)

Effort: 2.5d.

---

## Phase 10 — Cron management

UI to add/edit cron entries on a node; agent writes `/etc/cron.d/bpm` atomically. (Original phase 7.)

Effort: 1d.

---

## Phase 11 — S3 storage browser

Add Hetzner endpoint, browse buckets, upload/download. (Original phase 8.)

Effort: 1.5d.

---

## Phase 12 — Backup engine

Plans with schedule, agent → tar.gz → S3 multipart, retention, Discord notify. (Original phase 9.)

Effort: 2.5d.

---

## Phase 13 — TOTP 2FA + admin user management + audit log

(Original phase 10.) Effort: 1.5d.

---

## Phase 14 — Production hardening, mTLS, docs

(Original phase 11.) Effort: 1.5d.

---

## Cross-cutting

- Tests: every handler gets at least one happy-path test.
- Lint: `golangci-lint run`, `next lint` in CI.
- Telemetry: `log/slog` everywhere.

**Total remaining: ~18 working days.** After Phase 8 the tool replaces Beszel + Dozzle + Portainer-monitoring entirely.

## Open decisions (carry forward from architecture doc)

1. Single leader (no HA) — recommended.
2. Agent identity = hostname (`BPM_AGENT_HOST_ID` override).
3. Cron via `/etc/cron.d/bpm` only.
4. Bind-mount `/:/host:rslave` for v1.
5. Inline CodeMirror editor for files <1 MiB.
6. S3 upload through the leader for normal use; presigned for >100 MiB later.
7. TOTP only; WebAuthn deferred.
8. Binary admin/user role (no granular RBAC) for v1.
9. SQLite only; portable SQL where cheap.
10. Retention is per-plan-per-node.
11. SSE same-origin only; WebSocket only for log streaming.
12. Discord-only notifier interface; Slack/Teams later.
