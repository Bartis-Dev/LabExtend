# LabExtend — Design Document

**Status:** Approved
**Date:** 2026-05-11
**Repo:** https://github.com/Bartis-Dev/LabExtend
**License:** MIT

LabExtend is a self-hosted, single-binary homelab dashboard. It renders a flexible grid of service cards (drag-and-drop, resizable groupings) backed by SQLite, ships a Go server with an embedded React frontend, and exposes a live health-check feed over WebSocket.

---

## 1. Goals & Non-Goals

### Goals
- Single binary deploy (`labextend`) with embedded frontend.
- Mandatory authentication, first-run setup wizard, env-driven password reset.
- Drag-and-drop dashboard with service cards and category containers.
- Multi-theme system: quick palette editor + raw CSS editor, named theme persistence.
- HTTP and TCP health checks streamed over WebSocket.
- Open-source (MIT), Docker-first, CI on self-hosted runners.

### Non-Goals (YAGNI)
- Multi-user / role-based access (single user only).
- WebSocket/SSE based real-time chat or activity feed.
- Theme import/export as files (deferred).
- ICMP ping (requires raw sockets, friction in containers).
- Tags, search, advanced filtering on the dashboard.
- Full E2E test suite.

---

## 2. Tech Stack

| Layer | Choice | Rationale |
|---|---|---|
| Backend | Go 1.23+, `net/http` + `go-chi/chi/v5` | Clean middleware composition, std-compatible. |
| Database | `modernc.org/sqlite` | Pure-Go, no CGO → easy cross-compile, simple Docker. |
| Auth | `golang-jwt/jwt/v5` + `golang.org/x/crypto/argon2` | argon2id hashing, JWT HS256 in HTTP-only cookie. |
| WebSocket | `github.com/coder/websocket` | Modern, small, context-aware, no CGO. |
| Frontend | Vite + React 18 + TypeScript + Tailwind 3 | Minimal build, fast HMR, perfect for embed SPA. |
| Grid | `react-grid-layout` | Drag, drop, resize out of the box. |
| Color UI | `@uiw/react-color` | Composable color picker. |
| CSS Editor | `@monaco-editor/react` | VSCode-grade CSS editing for advanced theme tab. |
| State | Zustand | Minimal client store; no Redux ceremony. |
| HTTP Client | TanStack Query | Cache + invalidation for REST + WS hybrid. |
| Embed | `go:embed all:web/dist` | Single binary deploy. |

---

## 3. Project Structure

```
LabExtend/
├── cmd/labextend/
│   └── main.go
├── internal/
│   ├── api/              # HTTP handlers (auth, services, categories, themes, settings, healthcheck, icons, ws)
│   │   ├── router.go
│   │   ├── auth.go
│   │   ├── bootstrap.go
│   │   ├── services.go
│   │   ├── categories.go
│   │   ├── layout.go
│   │   ├── themes.go
│   │   ├── settings.go
│   │   ├── icons.go
│   │   └── ws.go
│   ├── auth/             # JWT, argon2id, cookie helpers, ratelimit
│   ├── config/           # ENV parsing
│   ├── db/               # SQLite open, migrations, query helpers
│   │   ├── db.go
│   │   ├── migrations.go
│   │   └── migrations/
│   │       └── 0001_init.sql
│   ├── healthcheck/      # Worker, TCP probe, HTTP probe, broadcaster
│   ├── theme/            # Defaults + theme service
│   └── web/              # go:embed for /web/dist
│       └── embed.go
├── web/                  # Vite project
│   ├── src/
│   │   ├── api/          # Typed REST/WS clients
│   │   ├── components/
│   │   ├── pages/
│   │   │   ├── Dashboard.tsx
│   │   │   ├── Auth.tsx
│   │   │   └── Settings.tsx
│   │   ├── store/        # Zustand stores
│   │   ├── styles/
│   │   │   └── globals.css
│   │   ├── App.tsx
│   │   └── main.tsx
│   ├── index.html
│   ├── package.json
│   ├── tailwind.config.ts
│   ├── postcss.config.cjs
│   ├── tsconfig.json
│   └── vite.config.ts
├── .github/
│   └── workflows/
│       └── build.yml     # self-hosted runner
├── docs/
│   └── superpowers/specs/
│       └── 2026-05-11-labextend-design.md
├── data/                 # runtime; gitignored (db + icons/)
├── .gitignore
├── .dockerignore
├── Dockerfile
├── LICENSE
├── Makefile
├── README.md
└── go.mod
```

