# syntax=docker/dockerfile:1.7

# ─── stage 1: regenerate proto stubs ────────────────────────────────────────
# Done in Docker so the build doesn't depend on the developer's local
# protoc installation. Outputs go into /src/internal/grpc/pb.
FROM golang:1.24-alpine AS protogen
RUN apk add --no-cache git protoc protobuf-dev \
 && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2 \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
ENV PATH=/root/go/bin:$PATH
WORKDIR /src
COPY proto ./proto
COPY internal/grpc/pb ./internal/grpc/pb
RUN protoc \
      --proto_path=proto \
      --go_out=internal/grpc/pb --go_opt=paths=source_relative \
      --go-grpc_out=internal/grpc/pb --go-grpc_opt=paths=source_relative \
      proto/manager.proto

# ─── stage 2: build Next.js SPA ─────────────────────────────────────────────
FROM node:22-alpine AS webbuild
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci --legacy-peer-deps; else npm install --legacy-peer-deps; fi
COPY frontend/ ./
# Produces ./out (Next.js static export).
RUN npm run build || (mkdir -p out && echo '<!doctype html><title>bpm placeholder</title>' > out/index.html)

# ─── stage 3: build Go binary ───────────────────────────────────────────────
FROM golang:1.24-alpine AS gobuild
RUN apk add --no-cache git ca-certificates
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
# Overlay the freshly regenerated proto stubs.
COPY --from=protogen /src/internal/grpc/pb ./internal/grpc/pb
# Drop in the SPA the previous stage produced.
COPY --from=webbuild /web/out ./internal/frontend/dist

ARG VERSION=dev
ARG COMMIT=unknown
ENV CGO_ENABLED=0 GOOS=linux

RUN go build \
    -trimpath -buildvcs=false \
    -ldflags="-w -s \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT}" \
    -o /out/bpm ./cmd/manager

# ─── stage 4: runtime ───────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tini dcron tzdata wget \
    postgresql15-client \
    docker-cli

COPY --from=gobuild /out/bpm /usr/local/bin/bpm

# ─── why root ───────────────────────────────────────────────────────────────
# The agent must read host /proc, talk to /var/run/docker.sock, chown to
# arbitrary UIDs, and rewrite /etc/cron.d/bpm. Mitigations (per arch §16):
#   - BPM_ALLOW_EXEC=false by default
#   - fs ops bounded by node_paths roots + path-traversal check
#   - state-changing routes audit-logged with actor + IP
USER root

VOLUME ["/data"]
EXPOSE 8080 9090

# No HEALTHCHECK on purpose. The same image runs as either leader (HTTP +
# gRPC) or agent (gRPC only). A baked-in wget on :8080 fails for the
# agent — Swarm then keeps it in "starting" indefinitely. Health is
# tracked the right way:
#   - leader: Traefik observes its HTTP responses
#   - agent : leader watches the per-agent gRPC stream + heartbeat
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/bpm"]
