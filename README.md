# LabExtend

LabExtend is a Docker Swarm-native monitoring and backup tool shipped as one Go
binary (`bpm`) in one image. One replica runs as the **leader** (web UI, REST +
SSE API, gRPC endpoint, SQLite database); a global service runs an **agent** on
every other node. Together they cover live node metrics, containers and their
logs, web analytics from Traefik, scheduled backups to S3-compatible storage,
cron jobs and event notifications, behind an OIDC login (Pocket ID).

## Features

**Monitoring**
- Live node dashboard: CPU, RAM, disk, network and disk I/O per node, pushed
  over SSE every heartbeat (5s by default), with 1h / 12h / 24h averages and
  history charts. Swarm node labels are shown next to each node.
- Containers across the whole swarm, grouped per node or stack: state, health,
  CPU, RAM, network rates and restart count. Three or more restarts within 60s
  mark a container as crash-looping.
- Live logs per container: the backlog opens instantly, then the stream tails.
  The leader keeps the newest 5000 lines per container (pruned in batches of
  1000), drops containers that have been stopped for more than 48 hours, and
  deletes the log lines of containers that no longer exist.
- Web Analytics for every site behind Traefik, computed from Traefik's own
  access log, without a script on the sites and without storing IP addresses.

**Backups**
- Backup plans with a cron schedule, node selection, S3 key template and
  compression, run by the agents and uploaded straight to S3-compatible storage
  (for example Cloudflare R2 or Hetzner Object Storage).
- Two engines: `tar` (tar + gzip of host paths) and `pgdump` (`pg_dump -Fc`,
  optionally verified by restoring the schema into a throwaway Postgres
  container).
- Keep-N retention per plan and node, with `bpm retention-dry-run` to preview
  what would be deleted.
- S3 endpoint management with a write probe and a read-only bucket browser.
  Credentials are stored encrypted (AES-GCM with `BPM_SECRETS_KEY`).

**Operations**
- Cron management: jobs are stored on the leader and written atomically to
  `/etc/cron.d/bpm` on each node.
- Read-only file browser per node (directory listings, no file contents and
  no writes), off unless `BPM_FILEBROWSER=true`.
- "Restart Portainer" button that force-updates the Portainer agent service
  (`BPM_PORTAINER_SERVICE`) through the leader's manager socket.

**Notifications**
- Fixed events: backup failed, backup retention failed, node offline, node
  online, container crash loop.
- A bell in the sidebar with the unread count and links to the run, node or
  container, plus Discord webhooks that subscribe to individual event types.

**Access and audit**
- Sign-in only through an OIDC provider (Pocket ID). Users are provisioned from
  provider groups; the admin role follows the admin group on every login.
- Audit log of state-changing actions with a mode (`auto`, `on`, `off`) on the
  Settings page.
- Optional mTLS for the agent channel (`scripts/gen-mtls.sh`).

Not part of LabExtend (removed on purpose): a browser terminal, file writes of
any kind, local passwords and TOTP, and threshold alert rules.

## Architecture

```
                      Operator browser
                             │ HTTPS (session cookie + CSRF header)
                             ▼ REST + SSE (incl. live logs)
   ┌─────────────────────────────────────────────────────────────────┐
   │  LEADER  (LEADER=true, replicas: 1)                swarm node A │
   │  HTTP :8080 │ gRPC :9090 │ SQLite ${DATA_DIR}/labextend.db      │
   │  - metric, container and log stores                             │
   │  - web analytics (Traefik access log lines)                     │
   │  - backup scheduler + retention, cron state                     │
   │  - event watcher → notifications → SSE + webhooks               │
   │  - OIDC login, sessions, audit log                              │
   │  - in-process agent loop (monitors the leader's own host)       │
   └─────────────────────────────────────────────────────────────────┘
        ▲ gRPC bidi stream (shared token, optional mTLS)
        │ over the labextend-net overlay
   ┌────┴────────────────────────────────────────────────────────────┐
   │  AGENTS  (LEADER_ADDR=labextend-leader:9090, mode: global)      │
   │  - host metrics from /proc                                      │
   │  - Docker collector (container snapshots + stats)               │
   │  - log tailer per container (max 200 lines/s)                   │
   │  - executes leader commands: backups, cron, file listing,       │
   │    swarm service update                                         │
   └─────────────────────────────────────────────────────────────────┘
```