Runtime artifacts live under `LABEXTEND_DATA_DIR` (default `/data`):
- `labextend.db` — SQLite database
- `icons/` — uploaded icons (UUID-named)

---

## 4. Data Model (SQLite)

```sql
-- Users (single user expected, but no hard limit)
CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT    NOT NULL UNIQUE,
  password_hash TEXT    NOT NULL,
  created_at    INTEGER NOT NULL
);

-- Service cards
CREATE TABLE services (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  name                  TEXT    NOT NULL,
  description           TEXT    NOT NULL DEFAULT '',
  host_primary          TEXT    NOT NULL,
  port_primary          INTEGER,
  host_alt              TEXT,
  port_alt              INTEGER,
  icon_path             TEXT,
  category_id           INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  layout_x              INTEGER NOT NULL DEFAULT 0,
  layout_y              INTEGER NOT NULL DEFAULT 0,
  layout_w              INTEGER NOT NULL DEFAULT 1,
  layout_h              INTEGER NOT NULL DEFAULT 1,
  ping_primary          INTEGER NOT NULL DEFAULT 0, -- bool
  ping_alt              INTEGER NOT NULL DEFAULT 0,
  hc_primary_enabled    INTEGER NOT NULL DEFAULT 0,
  hc_primary_url        TEXT,                       -- override, else derived from host_primary
  hc_alt_enabled        INTEGER NOT NULL DEFAULT 0,
  hc_alt_url            TEXT,
  created_at            INTEGER NOT NULL,
  updated_at            INTEGER NOT NULL
);

-- Category containers
CREATE TABLE categories (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT    NOT NULL,
  border_color TEXT    NOT NULL DEFAULT '#3b82f6',
  layout_x     INTEGER NOT NULL DEFAULT 0,
  layout_y     INTEGER NOT NULL DEFAULT 0,
  layout_w     INTEGER NOT NULL DEFAULT 3,
  layout_h     INTEGER NOT NULL DEFAULT 2,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

-- Themes (multi-theme support)
CREATE TABLE themes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT    NOT NULL UNIQUE,
  palette_json TEXT    NOT NULL,  -- JSON: { "--bg": "#0a0a0a", ... }
  custom_css   TEXT    NOT NULL DEFAULT '',
  is_default   INTEGER NOT NULL DEFAULT 0,
  is_active    INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

-- Key-value settings (grid_cols, hc_interval, jwt_secret, ...)
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

Indexes: `services(category_id)`, `themes(is_active)`.

Layout coordinates are in grid units. Grid column count is set globally in settings (default 6, range 4–12). Service cards default to 1×1; categories to 3×2 or 2×2 — both resizable in 1-unit increments. Cards inside a category render in a nested grid with their own `layout_x/y/w/h` relative to the category. If a category is shrunk below the bounding box of its children, the affected children are detached (`category_id = NULL`) rather than crashing.

---

## 5. Environment Variables

| Variable | Default | Description |
|---|---|---|
| `LABEXTEND_LISTEN` | `0.0.0.0:8080` | HTTP listen address. |
| `LABEXTEND_DATA_DIR` | `/data` | Where DB and `icons/` live. |
| `LABEXTEND_PASSWORD_RESET` | `false` | If `true` at boot, deletes all users → forces setup wizard. Set back to `false` and restart. |
| `LABEXTEND_SESSION_TIMEOUT` | `7d` | JWT lifetime. Accepts Go duration extended with `d` (e.g. `30m`, `3h`, `7d`, `720h`). |
| `LABEXTEND_HEALTHCHECK_INTERVAL` | `60s` | Initial value; persisted to settings on first boot, editable in UI. |
| `LABEXTEND_JWT_SECRET` | *(empty)* | If set, used as HMAC secret. If empty, auto-generated and stored in `settings`. |
| `LABEXTEND_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |

`d` suffix support is custom: parse `^(\d+)d$` → `n * 24h`, else fall back to `time.ParseDuration`.

---

## 6. HTTP API

All routes are JSON unless noted. Mutations require auth (JWT cookie). `/api/bootstrap`, `/api/auth/*`, and `/api/setup` are public.

