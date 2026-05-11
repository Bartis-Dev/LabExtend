.PHONY: dev build docker clean test web-build web-install

web-install:
	cd web && npm ci

web-build: web-install
	cd web && npm run build

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
