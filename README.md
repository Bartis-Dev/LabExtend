# labextend

A Docker Swarm-native infrastructure + monitoring tool. Single Go binary deployed as a global service. One node runs as **leader** (WebUI + orchestration); all others run as **agents**. After phase 8 it replaces Beszel (node metrics) + Dozzle (container logs) + the monitoring slice of Portainer.

## What it does today (post phase 8)

- **Live node dashboard** — per-node CPU/RAM/Disk bars + network throughput, ~5s refresh, with average buttons for 1h / 12h / 24h windows.
- **Container view** — every container across the swarm: name, image, state, health, CPU%, RAM, network rates, restart counter. Auto-detects restart-loops (≥3 restarts/60s → "crashed").
- **Live logs** — per-container streaming with persisted ringbuffer (last 5000 lines per container, configurable). Open a container → instantly see backlog, then live tail.
- **Notifications + webhooks**: failed backups, offline nodes and crash-looping containers show up in the bell in the sidebar and go to every Discord webhook subscribed to them.
- **Auth** — sign-in with Pocket ID (OIDC) only, roles from Pocket ID groups, opaque sessions, CSRF.
- **SSE** — UI updates without polling.

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

Read-only file browser per-node, cron management, S3 browser, **backup engine** (the original headline), mTLS hardening.

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
   │  - event watcher (offline nodes, crash loops) + notify   │
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
| Event (backup failed, node offline/online, crash loop) | leader → SSE + subscribed Discord webhooks | on change | `notifications` (90 days, newest 500) |

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
| `EXTERNAL_URL` | unset | Public base URL of the UI. Needed for OIDC: the redirect URL is `EXTERNAL_URL` + `/api/auth/oidc/callback`. |
| `BPM_OIDC_ISSUER` | unset | OIDC issuer, e.g. `https://id.bartis.dev`. Required: sign-in works only when issuer, client ID and client secret are all set. Also the "manage passkeys" link on the Account page. |
| `BPM_OIDC_CLIENT_ID` | unset | OIDC client ID. |
| `BPM_OIDC_CLIENT_SECRET` | unset | OIDC client secret. `BPM_OIDC_CLIENT_SECRET_FILE` reads it from a file instead (Docker Swarm secret). |
| `BPM_OIDC_NAME` | `Pocket ID` | Provider name shown on the login and account pages. |
| `BPM_OIDC_USER_GROUP` | `labextend` | Pocket ID group whose members may sign in. |
| `BPM_OIDC_ADMIN_GROUP` | `labextend-admins` | Pocket ID group whose members sign in as admin. |
| `BPM_FILEBROWSER` | `false` | `true` enables the file browser. |

## Pocket ID login (OIDC)

Pocket ID is the only way to sign in (any OIDC provider works). There are no local passwords, no TOTP, no setup wizard and no user management in LabExtend: users and roles live in Pocket ID groups. Authorization code flow with PKCE (S256), state and nonce, as a confidential client.

1. In Pocket ID, create two groups (the group **name** is what counts, not the friendly name):
   - `labextend`: may sign in as a regular user.
   - `labextend-admins`: may sign in as admin. Admins do not also need `labextend`.
2. Create an OIDC client:
   - Callback URL: `https://labextend.bartis.dev/api/auth/oidc/callback` (generally `EXTERNAL_URL` + `/api/auth/oidc/callback`)
   - Scopes used: `openid profile email groups`
   - Keep it a confidential client (with a client secret); PKCE stays on.
   - Allowed user groups: `labextend` and `labextend-admins`, so Pocket ID itself refuses everyone else.
3. Set `EXTERNAL_URL`, `BPM_OIDC_ISSUER`, `BPM_OIDC_CLIENT_ID` and `BPM_OIDC_CLIENT_SECRET` (or `BPM_OIDC_CLIENT_SECRET_FILE`) on the leader and redeploy.

On every login:

- Someone in neither group is refused (audit `auth.oidc.failed` with the reason), and any sessions they still hold are revoked.
- An unknown subject in a group gets a user row: username from `preferred_username` (`-2`, `-3`, ... on a collision), email and display name from the token. If the email is missing or already used by another row, `<subject>@oidc.invalid` is stored instead.
- An existing user is matched by issuer + subject only, never by email. Role (admin exactly when in the admin group), email and name are refreshed. Removing someone from the admin group takes their admin rights on their next login. The role is read from the database on every request, so existing sessions follow at once.

If OIDC is not configured, the leader logs an error and keeps running (agents are unaffected), and the login page says which variables are missing. If the issuer is unreachable at boot, discovery is retried on the next sign-in.

### Break-glass

Lost your passkey or locked out? Create a one-time login link in Pocket ID on the server and add a new passkey with it:

```bash
docker exec <pocket-id-container> /app/pocket-id one-time-access-token <user>
```

Then sign in to LabExtend normally. If the account is not in `labextend` or `labextend-admins`, fix the group membership in the Pocket ID admin UI first.

## Audit log and settings

**Settings** (admin only) holds the audit log mode, stored in the database:

- `auto` (default): full log with two or more users. With exactly one user only the newest 100 entries are kept, trimmed after every insert and once at startup.
- `on`: keep everything.
- `off`: record nothing. The Audit page is hidden from the sidebar.

## Notifications

There are no threshold rules. The leader emits a fixed set of events:

| Type | Severity | When |
|---|---|---|
| `backup.failed` | critical (failed), warning (partial) | A run ends failed or partial, including runs that die before reaching a node and runs cut off by a leader restart. Names the failed nodes and the error. |
| `backup.retention_failed` | warning | The backup succeeded but deleting old archives failed. |
| `node.offline` | critical | No heartbeat for 60s. Time the leader itself was down does not count. |
| `node.online` | info | An offline node stayed connected for 30s, so a flapping node is reported once. |
| `container.crashloop` | critical | A container starts crash-looping (3 or more restarts within 60s). Once per transition. |

Every event is stored in `notifications` (newest 500, at most 90 days), pushed
to the panel over SSE and posted to every enabled webhook subscribed to its type
(Webhooks page, one checkbox per type). A post that fails is logged and shown on
the notification as a delivery error. The bell in the sidebar shows the unread
count (red while a critical one is unread) and links each notification to its
backup run, node or container. Read state is shared by all users.

A backup plan's own webhook is only used for successful runs: with "Successful
runs: posted to this webhook" (`webhook_mode=always`) each successful run posts
a summary there. Failures always go through the events above, whatever the
plan says.

## File browser

Off by default. `BPM_FILEBROWSER=true` on the leader turns it on; without it every file route answers 403 and the Files page explains how to enable it. It is enforced on the leader only: agents take commands from the leader alone, and a leader that wanted to read files could just as well run a backup or a cron job, so a second switch on every agent would add configuration without a boundary.

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

# Set BPM_OIDC_ISSUER, BPM_OIDC_CLIENT_ID, BPM_OIDC_CLIENT_SECRET and
# EXTERNAL_URL=http://localhost:8080 first, then open http://localhost:8080
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
  leader/            HTTP+SSE server, gRPC server, stores (metrics/container/log), event watcher
  notify/            Notifications: storage, retention, webhook fan-out
  auth/              Sessions
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