```
GET    /api/bootstrap                    → { needs_setup, active_theme }
POST   /api/setup                        body: { username, password, password_confirm }

POST   /api/auth/login                   body: { username, password }
POST   /api/auth/logout
GET    /api/auth/me                      → { username }

GET    /api/services
POST   /api/services                     body: ServiceInput
GET    /api/services/:id
PUT    /api/services/:id                 body: ServiceInput
DELETE /api/services/:id
POST   /api/services/:id/icon            multipart: file
DELETE /api/services/:id/icon

GET    /api/categories
POST   /api/categories
PUT    /api/categories/:id
DELETE /api/categories/:id               (services inside detach, category_id=NULL)

PUT    /api/layout                       body: { services: [...], categories: [...] } bulk update on drag-end

GET    /api/themes
POST   /api/themes                       body: { name, palette, custom_css }
PUT    /api/themes/:id
DELETE /api/themes/:id                   (default theme cannot be deleted)
POST   /api/themes/:id/activate

GET    /api/settings
PUT    /api/settings                     body: { key: value, ... }

GET    /api/healthcheck/status           → { [serviceId]: { primary: "up|down|n/a", alt: "..." } }

GET    /api/icons/:filename              static file serve (no auth — icons are not sensitive)

GET    /api/ws                           WebSocket: server pushes { type: "hc_update", data: ... }
```

`ServiceInput` shape:
```ts
{
  name: string,
  description: string,
  host_primary: string,       // "https://app.lan" or "app.lan:1234"
  port_primary?: number,
  host_alt?: string,
  port_alt?: number,
  category_id?: number | null,
  ping_primary: boolean,
  ping_alt: boolean,
  hc_primary_enabled: boolean,
  hc_primary_url?: string,
  hc_alt_enabled: boolean,
  hc_alt_url?: string,
  layout: { x: number, y: number, w: number, h: number }
}
```

---

## 7. Frontend Routes

| Path | Component | Notes |
|---|---|---|
| `/` | `Dashboard` | Protected. Grid of services + categories. |
| `/auth` | `Auth` | Renders **Login** or **Setup** based on `/api/bootstrap`. |
| `/settings` | `Settings` | Theme editor, grid columns, healthcheck interval, password change. |

The app fetches `/api/bootstrap` once on load. If `needs_setup` → redirect to `/auth` showing the setup form. If unauthenticated → redirect to `/auth` showing login.

---

## 8. Auth Flow

1. **Bootstrap:** Server checks `users` table on every `/api/bootstrap` request. Empty → `needs_setup: true`.
2. **Setup:** `POST /api/setup` validates password match, hashes with argon2id (mem=64MB, time=3, parallel=2), inserts user, sets JWT cookie. Idempotent guard: 409 if users already exist.
3. **Login:** `POST /api/auth/login` verifies hash, issues JWT (HS256, HTTP-only, `SameSite=Strict`, `Secure` when behind TLS — detected via `X-Forwarded-Proto` or scheme), expiry = `LABEXTEND_SESSION_TIMEOUT`.
4. **Logout:** Clears cookie. JWT is stateless so no server invalidation; expiry is the safety net.
5. **Rate limit:** 5 failed logins per IP per 5 min (in-memory token bucket).
6. **Password reset:** No UI flow. The "Forgot password?" link opens a modal with these instructions:
   > Set the environment variable `LABEXTEND_PASSWORD_RESET=true` and restart the container. On next start, the existing user will be deleted and you'll see the setup wizard. After completing setup, remove or set `LABEXTEND_PASSWORD_RESET=false` and restart again.

---

## 9. Theme System

### Storage
A theme is `{ name, palette (JSON of CSS vars), custom_css (string) }`. Exactly one theme has `is_active=1` at any time.

### Default palette (12 vars, locked dark theme defaults)
```
--bg:            #0a0a0a
--bg-card:       #141414
--bg-elevated:   #1c1c1c
--fg:            #e5e5e5
--fg-muted:      #9ca3af
--accent:        #6366f1
--accent-hover:  #818cf8
--border:        #262626
--border-strong: #3f3f46
--danger:        #ef4444
--success:       #22c55e
--warning:       #eab308
```

