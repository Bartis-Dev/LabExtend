# LabExtend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build LabExtend — a self-hosted homelab dashboard (single Go binary, embedded React frontend, SQLite storage, drag-and-drop grid, multi-theme system, JWT auth, healthcheck worker over WebSocket).

**Architecture:** Go 1.23 backend with `chi` router and `modernc.org/sqlite` exposing a JSON REST API + WebSocket. Vite + React + TypeScript + Tailwind frontend embedded via `go:embed`. JWT in HTTP-only cookie with argon2id password hashing. `react-grid-layout` for drag/drop/resize. Multi-stage Docker build on self-hosted GitHub Actions runner publishing to ghcr.io.

**Tech Stack:** Go, chi, modernc.org/sqlite, golang-jwt/jwt v5, coder/websocket, argon2id, React 18, TypeScript, Vite, Tailwind 3, react-grid-layout, @uiw/react-color, @monaco-editor/react, Zustand, TanStack Query.

**Spec:** `docs/superpowers/specs/2026-05-11-labextend-design.md`

**Repo:** https://github.com/Bartis-Dev/LabExtend

**Conventions:**
- One logical task per commit. Conventional Commits format (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`).
- Each task ends with `git add <paths> && git commit -m "<msg>"`.
- Backend tests with stdlib `testing` + `httptest`. Run `go test ./...` after each backend task.
- Frontend: type-check after each task with `npm run typecheck`. Manual smoke-test via `make dev`.

---

## Phase 0 — Repo Scaffolding

### Task 1: Go module + root files

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.dockerignore`
- Create: `LICENSE`
- Create: `README.md` (stub — populated in Task 36)
- Create: `Makefile`

- [ ] **Step 1: Initialize Go module**

```bash
cd C:/Users/Bartis/Desktop/coding/projects/LabExtend
go mod init github.com/Bartis-Dev/LabExtend
```

- [ ] **Step 2: Write `.gitignore`**

```
# Binaries
/bin/
/labextend
/labextend.exe
*.test
*.out

# Go
vendor/

# Node
web/node_modules/
web/dist/
web/.vite/
npm-debug.log*
yarn-debug.log*

# Runtime data
/data/
*.db
*.db-journal
*.db-wal
*.db-shm

# IDE
.vscode/
.idea/
*.swp

# OS
.DS_Store
Thumbs.db
desktop.ini

# Secrets
.env
.env.local
*.pem
*.key
```

- [ ] **Step 3: Write `.dockerignore`**

```
.git
.github
docs
data
web/node_modules
web/dist
bin
*.md
!README.md
```

- [ ] **Step 4: Write `LICENSE` (MIT)**

```
MIT License

Copyright (c) 2026 Bartis-Dev

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 5: Write `README.md` stub**

```markdown
# LabExtend

Self-hosted homelab dashboard. WIP — see `docs/superpowers/specs/` for design.
```

- [ ] **Step 6: Write `Makefile`**

```makefile
.PHONY: dev build docker clean test web-dev web-build

web-build:
	cd web && npm ci && npm run build

build: web-build
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/labextend ./cmd/labextend

dev:
	@echo "Run in two terminals:"
	@echo "  cd web && npm run dev"
	@echo "  go run ./cmd/labextend"

docker:
	docker build -t labextend:dev .

test:
	go test ./...

clean:
	rm -rf bin web/dist data
```

- [ ] **Step 7: Commit**

```bash
git add .gitignore .dockerignore LICENSE README.md Makefile go.mod
git commit -m "chore: initialize Go module and root project files"
```

---

### Task 2: Vite + React + Tailwind scaffold

**Files:**
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/tsconfig.node.json`
- Create: `web/vite.config.ts`
- Create: `web/tailwind.config.ts`
- Create: `web/postcss.config.cjs`
- Create: `web/index.html`
- Create: `web/src/main.tsx`
- Create: `web/src/App.tsx`
- Create: `web/src/styles/globals.css`

- [ ] **Step 1: Create `web/package.json`**

```json
{
  "name": "labextend-web",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "typecheck": "tsc -b --noEmit"
  },
  "dependencies": {
    "@monaco-editor/react": "^4.6.0",
    "@tanstack/react-query": "^5.59.0",
    "@uiw/react-color": "^2.3.4",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-grid-layout": "^1.5.0",
    "react-router-dom": "^6.27.0",
    "zustand": "^5.0.0"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@types/react-grid-layout": "^1.3.5",
    "@vitejs/plugin-react": "^4.3.3",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.47",
    "tailwindcss": "^3.4.14",
    "typescript": "^5.6.3",
    "vite": "^5.4.10"
  }
}
```

- [ ] **Step 2: Write TypeScript config files**

`web/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": { "@/*": ["src/*"] }
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

`web/tsconfig.node.json`:
```json
{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true,
    "strict": true
  },
  "include": ["vite.config.ts"]
}
```

- [ ] **Step 3: Write `web/vite.config.ts`**

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true, ws: true },
    },
  },
  build: { outDir: 'dist', emptyOutDir: true },
});
```

- [ ] **Step 4: Write Tailwind + PostCSS configs**

`web/tailwind.config.ts`:
```ts
import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg:            'var(--bg)',
        'bg-card':     'var(--bg-card)',
        'bg-elevated': 'var(--bg-elevated)',
        fg:            'var(--fg)',
        'fg-muted':    'var(--fg-muted)',
        accent:        'var(--accent)',
        'accent-hover':'var(--accent-hover)',
        border:        'var(--border)',
        'border-strong':'var(--border-strong)',
        danger:        'var(--danger)',
        success:       'var(--success)',
        warning:       'var(--warning)',
      },
    },
  },
  plugins: [],
} satisfies Config;
```

`web/postcss.config.cjs`:
```js
module.exports = { plugins: { tailwindcss: {}, autoprefixer: {} } };
```

- [ ] **Step 5: Write `web/index.html`**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>LabExtend</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 6: Write `web/src/styles/globals.css`**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

:root {
  --bg: #0a0a0a;
  --bg-card: #141414;
  --bg-elevated: #1c1c1c;
  --fg: #e5e5e5;
  --fg-muted: #9ca3af;
  --accent: #6366f1;
  --accent-hover: #818cf8;
  --border: #262626;
  --border-strong: #3f3f46;
  --danger: #ef4444;
  --success: #22c55e;
  --warning: #eab308;
}

