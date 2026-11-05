# labextend

A Docker Swarm-native infrastructure + monitoring tool. Single Go binary deployed as a global service. One node runs as **leader** (WebUI + orchestration); all others run as **agents**. After phase 8 it replaces Beszel (node metrics) + Dozzle (container logs) + the monitoring slice of Portainer.

## What it does today (post phase 8)

- **Live node dashboard** — per-node CPU/RAM/Disk bars + network throughput, ~5s refresh, with average buttons for 1h / 12h / 24h windows.
- **Container view** — every container across the swarm: name, image, state, health, CPU%, RAM, network rates, restart counter. Auto-detects restart-loops (≥3 restarts/60s → "crashed").
- **Live logs** — per-container streaming with persisted ringbuffer (last 5000 lines per container, configurable). Open a container → instantly see backlog, then live tail.
- **Alerts + webhooks** — threshold rules with hysteresis (CPU > 80% for 60s on any node → Discord). Recovery posts too.
- **Auth** — SQLite users, Argon2id, opaque sessions, CSRF. Setup wizard on first boot.
- **SSE** — UI updates without polling.
- **Browser terminal** — a root shell on any node's host, admin-only and
  audit-logged, capped at one hour. The agent starts a privileged helper
  container that `nsenter`s into the host namespaces (no sshd, no keys); the
  browser reaches it over a WebSocket on the leader. Anyone who can reach this
  is root on that host — treat admin accounts accordingly and put 2FA on them.

## Web Analytics

Page-view statistics for every website behind Traefik, computed from Traefik's
own JSON access log. Nothing is added to the websites: no script, no pixel, no
third party. The page is **Web Analytics** in the sidebar.

**Source.** The leader already receives every container's log lines. Lines from
containers whose image name (the part after the last `/`) starts with
`BPM_ANALYTICS_IMAGE_PREFIX` (default `traefik`) are parsed as access records.
Traefik must log access as JSON to stdout and keep the `User-Agent` and
`Referer` headers:

```yaml
command:
  - --accesslog=true
  - --accesslog.format=json
  - --accesslog.fields.headers.names.User-Agent=keep
  - --accesslog.fields.headers.names.Referer=keep
```

Traefik's own log lines, lines that are not JSON, and requests no router matched
(scanners with made-up `Host` headers) or that a `@internal` router answered
(entrypoint redirects, dashboard, ping) are skipped. Unparseable lines are
counted, never logged one by one, and the count is returned by
`GET /api/analytics/hosts`.

**What it counts, per host (`RequestHost`, lower-cased, port stripped):**

| Number | Meaning |
|---|---|
| Requests | every routed request, including assets and bots |
| Page views | `GET`/`HEAD`, status 200-399, a path without a file extension (or `.html`), not under `/_next`, `/api`, `/static`, `/assets`, `/favicon`, `/robots.txt`, `/sitemap`, `/.well-known`, `/icon`, `/apple-icon`, `/opengraph-image` |
| Bot hits | an empty User-Agent or one naming a crawler, monitor, preview fetcher or HTTP library; never in page views or visitors |
| Visitors | distinct visitors per UTC day (see privacy); over several days the sum of the daily counts |

Breakdowns per host and day: top pages, referrer domains (external only, the
site itself excluded, `direct` without a referrer), browsers, operating systems
and status classes, each capped at the top 50 once the day is over.

**Privacy.** No IP address is written anywhere: not to the database, not to a
log. A visitor is `sha256(day_salt || ClientHost || 0x00 || User-Agent)`, where
`day_salt` is 32 random bytes per UTC day. On the first flush of a new day the
previous day's salt and every hash of that day are deleted, and only the number
of distinct hashes stays. The same person therefore cannot be recognised from
one day to the next, not even by someone holding the database. Within a day the
salt is stored so a leader restart does not count everyone twice.