### Editor (in Settings page)
Two tabs:
- **Quick Edit** — each variable rendered as `[color swatch / picker] [hex input]`. Live preview by writing to `<style id="active-theme">` on the fly.
- **Custom CSS** — Monaco editor with CSS mode, appended after the palette block in the same style tag. Allows arbitrary overrides.

Below the tabs:
- "Theme name" input.
- **Save** button: if name matches existing theme → PUT (overwrite), else POST (create).
- **Theme list:** rows of `[name] [activate] [edit] [delete]`. The seeded "default" theme cannot be deleted.

### Render
On every page mount, `Theme` store fetches active theme and writes:
```html
<style id="active-theme">
  :root { --bg: ...; --bg-card: ...; ... }
  /* custom_css here */
</style>
```
All Tailwind utility colors map through these variables (configured in `tailwind.config.ts` `theme.extend.colors`).

---

## 10. Healthcheck Worker

A single goroutine ticks every `hc_interval`. Each tick:

1. Snapshot all services from DB.
2. For each service, run probes for primary host and alt host **in parallel** (one goroutine per probe, bounded semaphore of 16):
   - **Ping probe:** TCP-connect to `host:port` (3s timeout). Port resolved from explicit field, else parsed from host string, else default 80/443 by scheme.
   - **Health probe:** HTTP GET `hc_url` if set, else derive from host. 5s timeout. 2xx/3xx = up. Custom CA / TLS verify always on.
3. Aggregate results into a map `serviceID → { primary: "up"|"down"|"n/a", alt: ... }`. `"n/a"` when neither toggle is enabled for that host.
4. Compare with previous snapshot; if any value changed, broadcast over the WebSocket hub.
5. Cache latest map for GET `/api/healthcheck/status`.

Worker is started by `main.go` after DB open. Shutdown via context cancellation on SIGTERM.

---

## 11. WebSocket Hub

`/api/ws` accepts authenticated connections (cookie auth on upgrade). Server-push only — clients don't send.

Message envelope: `{ "type": "hc_update", "data": { ... } }`.

Future message types reserved (not implemented yet):
- `"metric"` — CPU/RAM samples per service host.
- `"event"` — generic toast/notification push.

Hub maintains a slice of clients with a mutex; broadcasts iterate and drop slow clients (10s write timeout). On connection close, client is removed.

Frontend: lazy `useWebSocket` hook, auto-reconnect with exponential backoff (1s, 2s, 4s, max 30s). Falls back to polling `/api/healthcheck/status` every 15s if WS unavailable.

---

## 12. Drag & Drop / Grid

- `react-grid-layout` with `cols={settings.grid_cols}` (default 6, range 4–12).
- `compactType=null` and `preventCollision=false` to allow gaps and free placement.
- **Two layouts in parallel:**
  - **Outer layout:** categories + uncategorized services placed in the main grid.
  - **Inner layouts:** each category renders its own nested `react-grid-layout` constrained to its container size, holding its child services.
- On drag-end of any layout, the changed positions are diffed and sent as a single `PUT /api/layout` with both lists. Optimistic update locally.
- **Drop into category:** dragging an uncategorized service over a category triggers `category_id = X`; drag out resets to `NULL`. Handled via `onDrop` from react-dnd HTML5 backend layered alongside RGL drag handles.
- **Category resize:** drag corner handle. If new size < bounding box of children, the children that fall outside have `category_id` set to `NULL` and are placed in the outer grid at their absolute position.
- **Card sizes:** services 1×1 by default, resizable 1–4 wide / 1–3 tall. Categories 2×2 / 3×2 default, resizable 2–6 wide / 2–6 tall.

---

## 13. Icon Handling

- Upload: `POST /api/services/:id/icon` multipart, `file` field.
- Validation: MIME-Type whitelist `image/png`, `image/jpeg`, `image/webp`, `image/svg+xml`. Max 2 MB.
- SVG sanitization: parse with `github.com/microcosm-cc/bluemonday` UGC policy adapted for SVG (allowlist of safe tags, strip `<script>`, `on*` attrs).
- Storage: filename = `<uuid>.<ext>`, written to `$LABEXTEND_DATA_DIR/icons/`. Path stored as `icon_path` (relative: `icons/<uuid>.<ext>`).
- Serving: `GET /api/icons/:filename` (no auth, path-traversal guarded with `filepath.Clean` + prefix check). `Cache-Control: public, max-age=31536000, immutable` (UUIDs make it safe).
- Delete: `DELETE /api/services/:id/icon` removes file and clears `icon_path`.