Design choices worth knowing:
- One leader, no HA. If the leader is down, nothing is collected and no backup
  runs; agents reconnect when it is back.
- Agents connect out to the leader and only take commands from it. They expose
  no port.
- Rates (bytes per second) are computed on the leader; the browser only renders.
- Agent identity is the hostname unless `BPM_AGENT_HOST_ID` is set.

### Data flow

| Source | Channel | Frequency | Storage |
|---|---|---|---|
| Host metrics (`/proc/stat`, `/proc/meminfo`, `/proc/net/dev`, `/proc/diskstats`) | gRPC heartbeat | `BPM_HEARTBEAT_INTERVAL` (5s) | `node_metrics` (latest), `node_metric_samples` (raw, 2h), `node_metric_buckets` (1 min averages, 24h by default, 7 days in the stack file) |
| Container snapshots (Docker API) | gRPC container report | every heartbeat | `container_state` |
| Container logs (Docker API, follow) | gRPC log batch | batched | `container_log_lines` (5000 per container) |
| Traefik access log lines | same log stream | flushed every 30s | `web_*` tables |
| Events | leader → SSE + subscribed webhooks | on change | `notifications` (newest 500, at most 90 days) |

## Quick start (Docker Swarm)

### 1. Pocket ID (or any OIDC provider)

LabExtend has no local accounts, so set up the provider first.

1. Create two groups. The group **name** counts, not the friendly name:
   - `labextend`: may sign in as a regular user.
   - `labextend-admins`: may sign in as admin (does not also need `labextend`).
2. Create an OIDC client:
   - Callback URL: `https://<host>/api/auth/oidc/callback`, i.e. `EXTERNAL_URL`
     + `/api/auth/oidc/callback`.
   - Scopes requested by LabExtend: `openid profile email groups`.
   - Confidential client (with a client secret). LabExtend uses PKCE (S256),
     state and nonce on top.
   - Allowed user groups: `labextend` and `labextend-admins`, so the provider
     itself refuses everyone else.

### 2. Secrets and labels

```bash
# Agents need the same token as the leader, so generate it once and export it.
export BPM_AGENT_TOKEN=$(openssl rand -hex 32)
export BPM_SECRETS_KEY=$(openssl rand -hex 32)   # encrypts S3 credentials; keep a copy

export EXTERNAL_URL=https://labextend.example.com
export BPM_OIDC_ISSUER=https://id.example.com
export BPM_OIDC_CLIENT_ID=<client id>

# Client secret as a Swarm secret (then uncomment the secrets blocks and
# BPM_OIDC_CLIENT_SECRET_FILE in docker-stack.yml) ...
printf %s '<client secret>' | docker secret create labextend_oidc_client_secret -
# ... or simply: export BPM_OIDC_CLIENT_SECRET=<client secret>

# Pin the leader to one node.
docker node update --label-add labextend_leader=true <leader-node-id>
```

If `BPM_AGENT_TOKEN` or `BPM_SECRETS_KEY` is missing, the leader generates it
on first boot and writes it to `${DATA_DIR}/.env.generated`. That is fine for a
single node, but agents on other nodes then cannot connect until they get the
same token, and losing `BPM_SECRETS_KEY` makes the stored S3 credentials
unreadable.

### 3. Deploy

```bash
docker stack deploy -c docker-stack.yml labextend
```

Neither port is published. The UI listens on `:8080` and is meant to be
reached by a reverse proxy over an overlay network both services join; gRPC
`:9090` is reached by the agents over `labextend-net`, which is encrypted
(IPsec ESP, so the nodes must allow IP protocol 50 between each other). Put a
reverse proxy in front for TLS, for example Traefik:

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.labextend.rule=Host(`labextend.example.com`)
  - traefik.http.routers.labextend.entrypoints=websecure
  - traefik.http.routers.labextend.tls.certresolver=<resolver>
  - traefik.http.services.labextend.loadbalancer.server.port=8080
