# syntax=docker/dockerfile:1.7

# 1. Frontend build
FROM node:20-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ .
RUN npm run build

# 2. Go build
FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/labextend \
    ./cmd/labextend

# Pre-create /data with nonroot ownership so the runtime volume inherits
# writable permissions when the named volume is first populated.
# distroless 'nonroot' = UID/GID 65532.
RUN mkdir -p /out/data && chown -R 65532:65532 /out/data

# 3. Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build /out/labextend /labextend
VOLUME ["/data"]
EXPOSE 10000 10001
ENV LABEXTEND_DATA_DIR=/data \
    LABEXTEND_LISTEN=0.0.0.0:10000 \
    LABEXTEND_TLS_LISTEN=0.0.0.0:10001
ENTRYPOINT ["/labextend"]