html, body, #root { height: 100%; }
body {
  background: var(--bg);
  color: var(--fg);
  font-family: ui-sans-serif, system-ui, -apple-system, sans-serif;
  margin: 0;
}
```

- [ ] **Step 7: Write `web/src/main.tsx` and `App.tsx`**

`web/src/main.tsx`:
```tsx
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './styles/globals.css';
import App from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode><App /></StrictMode>
);
```

`web/src/App.tsx`:
```tsx
export default function App() {
  return <div className="p-8"><h1 className="text-2xl font-bold">LabExtend</h1></div>;
}
```

- [ ] **Step 8: Install + smoke build**

```bash
cd web && npm install && npm run build && cd ..
```

Expected: `web/dist/index.html` exists.

- [ ] **Step 9: Commit**

```bash
git add web/package.json web/package-lock.json web/tsconfig*.json web/vite.config.ts web/tailwind.config.ts web/postcss.config.cjs web/index.html web/src/
git commit -m "feat(web): scaffold Vite + React + Tailwind frontend"
```

---

### Task 3: Go embed wiring + minimal main.go

**Files:**
- Create: `cmd/labextend/main.go`
- Create: `internal/web/embed.go`

- [ ] **Step 1: Write `internal/web/embed.go`**

```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded built frontend (rooted at dist/).
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
```

Note: We use `web/dist` as the build output but `internal/web/dist` for embed. Wire by copying or symlinking. **Decision:** keep build output at `web/dist` and use a build step to copy to `internal/web/dist`. Update Makefile and Dockerfile to copy.

Actually simpler: place the embed file *inside* `web/` so it can `//go:embed all:dist`. Use `internal/web/embed.go` with a relative path that traverses up:

Replace above with:
```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil { panic(err) }
	return sub
}
```

Move the file to `web/embed.go` (package `web`). Update Makefile target paths accordingly.

**Revised file:** `web/embed.go` (NOT `internal/web/embed.go`)

```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil { panic(err) }
	return sub
}
```

- [ ] **Step 2: Write `cmd/labextend/main.go`**

```go
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	web "github.com/Bartis-Dev/LabExtend/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	listen := envDefault("LABEXTEND_LISTEN", "0.0.0.0:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	staticFS := http.FS(web.FS())
	mux.Handle("/", spaHandler(staticFS))

	srv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("server listening", "addr", listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := fsys.Open(r.URL.Path)
		if err != nil {
			r.URL.Path = "/"
		} else {
			_ = f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

func envDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" { return v }
	return fallback
}

var _ = fs.ValidPath
```

- [ ] **Step 3: Smoke test**

```bash
cd web && npm run build && cd ..
go build -o bin/labextend ./cmd/labextend
./bin/labextend &
curl -s http://localhost:8080/api/healthz
curl -s http://localhost:8080/ | head -c 200
kill %1
```

Expected: `{"status":"ok"}` and HTML containing `<div id="root">`.

- [ ] **Step 4: Commit**

```bash
git add cmd/ web/embed.go Makefile go.mod go.sum
git commit -m "feat: embed Vite build into Go binary, add /api/healthz"
```

---

### Task 4: Dockerfile + CI workflow

**Files:**
- Create: `Dockerfile`
- Create: `.github/workflows/build.yml`

- [ ] **Step 1: Write `Dockerfile`** (full content from spec §15)

```dockerfile
FROM node:20-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/labextend ./cmd/labextend

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/labextend /labextend
VOLUME ["/data"]
EXPOSE 8080
ENV LABEXTEND_DATA_DIR=/data
ENTRYPOINT ["/labextend"]
```

- [ ] **Step 2: Write `.github/workflows/build.yml`** (full content from spec §15)

Full YAML as written in spec §15. Self-hosted runner.

- [ ] **Step 3: Smoke test locally**

```bash
docker build -t labextend:dev .
docker run --rm -p 8080:8080 -v $(pwd)/data:/data labextend:dev &
sleep 2
curl -s http://localhost:8080/api/healthz
docker stop $(docker ps -q --filter ancestor=labextend:dev)
```

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .github/
git commit -m "ci: add multi-stage Dockerfile and self-hosted build workflow"
```

---

## Phase 1 — Backend Core

### Task 5: Config (ENV parsing)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

`internal/config/config_test.go`:
```go
package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"7d":    7 * 24 * time.Hour,
		"30m":   30 * time.Minute,
		"3h":    3 * time.Hour,
		"720h":  720 * time.Hour,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil { t.Fatalf("%q: %v", in, err) }
		if got != want { t.Errorf("%q: got %v, want %v", in, got, want) }
	}
	if _, err := ParseDuration("bad"); err == nil { t.Error("expected error for 'bad'") }
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LABEXTEND_LISTEN", "")
	c := Load()
	if c.Listen != "0.0.0.0:8080" { t.Errorf("default Listen = %q", c.Listen) }
	if c.DataDir != "/data" { t.Errorf("default DataDir = %q", c.DataDir) }
	if c.SessionTimeout != 7*24*time.Hour { t.Errorf("default SessionTimeout = %v", c.SessionTimeout) }
}
```

- [ ] **Step 2: Implement**

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen              string
	DataDir             string
	PasswordReset       bool
	SessionTimeout      time.Duration
	HealthcheckInterval time.Duration
	JWTSecret           string // if empty, generated and persisted in DB
	LogLevel            string
}

func Load() Config {
	return Config{
		Listen:              envDefault("LABEXTEND_LISTEN", "0.0.0.0:8080"),
		DataDir:             envDefault("LABEXTEND_DATA_DIR", "/data"),
		PasswordReset:       envBool("LABEXTEND_PASSWORD_RESET", false),
		SessionTimeout:      envDuration("LABEXTEND_SESSION_TIMEOUT", 7*24*time.Hour),
		HealthcheckInterval: envDuration("LABEXTEND_HEALTHCHECK_INTERVAL", 60*time.Second),
		JWTSecret:           os.Getenv("LABEXTEND_JWT_SECRET"),
		LogLevel:            envDefault("LABEXTEND_LOG_LEVEL", "info"),
	}
}

var daysRE = regexp.MustCompile(`^(\d+)d$`)

func ParseDuration(s string) (time.Duration, error) {
	if m := daysRE.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil { return 0, fmt.Errorf("invalid duration %q", s) }
	return d, nil
}

func envDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" { return v }
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" { return fallback }
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" { return fallback }
	d, err := ParseDuration(v)
	if err != nil { return fallback }
	return d
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/config/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add ENV parsing with custom duration format (7d/3h)"
```

