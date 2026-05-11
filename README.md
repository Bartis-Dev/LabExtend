# LabExtend

A self-hosted homelab dashboard. Single Go binary, embedded React frontend, SQLite storage. Drag-and-drop service cards, resizable categories, multi-theme system, live healthchecks streamed over WebSocket.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Features

- **Single binary.** Frontend is `go:embed`-ed; the binary plus a `/data` volume is the whole runtime.
- **Drag-and-drop grid.** Configurable columns (4–12). Service cards and category containers can be moved, resized, and freely positioned.
- **Categories.** Group services into resizable containers with their own nested grid and border color.
- **Live healthchecks.** TCP-connect ping and HTTP(S) probe per host (primary + alternate). Status streamed over WebSocket with REST polling fallback.
- **Multi-theme system.** Twelve CSS custom properties exposed through a color-picker UI (Quick Edit) plus a Monaco-backed Custom CSS tab. Save named themes and switch between them instantly.
- **Icons.** Upload PNG / JPG / WebP / SVG (sanitised) per service; first-letter fallback otherwise.
- **First-run setup wizard.** No admin user is created in advance.
- **Env-driven password reset.** Set `LABEXTEND_PASSWORD_RESET=true`, restart, and the setup wizard returns.
- **Single-user, opinionated.** No multi-tenancy ceremony.

---

## Quick start — Docker

```bash
docker run -d \
  --name labextend \
  -p 8080:8080 \
  -v labextend-data:/data \
  --restart unless-stopped \
  ghcr.io/bartis-dev/labextend:latest
```

Open <http://localhost:8080> and complete the setup wizard (pick username + password ≥ 8 chars). You're in.

### docker-compose

```yaml
services:
  labextend:
    image: ghcr.io/bartis-dev/labextend:latest
    container_name: labextend
    ports:
      - "8080:8080"
    volumes:
      - labextend-data:/data
    restart: unless-stopped
    environment:
      LABEXTEND_SESSION_TIMEOUT: 7d

volumes:
  labextend-data:
```

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `LABEXTEND_LISTEN` | `0.0.0.0:8080` | HTTP listen address. |
| `LABEXTEND_DATA_DIR` | `/data` | Directory for the SQLite database and uploaded icons. Mount this as a volume. |
| `LABEXTEND_SESSION_TIMEOUT` | `7d` | JWT cookie lifetime. Accepts Go duration syntax extended with `Nd` for days: `30m`, `3h`, `7d`, `720h`. |
| `LABEXTEND_HEALTHCHECK_INTERVAL` | `60s` | Initial probe cadence on first boot. Editable in **Settings → Healthcheck** afterwards (10s – 1h). |
| `LABEXTEND_PASSWORD_RESET` | `false` | If `true` at startup, **all users are deleted** and the setup wizard returns. Use to recover a lost password — see below. |
| `LABEXTEND_JWT_SECRET` | *(empty)* | When set, used as HMAC secret. When empty, a fresh 32-byte secret is generated and stored in the database on first boot. |
| `LABEXTEND_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |

The data directory contains:

```
/data
├── labextend.db          # SQLite (users, services, categories, themes, settings)
└── icons/
    └── <uuid>.png        # Uploaded icons (UUID-named)
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
    proxy_pass http://labextend:8080;
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
# Terminal 1 — Vite dev server (HMR, proxies /api to :8080)
cd web && npm run dev

# Terminal 2 — Go server
go run ./cmd/labextend
```

Visit <http://localhost:5173>.

### Docker image (local)

```bash
make docker
docker run --rm -p 8080:8080 -v $(pwd)/data:/data labextend:dev
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