**Storage and retention.** Counts are aggregated in memory and written to SQLite
every 30 seconds, so the page lags by up to that much. Daily totals and
breakdowns are kept `BPM_ANALYTICS_RETENTION_DAYS` (default 400) days, hourly
totals for the 24h chart 48 hours.

**Limits to know.** The agent ships at most 200 log lines per second per
container and drops the rest, so a Traefik serving more than that is
undercounted. `ClientHost` is the address Traefik sees: behind a CDN or another
proxy it is that proxy unless Traefik's `forwardedHeaders.trustedIPs` is set,
and visitors are then undercounted.

## What's planned (phases 9–14)

Filebrowser per-node, cron management, S3 browser, **backup engine** (the original headline), TOTP 2FA, admin user management, mTLS hardening.

## Architecture

```
                      Operator Browser
                             │ HTTPS (cookies + CSRF)
                             ▼ REST + SSE (incl. live logs)
   ┌──────────────────────────────────────────────────────────┐
   │  LEADER  (LEADER=true)               swarm node A         │
   │  Go binary: HTTP :8080 │ gRPC :9090 │ SQLite /data/labextend.db │
   │  - metrics store (5s heartbeat + 1min downsampled to DB)  │
   │  - container store (cluster-wide map, ContainerReport)    │
   │  - log store (ringbuffer per container in SQLite)         │
   │  - alert engine (rule evaluator w/ hysteresis, Discord)   │
   │  - in-process agent loop (monitors leader's own host)     │
   └──────────────────────────────────────────────────────────┘
        ▲ gRPC bidi (shared-secret)         ▲
        │                                   │
   ┌────┴──────┐                     ┌──────┴──────┐
   │ AGENT B   │  ...                │ AGENT N     │
   │ - sysmetrics (CPU/RAM/Net/IO)   │             │
   │ - Docker collector (every 5s)   │             │
   │ - Log tailer per container      │             │
   │ - dispatches fs/cron/backup     │             │
   └───────────────────────────────────────────────┘
```

## Monitoring data flow

| Source | Channel | Frequency | Storage |
|---|---|---|---|
| Host metrics (`/proc/stat`, `/proc/net/dev`, `/proc/diskstats`) | gRPC `Heartbeat` | 5s | `node_metrics` (latest) + `node_metric_buckets` (1min avg, 7-day retention) |
| Container snapshots (Docker API `/containers/json` + `/stats?stream=0`) | gRPC `Event_ContainerReport` | 5s | `container_state` |
| Container logs (`/containers/{id}/logs?follow=1`) | gRPC `Event_LogBatch` | 1.5s flush | `container_log_lines` (5000/container cap) |
| Alert rule fires | leader → Discord webhook | on change | `alert_history` |

UI receives delta-computed rates (bps) — the leader does the math, the browser does not.

## Configuration knobs