```

Set `BPM_TRUSTED_PROXIES` to the proxy's address on that shared network (a
CIDR is fine) so audit entries and the login rate limit see the real client IP
and HSTS follows `X-Forwarded-Proto`.

`GET /healthz` (process up) and `GET /readyz` (database reachable, status code only)
are available for probes. The image has no `HEALTHCHECK` on purpose: the same
image runs as agent, which has no HTTP listener.

### Login behaviour

On every login:
- Someone in neither group is refused (audit `auth.oidc.failed` with the
  reason), and any sessions they still hold are revoked.
- An unknown subject in a group gets a user row: username from
  `preferred_username` (`-2`, `-3`, ... on a collision), email and display name
  from the token. If the email is missing or used by another row,
  `<subject>@oidc.invalid` is stored instead.
- An existing user is matched by issuer + subject only, never by email. Role
  (admin exactly when in the admin group), email and name are refreshed. The
  role is read from the database on every request, so existing sessions follow
  a role change at once.
- Sign-in attempts are rate limited to 10 per 5 minutes per client IP.

If OIDC is not fully configured (issuer, client ID, client secret and
`EXTERNAL_URL`), the leader logs an error and keeps running, and the login page
says what is missing. If the issuer is unreachable at boot, discovery is
retried on the next sign-in.

## Configuration

All configuration is environment variables. `config.example.env` is a
commented template for local development. Role: **L** = leader, **A** = agent
(the leader's in-process agent uses the leader's values), **L+A** = both.

**Role and network**

| Variable | Role | Default | Description |
|---|---|---|---|
| `LEADER` | L | `false` | `true` runs the leader (HTTP + gRPC) plus an in-process agent. |
| `LEADER_ADDR` | A | unset | Leader gRPC address, e.g. `labextend-leader:9090`. Exactly one of `LEADER` and `LEADER_ADDR` must be set. |
| `HTTP_ADDR` | L | `:8080` | HTTP listen address. |
| `GRPC_ADDR` | L | `:9090` | gRPC listen address. |
| `EXTERNAL_URL` | L | unset | Public base URL of the UI. Required for login: the OIDC redirect URL is `EXTERNAL_URL` + `/api/auth/oidc/callback`. |
| `DATA_DIR` | L | `/data` | Holds `labextend.db` and `.env.generated`. |

**Secrets and agent channel**

| Variable | Role | Default | Description |
|---|---|---|---|
| `BPM_AGENT_TOKEN` | L+A | generated on the leader | Shared secret sent in gRPC metadata. Required on agents. |
| `BPM_SECRETS_KEY` | L | generated on the leader | 32 bytes hex. AES-GCM key for stored S3 credentials. Also needed by `retention-dry-run -list-bucket`. |
| `BPM_GRPC_TLS_CERT` | L+A | unset | Leader: server certificate. Agent: client certificate (CN must equal the agent's host ID). |
| `BPM_GRPC_TLS_KEY` | L+A | unset | Key for the certificate above. |
| `BPM_GRPC_TLS_CLIENT_CA` | L+A | unset | Leader: CA that client certificates must chain to. Agent: CA that verifies the leader. |

**Login (OIDC) and sessions**

| Variable | Role | Default | Description |
|---|---|---|---|
| `BPM_OIDC_ISSUER` | L | unset | Issuer URL. Also used for the "manage passkeys" link on the Account page. |
| `BPM_OIDC_CLIENT_ID` | L | unset | Client ID. |
| `BPM_OIDC_CLIENT_SECRET` | L | unset | Client secret. `BPM_OIDC_CLIENT_SECRET_FILE` reads it from a file (Swarm secret) instead. |
| `BPM_OIDC_NAME` | L | `Pocket ID` | Provider name shown on the login and account pages. |
| `BPM_OIDC_USER_GROUP` | L | `labextend` | Group whose members may sign in. |
| `BPM_OIDC_ADMIN_GROUP` | L | `labextend-admins` | Group whose members sign in as admin. |
| `SESSION_COOKIE_NAME` | L | `__Host-bpm_session` (`bpm_session` without secure cookies) | Session cookie name. |
| `SESSION_TTL_HOURS` | L | `12` | Session lifetime. Group changes in the provider apply at the next sign-in. |
| `SESSION_IDLE_HOURS` | L | `2` | Sign out after this long without a request; `0` disables it. |
| `SESSION_SECURE_COOKIE` | L | `true` | Set `false` only for plain-HTTP local development. |
| `BPM_TRUSTED_PROXIES` | L | unset | Comma-separated IPs/CIDRs whose `X-Forwarded-For` is believed. Empty trusts nobody and uses the TCP peer. Never set it to a range that reaches clients. |

**Features**

| Variable | Role | Default | Description |
|---|---|---|---|
| `BPM_FILEBROWSER` | L | `false` | `true` enables the read-only file browser. |
| `BPM_PORTAINER_SERVICE` | L | `portainer_agent` | Swarm service the "Restart Portainer" button force-updates. |
| `BPM_ANALYTICS_IMAGE_PREFIX` | L | `traefik` | Web Analytics reads the logs of containers whose image name (after the last `/`) starts with this. |
| `BPM_ANALYTICS_RETENTION_DAYS` | L | `400` | Days of daily analytics totals and breakdowns to keep. |
| `BPM_ALLOW_EXEC` | L+A | `false` | Read but currently without effect: the generic exec command is always refused. |

**Retention**

| Variable | Role | Default | Description |
|---|---|---|---|
| `BPM_LOG_MAX_LINES` | L | `5000` | Log lines kept per container. |
| `BPM_LOG_RETENTION_HOURS` | L | `0` | Optional age limit for log lines; `0` keeps by count only. |
| `BPM_METRIC_RETENTION_HOURS` | L | `24` | Retention of the 1-minute metric averages (`168` in `docker-stack.yml`). |
| `BPM_SAMPLE_RETENTION_HOURS` | L | `2` | Retention of raw per-heartbeat samples. |

**Agent**

| Variable | Role | Default | Description |
|---|---|---|---|
| `BPM_AGENT_HOST_ID` | A | hostname | Node identity. Must match the certificate CN when mTLS is on. |
| `BPM_AGENT_LABELS` | A | unset | Extra labels, e.g. `role=worker,zone=fsn1`. |
| `BPM_HEARTBEAT_INTERVAL` | A | `5s` | Metrics and container snapshot cadence (Go duration). |
| `BPM_DOCKER_SOCKET` | A | autodetect | Docker socket path; tries `/host/var/run/docker.sock`, then `/var/run/docker.sock`. |
| `BPM_HOST_PREFIX` | A | `/host` if it exists | Where the host's `/` is mounted inside the container (file browser, backups, cron file). |

**Diagnostics**

| Variable | Role | Default | Description |
|---|---|---|---|
| `BPM_LOG_LEVEL` | L+A | `info` | `debug`, `info`, `warn` or `error`. Logs are JSON on stdout. |
| `BPM_S3_DEBUG` | L+A | unset | `1` logs S3 requests and responses with bodies (for opaque 403s). |
| `CORS_ORIGINS` | L | unset | Parsed but currently unused; the UI is served same-origin. |

## Backups and retention

A plan defines the engine, source paths or Postgres DSN, target nodes, a cron
schedule, an S3 endpoint and bucket, a key template and `retention_keep`. The
leader's scheduler fires due plans; each agent streams its archive directly to
the bucket.

- **Key template** placeholders: `{date}`, `{datetime}`, `{host}`, `{node_id}`,
  `{plan}`, `{plan_id}`. Substituted values are made S3-safe: anything but
  letters, digits, `.`, `-` and `_` becomes `-` (R2 rejects a raw `:`, for
  example).
- **Retention** runs after every run that uploaded at least one archive. Per
  plan and node it keeps the newest `retention_keep` successful archives and
  deletes older objects in batches (`DeleteObjects`, 1000 keys per call), then
  their rows. A row is only removed once the provider confirmed the delete,
  the current run is never touched, an object another row still references is
  kept, and `retention_keep: 0` keeps everything. Prune errors are noted in the
  run's error summary and raise `backup.retention_failed` without failing the backup. Failed
  runs, which hold no object, are removed after 30 days.

Preview what retention would delete, against the real database and without
deleting anything:

```bash
docker exec <leader-container> bpm retention-dry-run
docker exec <leader-container> bpm retention-dry-run -list-bucket   # also list the bucket, report orphans
```

`-db <path>` overrides `${DATA_DIR}/labextend.db`. `-list-bucket` needs
`BPM_SECRETS_KEY` (already set in the leader container) and never deletes
unreferenced objects, it only reports them.

## Notifications and webhooks

There are no threshold rules. The leader emits a fixed set of events:

| Type | Severity | When |
|---|---|---|
| `backup.failed` | critical (failed), warning (partial) | A run ends failed or partial, including runs that fail before reaching a node and runs cut off by a leader restart. Names the failed nodes and the error. |
| `backup.retention_failed` | warning | The backup succeeded but deleting old archives failed. |
| `node.offline` | critical | No heartbeat for 60s. Time the leader itself was down does not count. |
| `node.online` | info | An offline node stayed connected for 30s, so a flapping node is reported once. |
| `container.crashloop` | critical | A container starts crash-looping (3 or more restarts within 60s). Once per transition. |

Every event is stored (newest 500, at most 90 days), pushed to the UI over SSE
and posted to every enabled webhook subscribed to its type (Webhooks page, one
checkbox per type). A failed post is logged and shown on the notification. The
bell shows the unread count (red while a critical one is unread); read state is
shared by all users. Webhooks are Discord webhooks; "Send test" reports
Discord's actual answer.

A backup plan's own webhook only receives successful runs, and only with
`webhook_mode=always`. Failures always go through `backup.failed`, whatever the
plan says.

## Web Analytics

Page-view statistics for every website behind Traefik, computed from Traefik's
JSON access log. Nothing is added to the websites.

**Source.** Log lines from containers whose image name (after the last `/`)
starts with `BPM_ANALYTICS_IMAGE_PREFIX` are parsed as access records. Traefik
must log access as JSON to stdout and keep the `User-Agent` and `Referer`
headers:

```yaml
command:
  - --accesslog=true
  - --accesslog.format=json
  - --accesslog.fields.headers.names.User-Agent=keep
  - --accesslog.fields.headers.names.Referer=keep