---

### Task 6: Database open + migrations

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/migrations.go`
- Create: `internal/db/migrations/0001_init.sql`
- Create: `internal/db/db_test.go`

- [ ] **Step 1: Write `internal/db/migrations/0001_init.sql`** (all tables from spec §4)

Full SQL as in spec section 4, plus indexes:
```sql
-- (full schema from spec §4 — users, services, categories, themes, settings)
CREATE INDEX IF NOT EXISTS idx_services_category ON services(category_id);
CREATE INDEX IF NOT EXISTS idx_themes_active ON themes(is_active);
```

- [ ] **Step 2: Write `internal/db/migrations.go`** with embedded migrations

```go
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil { return err }

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil { return err }
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	applied := map[string]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil { return err }
	for rows.Next() { var v string; rows.Scan(&v); applied[v] = true }
	rows.Close()

	for _, name := range names {
		if applied[name] { continue }
		sqlBytes, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil { return err }
		tx, err := db.Begin()
		if err != nil { return err }
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, strftime('%s','now'))`, name); err != nil {
			tx.Rollback(); return err
		}
		if err := tx.Commit(); err != nil { return err }
	}
	return nil
}
```

- [ ] **Step 3: Write `internal/db/db.go`**

```go
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil { return nil, err }
	dbPath := filepath.Join(dataDir, "labextend.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil { return nil, err }
	if err := db.Ping(); err != nil { db.Close(); return nil, err }
	db.SetMaxOpenConns(1) // SQLite single writer
	return db, nil
}
```

- [ ] **Step 4: Write `internal/db/db_test.go`**

```go
package db

import (
	"testing"
)

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil { t.Fatal(err) }
	defer d.Close()
	if err := Migrate(d); err != nil { t.Fatal(err) }
	if err := Migrate(d); err != nil { t.Fatalf("second migrate: %v", err) }
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	if n == 0 { t.Error("no migrations recorded") }
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/db/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/db/
git commit -m "feat(db): SQLite open with WAL + embedded migrations runner"
```

---

### Task 7: Settings store + JWT secret persistence

**Files:**
- Create: `internal/settings/settings.go`
- Create: `internal/settings/settings_test.go`

- [ ] **Step 1: Implement key-value settings with helpers**

```go
package settings

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Get(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) { return "", nil }
	return v, err
}

func (s *Store) Set(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetInt(key string, def int) (int, error) {
	v, err := s.Get(key); if err != nil { return 0, err }
	if v == "" { return def, nil }
	return strconv.Atoi(v)
}

func (s *Store) GetOrCreateJWTSecret() (string, error) {
	v, err := s.Get("jwt_secret"); if err != nil { return "", err }
	if v != "" { return v, nil }
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil { return "", err }
	v = hex.EncodeToString(b)
	return v, s.Set("jwt_secret", v)
}
```

- [ ] **Step 2: Test**

Verify Get/Set roundtrip + auto JWT secret generation against an in-memory migrated DB.

- [ ] **Step 3: Commit**

```bash
git add internal/settings/
git commit -m "feat(settings): key-value store with JWT secret bootstrap"
```

---

### Task 8: Auth primitives — argon2, JWT, ratelimit

**Files:**
- Create: `internal/auth/password.go`
- Create: `internal/auth/jwt.go`
- Create: `internal/auth/ratelimit.go`
- Create: `internal/auth/password_test.go`
- Create: `internal/auth/jwt_test.go`

- [ ] **Step 1: argon2id password hashing**

`internal/auth/password.go`:
```go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil { return "", err }
	hash := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" { return false, errors.New("invalid hash format") }
	var m, t uint32; var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil { return false, err }
	salt, err := base64.RawStdEncoding.DecodeString(parts[4]); if err != nil { return false, err }
	want, err := base64.RawStdEncoding.DecodeString(parts[5]); if err != nil { return false, err }
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
```

- [ ] **Step 2: JWT issue/verify**

`internal/auth/jwt.go`:
```go
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"u"`
	jwt.RegisteredClaims
}

func Issue(secret []byte, userID int64, username string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		UserID: userID, Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

func Verify(secret []byte, tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok { return nil, errors.New("bad alg") }
		return secret, nil
	})
	if err != nil { return nil, err }
	c, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid { return nil, errors.New("invalid token") }
	return c, nil
}
```

- [ ] **Step 3: In-memory rate limiter**

`internal/auth/ratelimit.go`:
```go
package auth

import (
	"sync"
	"time"
)

type Limiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	max     int
	window  time.Duration
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{hits: map[string][]time.Time{}, max: max, window: window}
}

// Allow records an attempt for key and returns true if under the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock(); defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	hits := l.hits[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) { kept = append(kept, t) }
	}
	if len(kept) >= l.max { l.hits[key] = kept; return false }
	l.hits[key] = append(kept, now)
	return true
}
```

- [ ] **Step 4: Tests**

`internal/auth/password_test.go`:
```go
package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := HashPassword("hunter2"); if err != nil { t.Fatal(err) }
	ok, err := VerifyPassword("hunter2", h); if err != nil || !ok { t.Fatalf("verify good: ok=%v err=%v", ok, err) }
	ok, _ = VerifyPassword("wrong", h); if ok { t.Error("verify wrong should fail") }
}
```

`internal/auth/jwt_test.go`:
```go
package auth

import (
	"testing"
	"time"
)

func TestJWTRoundtrip(t *testing.T) {
	secret := []byte("test-secret-test-secret-test-secret-32b")
	tok, err := Issue(secret, 42, "alice", time.Hour); if err != nil { t.Fatal(err) }
	c, err := Verify(secret, tok); if err != nil { t.Fatal(err) }
	if c.UserID != 42 || c.Username != "alice" { t.Errorf("claims mismatch: %+v", c) }
}
```

- [ ] **Step 5: Run tests + commit**

```bash
go test ./internal/auth/...
git add internal/auth/ go.mod go.sum
git commit -m "feat(auth): argon2id, JWT v5, in-memory ratelimiter"
```

---

### Task 9: Chi router + middleware + wire into main

**Files:**
- Create: `internal/api/router.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/server.go`
- Modify: `cmd/labextend/main.go`

- [ ] **Step 1: Build `Server` struct holding deps (db, config, settings, limiters, jwtSecret)**

`internal/api/server.go`:
```go
package api

