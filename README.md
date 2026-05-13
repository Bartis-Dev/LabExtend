# LabExtend

A self-hosted homelab dashboard. Single Go binary, embedded React frontend, SQLite storage. Drag-and-drop service cards, resizable categories, multi-theme system, live healthchecks streamed over WebSocket.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Features

- **Single binary.** Frontend is `go:embed`-ed; the binary plus a `/data` volume is the whole runtime.
- **Modular dashboard.** Toggleable built-in modules (Dashboard, DDNS, WoL, Command Lab, Secrets, Docs, Notes, Stats) plus user-defined iframe embeds in the navbar.
- **Drag-and-drop dashboard grid.** Configurable columns. Services group into resizable categories with their own nested grid.
- **Live healthchecks.** TCP-connect ping and HTTP(S) probe per host (primary + alternate). Status streamed over WebSocket with REST polling fallback.
- **DDNS sync.** Cloudflare provider with token verification; auto-update A/AAAA records when the public IP changes (2-service fallback for IP lookup). API tokens are AES-GCM encrypted at rest.
- **Wake-on-LAN.** UDP magic-packet sender with normalised MAC input.
- **Command Lab.** Visual command builder for Linux / PowerShell / cmd / Docker with autosize inputs, chmod permissions checkbox grid, and a catalogue of important file paths.
- **Zero-knowledge Secrets vault.** Argon2id key derivation + AES-GCM-256 encryption happen in the browser; the server only sees ciphertext. TOTP code generation included.
- **Markdown Docs.** Notes and external-link bookmarks grouped by category, rendered with `react-markdown` + `remark-gfm`.
- **Free-canvas Notes.** Trello-style cards on a pan/zoom canvas with collision-aware placement; a portal-rendered floating favourites card stays on top across every tab when toggled.
- **Push-based Stats.** Per-source ingest tokens, time-series storage, line/gauge widgets with auto-refresh.
- **Multi-theme system.** Twelve CSS custom properties exposed through a color-picker UI plus a Monaco-backed Custom CSS tab.
- **Optional HTTPS.** Self-sign in one click, upload a PEM pair, or point at env-mounted cert files. Hot-reload on upload.
- **First-run setup wizard.** No admin user is created in advance.
- **Env-driven password reset.** Set `LABEXTEND_PASSWORD_RESET=true`, restart, and the setup wizard returns.
- **Single-user, opinionated.** No multi-tenancy ceremony.

---

## Quick start — Docker

LabExtend listens on **`10000`** (HTTP) and, when a TLS certificate is configured,
**`10001`** (HTTPS). Wake-on-LAN (and other LAN-broadcast features) require
`network_mode: host` so the magic packets actually reach your network.

```bash
docker run -d \
  --name labextend \
  --network host \
  -v labextend-data:/data \
  --restart unless-stopped \
  ghcr.io/bartis-dev/labextend:latest
```

Open <http://localhost:10000> and complete the setup wizard (pick username + password ≥ 8 chars). You're in.

### docker-compose

```yaml
services:
  labextend:
    image: ghcr.io/bartis-dev/labextend:latest
    container_name: labextend
    # host networking is required for Wake-on-LAN broadcasts; if you don't
    # use WoL you can switch back to a port mapping (see below).
    network_mode: host
    volumes:
      - labextend-data:/data
    restart: unless-stopped
    environment:
      LABEXTEND_SESSION_TIMEOUT: 7d

volumes:
  labextend-data:
```

If you don't need Wake-on-LAN, replace `network_mode: host` with explicit port
mappings instead:

```yaml
    ports:
      - "10000:10000"  # HTTP
      - "10001:10001"  # HTTPS (only used when a TLS cert is configured)
```

---

## HTTPS

The Secrets vault and a few other browser features (Web Crypto API) only work
in a **secure context** — i.e. `https://` or `http://localhost`. There are
three ways to enable HTTPS:

1. **Upload a cert via the web UI** — open Settings → TLS, paste the PEM-encoded
   certificate chain and private key, hit Install. Stored at
   `data/tls/cert.pem` + `data/tls/key.pem` with mode `0600`. New handshakes
   pick up the cert on the next connection; existing TCP sessions are not
   interrupted. The HTTPS listener auto-starts on port `10001` after a
   restart (or immediately on first boot if a cert was already on disk).
2. **Generate a self-signed cert in-app** — Settings → TLS → "Generate self-
   signed". List the hostnames / IPs you'll use to reach LabExtend. Browsers
   warn about an unknown CA, but encryption works, and Web Crypto is happy.
3. **Point at existing files via env vars** — set
   `LABEXTEND_TLS_CERT_FILE` and `LABEXTEND_TLS_KEY_FILE` to absolute paths
   (often a mounted secret). LabExtend reads them at startup and won't touch
   them on disk afterwards.

