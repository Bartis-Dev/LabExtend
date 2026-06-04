#!/usr/bin/env bash
# Regenerate internal/grpc/pb from proto/manager.proto.
#
# Two modes:
#   1. Local protoc — if `protoc` and the two Go plugins are on PATH, use them.
#      Required versions:
#        - protoc >= 25
#        - protoc-gen-go      → go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#        - protoc-gen-go-grpc → go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   2. Docker fallback — if protoc is missing, run via a pinned bufbuild/buf
#      image. Requires Docker.

set -euo pipefail
cd "$(dirname "$0")/.."

if command -v protoc >/dev/null && command -v protoc-gen-go >/dev/null && command -v protoc-gen-go-grpc >/dev/null; then
  echo "[gen-proto] using local protoc ($(protoc --version))"
  protoc \
    --proto_path=proto \
    --go_out=internal/grpc/pb --go_opt=paths=source_relative \
    --go-grpc_out=internal/grpc/pb --go-grpc_opt=paths=source_relative \
    proto/manager.proto
elif command -v docker >/dev/null; then
  echo "[gen-proto] protoc not found — falling back to bufbuild/buf docker image"
  docker run --rm -v "$PWD":/work -w /work bufbuild/buf:1.45.0 generate
else
  echo "[gen-proto] need either: (a) protoc + protoc-gen-go + protoc-gen-go-grpc on PATH"
  echo "                         or (b) Docker installed"
  exit 1
fi

echo "[gen-proto] done — re-run 'go build ./...' to use the new code"