import (
	"database/sql"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
)

type Server struct {
	DB         *sql.DB
	Cfg        config.Config
	Settings   *settings.Store
	JWTSecret  []byte
	LoginLimit *auth.Limiter
	SetupLimit *auth.Limiter
}

func New(db *sql.DB, cfg config.Config, st *settings.Store, jwtSecret []byte) *Server {
	return &Server{
		DB: db, Cfg: cfg, Settings: st, JWTSecret: jwtSecret,
		LoginLimit: auth.NewLimiter(5, 5*60*1e9),
		SetupLimit: auth.NewLimiter(5, 60*1e9),
	}
}
```

- [ ] **Step 2: `internal/api/router.go` with chi**

```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) Routes(webHandler http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(middleware.Timeout(60_000_000_000)) // 60s

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealthz)
		r.Get("/bootstrap", s.handleBootstrap)
		// Auth + setup are added in next tasks
	})

	// SPA fallback
	r.NotFound(webHandler.ServeHTTP)
	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("content-type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleBootstrap(w http.ResponseWriter, _ *http.Request) {
	var count int
	s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	writeJSON(w, http.StatusOK, map[string]any{"needs_setup": count == 0})
}
```

- [ ] **Step 3: `internal/api/middleware.go` — auth middleware + helpers**

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

type ctxKey int

const userCtxKey ctxKey = 1

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("labextend_session")
		if err != nil { writeError(w, http.StatusUnauthorized, "unauthorized"); return }
		claims, err := auth.Verify(s.JWTSecret, c.Value)
		if err != nil { writeError(w, http.StatusUnauthorized, "unauthorized"); return }
		ctx := context.WithValue(r.Context(), userCtxKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userFromCtx(r *http.Request) *auth.Claims {
	v, _ := r.Context().Value(userCtxKey).(*auth.Claims)
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 4: Update `main.go` to wire everything**

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/api"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/db"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
	web "github.com/Bartis-Dev/LabExtend/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	database, err := db.Open(cfg.DataDir)
	if err != nil { slog.Error("db open", "err", err); os.Exit(1) }
	defer database.Close()
	if err := db.Migrate(database); err != nil { slog.Error("migrate", "err", err); os.Exit(1) }

	st := settings.New(database)
	if cfg.PasswordReset {
		if _, err := database.Exec(`DELETE FROM users`); err != nil {
			slog.Error("password reset", "err", err); os.Exit(1)
		}
		slog.Warn("LABEXTEND_PASSWORD_RESET=true: users deleted; setup wizard will appear")
	}

	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		jwtSecret, err = st.GetOrCreateJWTSecret()
		if err != nil { slog.Error("jwt secret", "err", err); os.Exit(1) }
	}

	srv := api.New(database, cfg, st, []byte(jwtSecret))
	webHandler := spaHandler(http.FS(web.FS()))
	handler := srv.Routes(webHandler)

	httpSrv := &http.Server{
		Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		slog.Info("server listening", "addr", cfg.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err); os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := fsys.Open(r.URL.Path)
		if err != nil { r.URL.Path = "/" } else { _ = f.Close() }
		fileServer.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 5: Smoke test**

```bash
go build ./...
go test ./...
rm -rf data && bin/labextend &
sleep 1
curl -s localhost:8080/api/bootstrap
# expect: {"needs_setup":true}
kill %1
```

- [ ] **Step 6: Commit**

```bash
git add cmd/ internal/ go.mod go.sum
git commit -m "feat(api): wire chi router, /api/bootstrap, request middleware"
```

---

## Phase 2 — Auth & Setup

### Task 10: Setup, Login, Logout, Me endpoints

**Files:**
- Create: `internal/api/auth.go`
- Modify: `internal/api/router.go`
- Create: `internal/api/auth_test.go`

- [ ] **Step 1: Implement handlers**

`internal/api/auth.go`:
```go
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

type setupReq struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.SetupLimit.Allow("global") { writeError(w, 429, "rate limited"); return }
	var req setupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid json"); return }
	if req.Username == "" || len(req.Password) < 8 { writeError(w, 400, "username required, password >= 8 chars"); return }
	if req.Password != req.PasswordConfirm { writeError(w, 400, "passwords do not match"); return }

	var existing int
	s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&existing)
	if existing > 0 { writeError(w, 409, "already set up"); return }

	hash, err := auth.HashPassword(req.Password)
	if err != nil { writeError(w, 500, "hash error"); return }
	res, err := s.DB.Exec(`INSERT INTO users(username,password_hash,created_at) VALUES (?,?,?)`,
		req.Username, hash, time.Now().Unix())
	if err != nil { writeError(w, 500, "db error"); return }
	id, _ := res.LastInsertId()

	s.issueSession(w, r, id, req.Username)
	writeJSON(w, 200, map[string]string{"username": req.Username})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !s.LoginLimit.Allow(ip) { writeError(w, 429, "too many attempts"); return }
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid json"); return }

	var id int64; var hash string
	err := s.DB.QueryRow(`SELECT id,password_hash FROM users WHERE username=?`, req.Username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) { writeError(w, 401, "invalid credentials"); return }
	if err != nil { writeError(w, 500, "db error"); return }
	ok, err := auth.VerifyPassword(req.Password, hash)
	if err != nil || !ok { writeError(w, 401, "invalid credentials"); return }

	s.issueSession(w, r, id, req.Username)
	writeJSON(w, 200, map[string]string{"username": req.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "labextend_session", Value: "", Path: "/",
		Expires: time.Unix(0, 0), MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(204)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := userFromCtx(r)
	writeJSON(w, 200, map[string]string{"username": c.Username})
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID int64, username string) {
	tok, _ := auth.Issue(s.JWTSecret, userID, username, s.Cfg.SessionTimeout)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name: "labextend_session", Value: tok, Path: "/",
		Expires: time.Now().Add(s.Cfg.SessionTimeout),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure,
	})
}
```

- [ ] **Step 2: Wire routes**

In `router.go`, inside `r.Route("/api", ...)`:
```go
r.Post("/setup", s.handleSetup)
r.Post("/auth/login", s.handleLogin)
r.Post("/auth/logout", s.handleLogout)
r.Group(func(r chi.Router) {
    r.Use(s.requireAuth)
    r.Get("/auth/me", s.handleMe)
})
```

- [ ] **Step 3: Integration test (setup → login → me)**

Use `httptest.NewServer` over a fresh migrated DB. Cookie jar to preserve session.

- [ ] **Step 4: Commit**

```bash
go test ./...
git add internal/api/
git commit -m "feat(auth): setup, login, logout, me endpoints with JWT cookie"
```

---

### Task 11: Frontend API client + bootstrap state

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/types.ts`
- Create: `web/src/store/auth.ts`