| Env | Default | What |
|---|---|---|
| `LEADER` | `false` | `true` → leader mode (HTTP + gRPC). Also spawns local agent. |
| `LEADER_ADDR` | — | Agent-only. e.g. `labextend-leader:9090`. |
| `BPM_AGENT_TOKEN` | auto-gen on leader | Shared secret in gRPC metadata. |
| `BPM_HEARTBEAT_INTERVAL` | `5s` | Host metrics + container snapshot cadence. |
| `BPM_LOG_MAX_LINES` | `5000` | Per-container line cap in SQLite. |
| `BPM_LOG_RETENTION_HOURS` | `0` (cap only) | Optional time-based prune. |
| `BPM_METRIC_RETENTION_HOURS` | `24` (Docker stack: `168` = 7d) | 1-min bucket retention. |
| `BPM_DOCKER_SOCKET` | autodetect | Host Docker socket path inside container. |
| `BPM_TRUSTED_PROXIES` | — | Comma-separated IPs/CIDRs whose `X-Forwarded-For` is believed. Empty trusts nobody and uses the TCP peer address. Set it to your reverse proxy, never to a range reaching clients. |
| `BPM_ALLOW_EXEC` | `false` | Enable generic ExecReq command (not recommended). |
| `BPM_PORTAINER_SERVICE` | `portainer_agent` | Swarm service the sidebar "Restart Portainer" button force-updates (runs on the leader's manager socket). |
| `BPM_ANALYTICS_IMAGE_PREFIX` | `traefik` | Web Analytics reads the access log of containers whose image name starts with this. |
| `BPM_ANALYTICS_RETENTION_DAYS` | `400` | Days of daily Web Analytics totals and breakdowns to keep. |
| `BPM_SHELL_IMAGE` | the agent's own image | Image the browser terminal's privileged `nsenter` helper container runs. The default is already on the node and ships `nsenter`; a replacement must do both. |

## Dev loop

Prereqs: Go 1.23+, Node 22+, **protoc** + protoc-gen-go + protoc-gen-go-grpc (or just Docker for the fallback path).

```bash
# 1. Regenerate proto stubs (REQUIRED after editing proto/manager.proto OR on a
#    fresh checkout if the committed pb.go is older than the proto)
bash scripts/gen-proto.sh

# 2. Frontend deps + first build
cd frontend && npm install --legacy-peer-deps && npm run build:embed && cd ..

# 3. Run the leader
LEADER=true \
DATA_DIR=./_dev-data \
SESSION_SECURE_COOKIE=false \
go run ./cmd/manager

# Open http://localhost:8080 → setup wizard
```

The leader's local agent auto-attaches; no separate agent process needed unless you want to test multi-node from a single machine.

### Multi-host local sim

```bash
# In a second shell:
LEADER_ADDR=localhost:9090 \
BPM_AGENT_TOKEN=<copy from _dev-data/.env.generated> \
BPM_AGENT_HOST_ID=test-node-2 \
go run ./cmd/manager
```

## Production deploy (Docker Swarm)

```bash
# Generate secrets once
export BPM_AGENT_TOKEN=$(openssl rand -hex 32)
export BPM_SECRETS_KEY=$(openssl rand -hex 32)
export BPM_TOTP_KEY=$(openssl rand -hex 32)

# Label the leader node
docker node update --label-add labextend_leader=true <leader-node-id>

# Deploy
docker stack deploy -c docker-stack.yml labextend
```

UI on `:8080` of the leader node. Put Traefik in front for TLS:

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.labextend.rule=Host(`labextend.bartis.dev`)
  - traefik.http.routers.labextend.entrypoints=websecure
  - traefik.http.routers.labextend.tls.certresolver=cloudflare
  - traefik.http.services.labextend.loadbalancer.server.port=8080
```

## Repository layout

```
cmd/manager/         Single binary entrypoint (LEADER vs AGENT chosen by env)
internal/
  config/            Env loading, defaults, validation, secret auto-gen
  db/                SQLite (modernc.org/sqlite), goose migrations
  agent/             Docker API client, container sampler, log tailer, host metrics
  leader/            HTTP+SSE server, gRPC server, stores (metrics/container/log), alert engine
  auth/              Sessions, Argon2id, TOTP
  backup/            (phase 12)
  s3/                (phase 11)
  cronctl/           (phase 10)
  discord/           Webhook client (real, no longer stub)
  frontend/          embed.FS for the built Next.js SPA
proto/manager.proto  gRPC service + messages (source of truth)
frontend/            Next.js 15 App Router, output: 'export'
scripts/gen-proto.sh Local or Docker-fallback proto regeneration
Dockerfile           Multi-stage: protogen → web build → go build → alpine runtime
docker-stack.yml     Production Swarm deployment (global agents + replicas=1 leader)
.github/workflows/   CI: proto regen → go test → frontend build → docker buildx + push
```

## License

TBD. Internal-use for now; switch to MIT or AGPL before public release.
