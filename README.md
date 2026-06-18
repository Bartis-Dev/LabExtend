# labextend

A Docker Swarm-native infrastructure + monitoring tool. Single Go binary deployed as a global service. One node runs as **leader** (WebUI + orchestration); all others run as **agents**. After phase 8 it replaces Beszel (node metrics) + Dozzle (container logs) + the monitoring slice of Portainer.

## What it does today (post phase 8)

- **Live node dashboard** — per-node CPU/RAM/Disk bars + network throughput, ~5s refresh, with average buttons for 1h / 12h / 24h windows.
- **Container view** — every container across the swarm: name, image, state, health, CPU%, RAM, network rates, restart counter. Auto-detects restart-loops (≥3 restarts/60s → "crashed").
- **Live logs** — per-container streaming with persisted ringbuffer (last 5000 lines per container, configurable). Open a container → instantly see backlog, then live tail.
- **Alerts + webhooks** — threshold rules with hysteresis (CPU > 80% for 60s on any node → Discord). Recovery posts too.
- **Auth** — SQLite users, Argon2id, opaque sessions, CSRF. Setup wizard on first boot.
- **SSE** — UI updates without polling.

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
| `BPM_ALLOW_EXEC` | `false` | Enable generic ExecReq command (not recommended). |
| `BPM_PORTAINER_SERVICE` | `portainer_agent` | Swarm service the sidebar "Restart Portainer" button force-updates (runs on the leader's manager socket). |

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