- [ ] **Step 1: `web/src/api/client.ts`** — typed fetch wrapper

```ts
export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, credentials: 'include', headers: {} };
  if (body !== undefined) {
    init.headers = { ...init.headers, 'content-type': 'application/json' };
    init.body = JSON.stringify(body);
  }
  const res = await fetch(path, init);
  if (!res.ok) {
    let msg = res.statusText;
    try { const j = await res.json(); if (j.error) msg = j.error; } catch {}
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  get:    <T>(p: string) => request<T>('GET', p),
  post:   <T>(p: string, body?: unknown) => request<T>('POST', p, body),
  put:    <T>(p: string, body?: unknown) => request<T>('PUT', p, body),
  delete: <T>(p: string) => request<T>('DELETE', p),
};
```

- [ ] **Step 2: `web/src/api/types.ts`** — initial DTOs

```ts
export type Bootstrap = { needs_setup: boolean };
export type Me = { username: string };
export type LoginReq = { username: string; password: string };
export type SetupReq = { username: string; password: string; password_confirm: string };
```

- [ ] **Step 3: `web/src/store/auth.ts`** — Zustand auth store

```ts
import { create } from 'zustand';
import { api } from '@/api/client';
import type { Bootstrap, Me, LoginReq, SetupReq } from '@/api/types';

type State = {
  needsSetup: boolean | null;
  user: Me | null;
  loading: boolean;
  bootstrap: () => Promise<void>;
  login: (req: LoginReq) => Promise<void>;
  setup: (req: SetupReq) => Promise<void>;
  logout: () => Promise<void>;
};

export const useAuth = create<State>((set) => ({
  needsSetup: null, user: null, loading: false,
  bootstrap: async () => {
    set({ loading: true });
    const b = await api.get<Bootstrap>('/api/bootstrap');
    let me: Me | null = null;
    try { me = await api.get<Me>('/api/auth/me'); } catch {}
    set({ needsSetup: b.needs_setup, user: me, loading: false });
  },
  login: async (req) => { const me = await api.post<Me>('/api/auth/login', req); set({ user: me }); },
  setup: async (req) => { const me = await api.post<Me>('/api/setup', req); set({ user: me, needsSetup: false }); },
  logout: async () => { await api.post('/api/auth/logout'); set({ user: null }); },
}));
```

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): typed API client + auth Zustand store"
```

---

### Task 12: Auth page (Login + Setup forms)

**Files:**
- Create: `web/src/pages/Auth.tsx`
- Create: `web/src/components/ForgotPasswordModal.tsx`
- Create: `web/src/components/Modal.tsx`
- Modify: `web/src/App.tsx` — add react-router

- [ ] **Step 1: Generic Modal component**

```tsx
import type { ReactNode } from 'react';

export function Modal({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: string; children: ReactNode }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60" onClick={onClose}>
      <div className="w-full max-w-md rounded-lg border border-border bg-bg-card p-6" onClick={(e) => e.stopPropagation()}>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button onClick={onClose} className="text-fg-muted hover:text-fg">×</button>
        </div>
        {children}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: ForgotPasswordModal** — shows instructions text from spec §8

- [ ] **Step 3: Auth page** — renders setup form if `needsSetup`, else login. Both submit through Zustand. On success, navigate to `/`.

```tsx
// abbreviated layout — centered card, dark, max-w-md
// Login: username, password, submit, "Forgot password?" link
// Setup: username, password, repeat password, submit
```

- [ ] **Step 4: Update App.tsx with routes**

```tsx
import { BrowserRouter, Route, Routes, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useEffect } from 'react';
import { useAuth } from '@/store/auth';
import Auth from '@/pages/Auth';
// Dashboard, Settings stubs

const qc = new QueryClient();

function RequireAuth({ children }: { children: JSX.Element }) {
  const { user, needsSetup } = useAuth();
  if (needsSetup) return <Navigate to="/auth" replace />;
  if (!user) return <Navigate to="/auth" replace />;
  return children;
}

export default function App() {
  const bootstrap = useAuth((s) => s.bootstrap);
  useEffect(() => { bootstrap(); }, [bootstrap]);
  return (
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <Routes>
          <Route path="/auth" element={<Auth />} />
          <Route path="/" element={<RequireAuth><Dashboard /></RequireAuth>} />
          <Route path="/settings" element={<RequireAuth><Settings /></RequireAuth>} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
```

- [ ] **Step 5: Smoke test**

`make dev`: setup wizard appears on first load; complete it; redirects to dashboard.

- [ ] **Step 6: Commit**

```bash
git add web/src/
git commit -m "feat(web): auth page with login + setup wizard + forgot password modal"
```

---

### Task 13: Navbar + Dashboard/Settings page stubs

**Files:**
- Create: `web/src/components/Navbar.tsx`
- Create: `web/src/pages/Dashboard.tsx`
- Create: `web/src/pages/Settings.tsx`
- Create: `web/src/components/Layout.tsx`

- [ ] **Step 1: Navbar — title left, gear + logout right**

```tsx
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/store/auth';
import { SettingsIcon, LogOutIcon } from './icons';

export function Navbar() {
  const logout = useAuth((s) => s.logout);
  const nav = useNavigate();
  return (
    <nav className="flex h-14 items-center justify-between border-b border-border bg-bg-card px-6">
      <Link to="/" className="font-bold tracking-wide">LabExtend</Link>
      <div className="flex items-center gap-2">
        <Link to="/settings" className="rounded p-2 hover:bg-bg-elevated" aria-label="Settings"><SettingsIcon /></Link>
        <button onClick={async () => { await logout(); nav('/auth'); }} className="rounded p-2 hover:bg-bg-elevated" aria-label="Logout"><LogOutIcon /></button>
      </div>
    </nav>
  );
}
```

- [ ] **Step 2: Layout wrapper** — Navbar + outlet for protected pages.

- [ ] **Step 3: Inline SVG icon components** (gear, leave, plus, edit, trash, x, drag-handle) in `web/src/components/icons.tsx`.

- [ ] **Step 4: Dashboard and Settings stubs** (just title + "coming soon"). Wrap protected routes in `<Layout>`.

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): navbar with logout/settings, layout wrapper, page stubs"
```

---

## Phase 3 — Services & Categories CRUD

### Task 14: Services CRUD backend

**Files:**
- Create: `internal/api/services.go`
- Create: `internal/api/services_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Service model + handlers (Create/Read/Update/Delete/List)**

