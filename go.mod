module github.com/Bartis-Dev/LabExtend

go 1.24

require (
	// HTTP / routing
	github.com/go-chi/chi/v5 v5.1.0
	github.com/joho/godotenv v1.5.1

	// Migrations
	github.com/pressly/goose/v3 v3.22.0

	// Scheduling
	github.com/robfig/cron/v3 v3.0.1

	// Auth
	golang.org/x/crypto v0.27.0 // for argon2 + bcrypt fallback

	// gRPC
	google.golang.org/grpc v1.66.0
	google.golang.org/protobuf v1.34.2

	// SQLite — pure-Go (CGO-free), works in Alpine + scratch images
	modernc.org/sqlite v1.32.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect

	// Utilities
	github.com/google/uuid v1.6.0
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.25.0 // indirect
	golang.org/x/text v0.18.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240604185151-ef581f913117 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)

require (
	github.com/aws/aws-sdk-go-v2 v1.41.11
	github.com/aws/aws-sdk-go-v2/config v1.32.22
	github.com/aws/aws-sdk-go-v2/credentials v1.19.21
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.22.24
	github.com/aws/aws-sdk-go-v2/service/s3 v1.103.1
	github.com/aws/smithy-go v1.27.0
	github.com/pquerna/otp v1.5.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.12 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.27 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.27 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.27 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.28 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.20 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.27 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.27 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.1.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.1 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
)