```

Traefik's own log lines, non-JSON lines, requests no router matched and
requests answered by an `@internal` router are skipped. Unparseable lines are
counted, not logged; the count is returned by `GET /api/analytics/hosts`.

**What it counts, per host** (`RequestHost`, lower-cased, port stripped):

| Number | Meaning |
|---|---|
| Requests | every routed request, including assets and bots |
| Page views | `GET`/`HEAD`, status 200-399, a path without a file extension (or `.html`), not under `/_next`, `/api`, `/static`, `/assets`, `/favicon`, `/robots.txt`, `/sitemap`, `/.well-known`, `/icon`, `/apple-icon`, `/opengraph-image` |
| Bot hits | an empty User-Agent or one naming a crawler, monitor, preview fetcher or HTTP library; never in page views or visitors |
| Visitors | distinct visitors per UTC day; over several days the sum of the daily counts |

Breakdowns per host and day: top pages, referrer domains (external only,
`direct` without a referrer), browsers, operating systems and status classes,
each capped at the top 50 once the day is over.

**Privacy.** No IP address is stored or logged. A visitor is
`sha256(day_salt || ClientHost || 0x00 || User-Agent)` with 32 random bytes of
salt per UTC day. On the first flush of a new day the previous day's salt and
hashes are deleted and only the count stays, so a person cannot be recognised
across days, not even with the database.

**Storage.** Counts are aggregated in memory and written every 30 seconds.
Daily data is kept `BPM_ANALYTICS_RETENTION_DAYS` days, hourly totals 48 hours.

**Limits.** An agent ships at most 200 log lines per second per container, so a
busier Traefik is undercounted. `ClientHost` is the address Traefik sees;
behind a CDN or another proxy, set Traefik's `forwardedHeaders.trustedIPs` or
visitors are undercounted.

## Security notes

- **OIDC only.** No local passwords, TOTP or setup wizard. Users and roles live
  in the provider's groups. Sessions are opaque, stored server-side, in an
  HttpOnly cookie; state-changing API calls require the CSRF header.
- **Break-glass.** Lost your passkey? Create a one-time login link in Pocket ID
  on its server and register a new passkey with it, then sign in normally:
  ```bash
  docker exec <pocket-id-container> /app/pocket-id one-time-access-token <user>
  ```
  If the account is in neither group, fix the membership in the Pocket ID admin
  UI first.
- **Admin boundary.** Files, cron, S3, backups, settings, audit log and the
  Portainer restart are admin-only, because they reach every node's host as
  root.
- **File browser** is off by default and, when on, only lists directories.
  The switch is enforced on the leader only: agents take commands from the leader alone, and a leader
  able to read files could equally run a backup or a cron job, so a switch on
  every agent would add configuration without adding a boundary.
- **Agent channel.** Every agent authenticates with `BPM_AGENT_TOKEN`. gRPC is
  not published outside the overlay network. For stronger isolation generate a
  CA, a leader certificate and one certificate per node:
  ```bash
  bash scripts/gen-mtls.sh ./mtls-out node-a node-b node-c
  ```
  With mTLS the leader binds each agent's claimed ID to its certificate CN, so
  one node cannot impersonate another (and receive its decrypted S3
  credentials). The script header lists the variables to set per role.
- **Trusted proxies.** Without `BPM_TRUSTED_PROXIES` the TCP peer is the client
  IP; behind a proxy that is the proxy. Setting it too wide lets clients forge
  their IP in the audit log and reset their own login rate limit.
- **Privileged mounts.** The image runs as root and both services mount:
  - `/` at `/host` (`rslave`): the file browser and backup sources read host
    paths under `/host`, and cron jobs are written to `/host/etc/cron.d/bpm`.
  - `/var/run/docker.sock` (read-only bind, but full Docker API access):
    container stats and logs, the `pgdump` verify container, and on the leader
    the Swarm service update.
  - `/etc/cron.d`: also mounted, although the agent writes the cron file
    through `/host` whenever `/host` exists.
  Treat the leader UI and the agent token as root on every node.

## Development

Prerequisites: Go (version from `go.mod`, currently 1.25+), Node 22+, and
either `protoc` with `protoc-gen-go` + `protoc-gen-go-grpc` or Docker for proto
regeneration. Host metrics are Linux-only; on other systems the agent reports
none.

```bash
# Regenerate gRPC stubs after editing proto/manager.proto
# (the generated files in internal/grpc/pb are committed)
bash scripts/gen-proto.sh