Define `Service` struct mirroring DB columns + `serviceInput` for body parsing. All handlers behind `requireAuth`. Standard JSON.

Key signatures:
```go
func (s *Server) listServices(w http.ResponseWriter, r *http.Request)
func (s *Server) createService(w http.ResponseWriter, r *http.Request)
func (s *Server) getService(w http.ResponseWriter, r *http.Request)
func (s *Server) updateService(w http.ResponseWriter, r *http.Request)
func (s *Server) deleteService(w http.ResponseWriter, r *http.Request)
```

- [ ] **Step 2: Routes**

```go
r.Group(func(r chi.Router) {
    r.Use(s.requireAuth)
    r.Get("/services", s.listServices)
    r.Post("/services", s.createService)
    r.Get("/services/{id}", s.getService)
    r.Put("/services/{id}", s.updateService)
    r.Delete("/services/{id}", s.deleteService)
})
```

- [ ] **Step 3: Tests** — full CRUD roundtrip via httptest.

- [ ] **Step 4: Commit**

```bash
git add internal/api/
git commit -m "feat(api): services CRUD"
```

---

### Task 15: Categories CRUD + layout bulk update

**Files:**
- Create: `internal/api/categories.go`
- Create: `internal/api/layout.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Categories CRUD** mirroring services pattern.

- [ ] **Step 2: Layout bulk update** — accepts `{services:[{id,x,y,w,h}], categories:[{id,x,y,w,h}]}` and runs both in a single tx.

- [ ] **Step 3: When deleting a category, set `category_id=NULL` on its services in the same tx.**

- [ ] **Step 4: Tests + commit**

```bash
git add internal/api/
git commit -m "feat(api): categories CRUD + bulk layout update"
```

---

### Task 16: Frontend services/categories store + form modal

**Files:**
- Create: `web/src/api/types.ts` (extend with Service, Category, ServiceInput)
- Create: `web/src/store/services.ts`
- Create: `web/src/components/ServiceForm.tsx`

- [ ] **Step 1: Extend types**

```ts
export type Service = {
  id: number; name: string; description: string;
  host_primary: string; port_primary?: number;
  host_alt?: string; port_alt?: number;
  icon_path?: string; category_id?: number | null;
  layout: { x: number; y: number; w: number; h: number };
  ping_primary: boolean; ping_alt: boolean;
  hc_primary_enabled: boolean; hc_primary_url?: string;
  hc_alt_enabled: boolean; hc_alt_url?: string;
};
export type Category = {
  id: number; name: string; border_color: string;
  layout: { x: number; y: number; w: number; h: number };
};
```

- [ ] **Step 2: TanStack Query hooks** for list/create/update/delete.

- [ ] **Step 3: ServiceForm** — modal with all fields. Toggle blocks for ping and healthcheck per host. Category dropdown sourced from categories query.

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): services/categories types, queries, service form modal"
```

---

### Task 17: Dashboard plain-list rendering (no D&D yet)

**Files:**
- Create: `web/src/components/ServiceCard.tsx`
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: ServiceCard** — name, description, primary/alt host links (`target="_blank" rel="noopener noreferrer"`), edit/delete icons (delete opens confirm modal).

- [ ] **Step 2: Dashboard** — "Add service" button + flat list of cards. No grid yet.

- [ ] **Step 3: Smoke test** — create/edit/delete a service.

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): basic service cards on dashboard (plain list)"
```

---

## Phase 4 — Drag & Drop Grid

### Task 18: react-grid-layout outer grid

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`
- Create: `web/src/components/Grid.tsx`
- Modify: `web/src/styles/globals.css` — RGL CSS

- [ ] **Step 1: Import RGL CSS in globals.css**

```css
@import 'react-grid-layout/css/styles.css';
@import 'react-resizable/css/styles.css';
```

- [ ] **Step 2: Grid component** — wraps `ResponsiveGridLayout` (or `GridLayout`) with our cols setting and `compactType={null}`, `preventCollision={false}`. Emits `(layout) => void` on drag/resize end.

- [ ] **Step 3: Dashboard** — composes Grid with ServiceCard children. On layout change, debounce + send `PUT /api/layout`.

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): drag/drop + resize grid for services (outer layout only)"
```

---

### Task 19: Categories on grid + nested layout

**Files:**
- Create: `web/src/components/CategoryCard.tsx`
- Modify: `web/src/components/Grid.tsx`
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: CategoryCard** — colored border, title bar, nested `GridLayout` inside for child services.

- [ ] **Step 2: Outer grid** renders categories + uncategorized services. Inner grid inside each category renders that category's services.

- [ ] **Step 3: Layout-change handler** — separate handlers for outer and per-category inner; bulk update both lists in one PUT.

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): categories on grid with nested service layout"
```

---

### Task 20: Drop into category + child detach on shrink

**Files:**
- Modify: `web/src/components/Grid.tsx`
- Modify: `web/src/components/CategoryCard.tsx`

- [ ] **Step 1: Drag from outer grid → drop on category** — use RGL's `onDrop` API or HTML5 dnd overlay. On drop, set `category_id` and add to that category's layout.

- [ ] **Step 2: On category resize**, compute child bounding box; any child whose `x+w > catW` or `y+h > catH` is detached (`category_id=null`) and moved to outer grid at the original absolute position.