---

## 14. Security Posture

- **argon2id** for password storage (params: time=3, memory=64MiB, threads=2, keyLen=32, saltLen=16).
- **JWT** HS256 with auto-generated 32-byte secret (persisted in `settings`), HTTP-only `SameSite=Strict` cookie, `Secure` flag when TLS detected.
- **CSRF:** SameSite=Strict cookie + Origin header check on all non-GET requests.
- **Rate limiting:** login endpoint (5/5min/IP), setup endpoint (5/min global).
- **Upload validation:** content-sniffed MIME (don't trust client), size cap, SVG sanitization.
- **Path traversal:** `filepath.Clean` + prefix check on all file serves.
- **Secrets:** never logged, never returned in API responses.
- **Default-deny:** all routes require auth except the explicit public list.

---

## 15. Build & Deployment

### Dockerfile (multi-stage)

```dockerfile
# 1. Frontend build
FROM node:20-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# 2. Go build
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/labextend ./cmd/labextend

# 3. Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/labextend /labextend
VOLUME ["/data"]
EXPOSE 8080
ENV LABEXTEND_DATA_DIR=/data
ENTRYPOINT ["/labextend"]
```

### GitHub Actions (`.github/workflows/build.yml`)

```yaml
name: build
on:
  push: { branches: [main], tags: ['v*'] }
  pull_request: { branches: [main] }
jobs:
  build:
    runs-on: self-hosted
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: cd web && npm ci && npm run build
      - run: go vet ./...
      - run: go test ./...
      - run: CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o labextend ./cmd/labextend
      - uses: docker/login-action@v3
        if: github.event_name != 'pull_request'
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/metadata-action@v5
        id: meta
        with:
          images: ghcr.io/bartis-dev/labextend
          tags: |
            type=ref,event=branch
            type=ref,event=pr
            type=semver,pattern={{version}}
            type=sha,prefix=sha-,format=short
            type=raw,value=latest,enable={{is_default_branch}}
      - uses: docker/build-push-action@v5
        with:
          context: .
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
```

### Makefile targets
- `make dev` — runs Vite dev server + Go server (proxy `/api` from Vite to Go).
- `make build` — builds frontend then Go binary into `./bin/labextend`.
- `make docker` — builds Docker image locally as `labextend:dev`.
- `make clean` — removes `bin/`, `web/dist/`, `data/`.

---

## 16. Testing Strategy

- **Backend:** Go unit tests on `auth` (argon2 roundtrip, JWT issue/verify, duration parser), `healthcheck` (TCP/HTTP probes against `httptest`), `theme` (default seed, activate exclusivity), `db` (migrations idempotent).
- **API:** A handful of `httptest` integration tests for setup → login → CRUD service → layout update → healthcheck status.
- **Frontend:** No formal test suite; manual verification of golden paths. Type-checking + `vite build` in CI as a smoke test.

---

## 17. Open Questions / Future Work

- Theme import/export as files (JSON download/upload).
- Per-service healthcheck interval override.
- Optional system metrics card type (CPU/RAM/Disk via WS).
- Tag/search filter on dashboard.
- Backup/restore button (zip data/ dir).

---

## 18. Implementation Order

Suggested phased build (each phase ends with a working app):

1. **Skeleton:** repo scaffolding, go.mod, Vite project, Tailwind, gitignore, LICENSE, README stub, Dockerfile, Makefile, CI workflow.
2. **Backend core:** config, DB open + migrations, settings store, JWT/argon2 helpers, chi router with middleware (logging, CORS, recover, rate limit, auth).
3. **Auth API:** `/api/bootstrap`, `/api/setup`, `/api/auth/*`. Frontend Auth page (login + setup).
4. **Services & Categories CRUD:** Backend + frontend dashboard rendering cards, no D&D yet.
5. **Drag & drop + grid:** react-grid-layout integration, layout persistence, category nesting.
6. **Icons:** upload, serve, delete, sanitize.
7. **Healthcheck worker + WS hub:** background probes, REST status, WS push, frontend status dots.
8. **Themes:** default seed, palette editor, custom CSS editor, multi-theme save/activate/delete.
9. **Settings polish:** grid_cols, hc_interval, password change form.
10. **Docs & release:** finalize README, tag v0.1.0, push.