# Build the SPA into internal/frontend/dist (embedded into the binary)
cd frontend && npm ci --legacy-peer-deps && npm run build:embed && cd ..

# Run a leader with its in-process agent
LEADER=true DATA_DIR=./_dev-data SESSION_SECURE_COOKIE=false \
EXTERNAL_URL=http://localhost:8080 \
BPM_OIDC_ISSUER=... BPM_OIDC_CLIENT_ID=... BPM_OIDC_CLIENT_SECRET=... \
go run ./cmd/manager
# open http://localhost:8080 (register http://localhost:8080/api/auth/oidc/callback at the provider)

# Optional second agent on the same machine
LEADER_ADDR=localhost:9090 BPM_AGENT_HOST_ID=test-node-2 \
BPM_AGENT_TOKEN=<from _dev-data/.env.generated> go run ./cmd/manager

# Tests
go test ./...
cd frontend && npm run type-check
```

`docker build .` regenerates the stubs, builds the SPA and the binary in one
multi-stage build. CI (`.github/workflows/build.yml`, self-hosted runner) runs
`go test ./...`, then builds the image and pushes it to
`ghcr.io/bartis-dev/labextend` tagged by branch, `sha-<commit>` and `latest`.

## Repository layout

```
cmd/manager/          Entry point: leader or agent by env; retention-dry-run subcommand
internal/
  agent/              Host metrics, Docker collector, log tailer, backup engines, cron, gRPC client
  auth/               Server-side sessions
  backup/             Leader-side scheduler, runner and retention
  config/             Env loading, validation, secret auto-generation
  cronctl/            Renders and atomically writes /etc/cron.d/bpm
  db/                 SQLite (modernc.org/sqlite) and goose migrations
  discord/            Discord webhook client
  frontend/           embed.FS for the built SPA
  grpc/pb/            Generated gRPC stubs (committed)
  leader/             HTTP + SSE + gRPC servers, stores, OIDC, analytics, audit, event watcher
  notify/             Notification storage, retention and webhook fan-out
  s3/                 S3 client (R2 and Hetzner quirks handled)
proto/manager.proto   gRPC service and messages (source of truth)
frontend/             Next.js 15 App Router SPA, static export
scripts/              gen-proto.sh, gen-mtls.sh
Dockerfile            protoc → web build → go build → Alpine runtime (pg_dump, docker-cli)
docker-stack.yml      Swarm deployment: replicas: 1 leader + global agents
CHANGELOG.md          User-facing changes, newest first
```

## License

MIT, see [LICENSE](LICENSE).