- [ ] **Step 3: Edge cases** — never crash on invalid layout. Defensive clamp on positions.

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): drop-into-category + auto-detach on shrink"
```

---

## Phase 5 — Icons

### Task 21: Icon upload + serve backend

**Files:**
- Create: `internal/api/icons.go`
- Modify: `internal/api/router.go`
- Modify: `go.mod` (add `bluemonday`, `google/uuid`)

- [ ] **Step 1: Upload handler** — multipart, MIME sniff (`http.DetectContentType`), whitelist, max 2MB, UUID filename, write to `<DataDir>/icons/`. Update `services.icon_path`.

- [ ] **Step 2: SVG sanitization** — if SVG, parse and rewrite via bluemonday with custom SVG-safe policy (allow `svg, g, path, circle, rect, polygon, polyline, line, text, defs, lineargradient, radialgradient, stop, mask, clippath`; strip `script`, `on*` attrs, `xlink:href` to http/https only).

- [ ] **Step 3: Serve handler** at `/api/icons/{filename}` — `filepath.Clean`, prefix check, `http.ServeFile`, `Cache-Control: public, max-age=31536000, immutable`.

- [ ] **Step 4: Delete icon endpoint** — removes file and clears `icon_path`.

- [ ] **Step 5: Tests** — upload happy path, oversize rejection, MIME rejection, traversal attempt rejection.

- [ ] **Step 6: Commit**

```bash
git add internal/api/ go.mod go.sum
git commit -m "feat(icons): upload, sanitize, serve, delete with traversal guards"
```

---

### Task 22: Icon UI on service form + dashboard cards

**Files:**
- Modify: `web/src/components/ServiceForm.tsx`
- Modify: `web/src/components/ServiceCard.tsx`

- [ ] **Step 1: Form** — file input + preview, "Remove icon" button.

- [ ] **Step 2: Card** — render icon at top-left if present; otherwise initials avatar (first letter of name on `--accent` background).

- [ ] **Step 3: Commit**

```bash
git add web/src/
git commit -m "feat(web): icon upload UI and initial-letter fallback"
```

---

## Phase 6 — Healthcheck + WebSocket

### Task 23: Healthcheck worker

**Files:**
- Create: `internal/healthcheck/worker.go`
- Create: `internal/healthcheck/probes.go`
- Create: `internal/healthcheck/hub.go`
- Create: `internal/healthcheck/worker_test.go`

- [ ] **Step 1: Probes** — `tcpProbe(host string, port int, timeout time.Duration) bool` and `httpProbe(url string, timeout time.Duration) bool`.

- [ ] **Step 2: URL/port parsing helper** — given `host_primary` + `port_primary`, derive `(scheme, host, port, hcURL)`.

- [ ] **Step 3: Worker.Run(ctx)** — ticker loop; on each tick, snapshot services, run probes with bounded concurrency (semaphore size 16), build status map, push to Hub if changed, save to in-memory snapshot.

- [ ] **Step 4: Hub** — `Subscribe() chan StatusMap`, `Publish(StatusMap)`, `Snapshot() StatusMap`. Subscribers receive on a buffered channel; slow consumers are dropped after a write deadline.

- [ ] **Step 5: Tests** — TCP probe against `httptest.NewServer` listener, HTTP probe against a 200 / 503 handler, worker with one service produces one update.

- [ ] **Step 6: Commit**

```bash
git add internal/healthcheck/
git commit -m "feat(healthcheck): TCP+HTTP probes, ticker worker, in-memory hub"
```

---

### Task 24: WebSocket endpoint + REST status

**Files:**
- Create: `internal/api/ws.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/server.go` (hold `*healthcheck.Hub`)
- Modify: `cmd/labextend/main.go` (start worker)
- Add dep: `github.com/coder/websocket`

- [ ] **Step 1: REST status endpoint** — `GET /api/healthcheck/status` returns `Hub.Snapshot()`.

- [ ] **Step 2: WebSocket handler** — upgrade with `websocket.Accept`, cookie-based auth on the original request, on connect send snapshot, then loop sending hub messages until client disconnects. Use 10s write timeout.

- [ ] **Step 3: main.go** — instantiate worker + hub, start with errgroup, pass into Server.

- [ ] **Step 4: Commit**

```bash
go test ./...
git add internal/ cmd/ go.mod go.sum
git commit -m "feat(api): /api/healthcheck/status + /api/ws live push"
```

---

### Task 25: Frontend status dots + WS hook with polling fallback

**Files:**
- Create: `web/src/api/ws.ts`
- Create: `web/src/store/healthcheck.ts`
- Modify: `web/src/components/ServiceCard.tsx`

- [ ] **Step 1: `ws.ts`** — `connectHC(onMessage)` opens `/api/ws`, auto-reconnect exponential backoff (1s, 2s, 4s, max 30s), returns close function.

- [ ] **Step 2: Zustand store** — holds `Record<serviceId, {primary, alt}>`. On mount, fetch `/api/healthcheck/status`, subscribe to WS, on WS failure fall back to polling `/api/healthcheck/status` every 15s.

- [ ] **Step 3: ServiceCard** — render small colored dot next to each host label: green/red/grey.

- [ ] **Step 4: Smoke** — add a service, toggle ping on primary, see green/red dot.

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): live status dots via WebSocket + polling fallback"
```

---

## Phase 7 — Themes

### Task 26: Theme backend (seed default, CRUD, activate)

**Files:**
- Modify: `internal/db/migrations/0001_init.sql` — add seed INSERT for default theme
- Create: `internal/api/themes.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/bootstrap.go` (or wherever bootstrap lives) — include `active_theme`

- [ ] **Step 1: Seed default theme** in migration with the 12 vars from spec §9.

- [ ] **Step 2: CRUD handlers**

- `GET /api/themes` → `[{id,name,palette,custom_css,is_active,is_default}]`
- `POST /api/themes` body `{name,palette,custom_css}` (409 if name exists)
- `PUT /api/themes/:id`
- `DELETE /api/themes/:id` (403 if `is_default=1`; if deleting active, activate default)
- `POST /api/themes/:id/activate` (clear is_active on others, set on this)

- [ ] **Step 3: Bootstrap includes active theme** so the very first paint can apply correct colors before any other fetches.