`LABEXTEND_TLS_SELF_SIGN=true` makes LabExtend generate a self-signed cert
automatically on first boot if none of the above produced one — handy for
disposable test deployments. Persistent setups should pick path 1 or 3.

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `LABEXTEND_LISTEN` | `0.0.0.0:10000` | HTTP listen address. |
| `LABEXTEND_TLS_LISTEN` | `0.0.0.0:10001` | HTTPS listen address. The listener only starts when a certificate is available. |
| `LABEXTEND_TLS_CERT_FILE` | *(empty)* | Path to a PEM certificate. Overrides any cert at `data/tls/cert.pem`. |
| `LABEXTEND_TLS_KEY_FILE` | *(empty)* | Path to the matching PEM private key. |
| `LABEXTEND_TLS_SELF_SIGN` | `false` | Generate a self-signed cert on first boot if none is configured. |
| `LABEXTEND_DATA_DIR` | `/data` | Directory for the SQLite database, uploaded icons, and TLS files. Mount this as a volume. |
| `LABEXTEND_SESSION_TIMEOUT` | `7d` | JWT cookie lifetime. Accepts Go duration syntax extended with `Nd` for days: `30m`, `3h`, `7d`, `720h`. |
| `LABEXTEND_HEALTHCHECK_INTERVAL` | `60s` | Initial probe cadence on first boot. Editable in **Settings → Healthcheck** afterwards (10s – 1h). |
| `LABEXTEND_PASSWORD_RESET` | `false` | If `true` at startup, **all users are deleted** and the setup wizard returns. Use to recover a lost password — see below. |
| `LABEXTEND_JWT_SECRET` | *(empty)* | When set, used as HMAC secret. When empty, a fresh 32-byte secret is generated and stored in the database on first boot. |
| `LABEXTEND_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |

The data directory contains:

```
/data
├── labextend.db          # SQLite (users, services, categories, themes, settings, modules,
│                         #         vault, ddns, wol, docs, notes, stats)
├── icons/
│   └── <uuid>.png        # Uploaded icons (UUID-named)
└── tls/                  # Created when a cert is installed via UI / self-sign
    ├── cert.pem          # PEM-encoded certificate chain (0600)
    └── key.pem           # PEM-encoded private key (0600)
```

---

## Forgot password

There is no in-app password recovery flow. To reset:

1. Set `LABEXTEND_PASSWORD_RESET=true`.
2. Restart the container.
3. On next start, the existing user is removed and the setup wizard reappears.
4. After completing setup, remove the variable (or set it to `false`) and restart once more so subsequent restarts don't wipe the account.

All services, categories, themes, layout, and icons are preserved across the reset — only users are deleted.

---

## Backup & restore

The entire app state lives under `LABEXTEND_DATA_DIR` (default `/data`):

```bash
# Backup
docker run --rm -v labextend-data:/data -v $(pwd):/backup alpine \
  tar -czf /backup/labextend-$(date +%F).tar.gz -C / data

# Restore
docker stop labextend
docker run --rm -v labextend-data:/data -v $(pwd):/backup alpine \
  tar -xzf /backup/labextend-2026-05-11.tar.gz -C /
docker start labextend
```

For a hot-friendly backup, briefly stop the container or use `sqlite3 labextend.db ".backup /backup/snapshot.db"` against the running file (WAL mode is on).

---

## Reverse proxy

LabExtend speaks plain HTTP and supports WebSockets at `/api/ws`. Most reverse proxies need explicit upgrade headers — sample nginx snippet:

```nginx
location / {
    proxy_pass http://labextend:10000;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # WebSocket for /api/ws
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 86400;
}
```

When `X-Forwarded-Proto: https` is present, LabExtend sets the `Secure` flag on the session cookie automatically.

---

## Build from source

Requirements: Go ≥ 1.23, Node ≥ 20.

```bash
git clone https://github.com/Bartis-Dev/LabExtend.git
cd LabExtend

# Frontend build then embed into Go binary
make build
./bin/labextend
```

### Development

Run frontend and backend in two terminals:

```bash
# Terminal 1 — Vite dev server (HMR, proxies /api to :10000)
cd web && npm run dev

# Terminal 2 — Go server
go run ./cmd/labextend
```

Visit <http://localhost:5173>.

### Docker image (local)

```bash
make docker
docker run --rm --network host -v $(pwd)/data:/data labextend:dev
```

---

## API overview

All routes return JSON unless noted. Mutations require a valid `labextend_session` cookie (`SameSite=Strict`, `HttpOnly`).

```
GET    /api/bootstrap                 needs_setup + active_theme
POST   /api/setup                     create initial user, set cookie
POST   /api/auth/login
POST   /api/auth/logout
GET    /api/auth/me
PUT    /api/auth/password