- [ ] **Step 4: Tests + commit**

```bash
go test ./...
git add internal/
git commit -m "feat(themes): backend CRUD, default seed, active-theme bootstrap"
```

---

### Task 27: Theme apply in frontend + Quick Edit palette UI

**Files:**
- Create: `web/src/store/theme.ts`
- Create: `web/src/components/ThemeStyle.tsx`
- Create: `web/src/pages/Settings.tsx` (replace stub)
- Create: `web/src/components/theme/PaletteEditor.tsx`

- [ ] **Step 1: ThemeStyle component** — writes `<style id="active-theme">:root { ... } customCSS</style>` into `<head>`.

- [ ] **Step 2: useTheme store** — fetches active theme from bootstrap; setActive(themeId), updateLocalPalette(palette), updateLocalCustomCSS(css), saveAs(name).

- [ ] **Step 3: PaletteEditor** — grid of 12 rows: label + `<ColorPicker>` (from `@uiw/react-color`) + hex input. On change, push to store for live preview.

- [ ] **Step 4: Settings page** — section "Theme" containing:
  - Theme list (active marker, switch, edit, delete)
  - "Theme name" input + "Save" button (POST if new, PUT if existing)
  - Tabs: **Quick Edit** (PaletteEditor) / **Custom CSS** (Task 28)

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): theme apply + Quick Edit palette tab + theme list"
```

---

### Task 28: Custom CSS Monaco tab

**Files:**
- Create: `web/src/components/theme/CustomCssEditor.tsx`
- Modify: `web/src/pages/Settings.tsx`

- [ ] **Step 1: Lazy-load Monaco** with `React.lazy` to avoid bloating the main bundle.

- [ ] **Step 2: Editor** with `language="css"`, dark theme. On change, push to store for live preview.

- [ ] **Step 3: Commit**

```bash
git add web/src/
git commit -m "feat(themes): Custom CSS Monaco editor tab"
```

---

## Phase 8 — Settings Polish

### Task 29: Settings UI — grid cols, hc interval, password change

**Files:**
- Create: `internal/api/settings.go`
- Modify: `internal/api/router.go`
- Modify: `web/src/pages/Settings.tsx`

- [ ] **Step 1: Backend** — `GET /api/settings` returns whitelisted keys (no `jwt_secret`!); `PUT /api/settings` updates whitelisted keys with validation (grid_cols 4-12, hc_interval 10s-3600s parsed as duration).

- [ ] **Step 2: Password change endpoint** — `PUT /api/auth/password` body `{current,new,new_confirm}`; verify current, update hash.

- [ ] **Step 3: Settings page sections** — Theme (already), Layout (grid cols select 4/5/6/7/8), Healthcheck (interval input), Account (password change form).

- [ ] **Step 4: Commit**

```bash
go test ./...
git add internal/ web/src/
git commit -m "feat(settings): grid cols, healthcheck interval, password change"
```

---

## Phase 9 — Docs & Release

### Task 30: README.md as full user docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write README** covering:

```markdown
# LabExtend

Self-hosted homelab dashboard. Single binary. Drag-and-drop. Multi-theme. Live healthchecks.

## Features
- Service cards with drag/drop, resize, groupings
- Multi-theme system (quick palette + raw CSS), live preview
- HTTP + TCP healthchecks streamed over WebSocket
- Custom icon upload (PNG, JPG, WebP, sanitized SVG)
- First-run setup wizard, JWT cookie auth (argon2id)
- SQLite, no external dependencies

## Quick Start (Docker)
```bash
docker run -d \
  --name labextend \
  -p 8080:8080 \
  -v labextend-data:/data \
  ghcr.io/bartis-dev/labextend:latest
```
Open http://localhost:8080 and complete the setup wizard.

## Environment Variables
| Variable | Default | Description |
| ... full table from spec §5 ... |

## Setup
1. First start opens the setup wizard at `/auth`.
2. Choose username + password (min 8 chars).
3. You're in.

## Forgot password / reset
Set `LABEXTEND_PASSWORD_RESET=true`, restart. The wizard returns. Unset and restart.

## Backup
The entire app state lives in `/data` (the volume). Stop the container, copy `/data`, done.

## Build from source
```bash
make build      # web + go binary in ./bin/labextend
make docker     # docker image labextend:dev
make dev        # see two-terminal hints
```

## Contributing
PRs welcome. License: MIT.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: write user-facing README with env table and quickstart"
```

---

### Task 31: First release tag

**Files:** (none)

- [ ] **Step 1: Final smoke test**

```bash
make clean && make build && bin/labextend &
# Open http://localhost:8080 and walk through: setup → create service → drag → theme edit → logout
kill %1
```

- [ ] **Step 2: Tag**

```bash
git tag -a v0.1.0 -m "labextend v0.1.0 — initial release"
```

- [ ] **Step 3: Push (when ready)**

```bash
git push -u origin main
git push origin v0.1.0
```

---

## Spec Coverage Check (Self-Review)

| Spec section | Implemented in tasks |
|---|---|
| §1 Goals | All tasks |
| §2 Tech stack | Tasks 1, 2, 6, 8, 21, 24 (deps added inline) |
| §3 Project structure | Task 1 (root), Task 2 (web/), Task 5+ (internal/) |
| §4 Data model | Task 6 (init migration), Task 26 (default theme seed) |
| §5 ENVs | Task 5 |
| §6 HTTP API | Tasks 9, 10, 14, 15, 21, 23-24, 26, 29 |
| §7 Frontend routes & shell | Tasks 12, 13, 27 |
| §8 Auth flow | Tasks 8, 10, 12 |
| §9 Theme system | Tasks 26, 27, 28 |
| §10 Healthcheck worker | Task 23 |
| §11 WebSocket hub | Tasks 23 (hub), 24 (endpoint), 25 (client) |
| §12 Grid/D&D | Tasks 18, 19, 20 |
| §13 Icons | Tasks 21, 22 |
| §14 Security | Tasks 8, 10 (ratelimit, argon2, cookie flags), 21 (uploads), 23 (path traversal) |
| §15 Build & deploy | Tasks 1 (Makefile), 4 (Dockerfile + CI) |
| §16 Testing | Tests in tasks 5, 6, 8, 10, 14, 15, 21, 23, 24, 29 |