GET    /api/services
POST   /api/services
GET    /api/services/{id}
PUT    /api/services/{id}
DELETE /api/services/{id}
POST   /api/services/{id}/icon        multipart upload
DELETE /api/services/{id}/icon

GET    /api/categories
POST   /api/categories
PUT    /api/categories/{id}
DELETE /api/categories/{id}

PUT    /api/layout                    bulk position update

GET    /api/themes
POST   /api/themes
PUT    /api/themes/{id}
DELETE /api/themes/{id}
POST   /api/themes/{id}/activate

GET    /api/settings
PUT    /api/settings

GET    /api/healthcheck/status        last status snapshot
GET    /api/ws                        WebSocket: pushes hc_update messages

GET    /api/icons/{filename}          public, traversal-safe

# Module CRUD (built-ins toggleable, iframes user-creatable)
GET    /api/modules
POST   /api/modules
PUT    /api/modules/{id}
DELETE /api/modules/{id}

# Zero-knowledge encrypted secrets vault (Argon2id + AES-GCM in browser)
GET    /api/vault/state
POST   /api/vault/setup
GET    /api/vault/entries
POST   /api/vault/entries
PUT    /api/vault/entries/{id}
DELETE /api/vault/entries/{id}

# DDNS providers, zones, records, auto-update worker
GET    /api/ddns/providers
POST   /api/ddns/providers
PUT    /api/ddns/providers/{id}
DELETE /api/ddns/providers/{id}
GET    /api/ddns/providers/{id}/zones        live proxy to Cloudflare
GET    /api/ddns/cards
POST   /api/ddns/cards
PUT    /api/ddns/cards/{id}
DELETE /api/ddns/cards/{id}
GET    /api/ddns/cards/{id}/records          live proxy
POST   /api/ddns/cards/{id}/records
PUT    /api/ddns/cards/{id}/records/{rid}
DELETE /api/ddns/cards/{id}/records/{rid}
POST   /api/ddns/cards/{id}/auto-update
GET    /api/ddns/auto-update                 sync status

# Wake-on-LAN
GET    /api/wol
POST   /api/wol
PUT    /api/wol/{id}
DELETE /api/wol/{id}
POST   /api/wol/{id}/wake

# Docs (markdown pages + link bookmarks)
GET    /api/docs
POST   /api/docs
GET    /api/docs/{id}
PUT    /api/docs/{id}
DELETE /api/docs/{id}

# Notes (Trello-style canvas)
GET    /api/notes
POST   /api/notes/cards
PUT    /api/notes/cards/{id}
PATCH  /api/notes/cards/{id}/layout          lightweight drag updates
DELETE /api/notes/cards/{id}
POST   /api/notes/cards/{id}/items
PUT    /api/notes/items/{id}
DELETE /api/notes/items/{id}

# Stats (time-series + ingest)
GET    /api/stats/sources
POST   /api/stats/sources
PUT    /api/stats/sources/{id}
POST   /api/stats/sources/{id}/rotate-token
DELETE /api/stats/sources/{id}
GET    /api/stats/sources/{id}/points
GET    /api/stats/widgets
POST   /api/stats/widgets
PUT    /api/stats/widgets/{id}
DELETE /api/stats/widgets/{id}
POST   /api/stats/ingest/{token}              public, token-authed

# TLS
GET    /api/tls/state
POST   /api/tls/cert                          install uploaded PEM
POST   /api/tls/self-signed                   generate + install
DELETE /api/tls/cert
```

---

## Security notes

- Passwords are hashed with **argon2id** (64 MiB / 3 passes / 2 lanes / 32-byte tag).
- Sessions are stateless JWTs (HS256) in an HTTP-only, `SameSite=Strict` cookie; `Secure` flag set when TLS detected.
- Login is rate limited (5 attempts / 5 minutes / IP). Setup is rate limited globally (5 / minute) until the first user exists.
- SVG uploads are sanitised through a strict allowlist; `<script>`, `on*`, and unsafe `xlink:href` targets are stripped.
- Path-traversal guards on the icons endpoint (filename cannot contain slashes or `..`, final path is verified inside the icons directory).
- TLS verification is disabled for outbound HTTP probes because homelab services typically use self-signed certificates. Probe responses are never relayed to the browser; LabExtend only stores the status.

---

## License

[MIT](LICENSE) — full permission for personal, commercial, modification, and redistribution use.

---

## Contributing

Issues and pull requests welcome at <https://github.com/Bartis-Dev/LabExtend>.
