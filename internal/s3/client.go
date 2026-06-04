// Package s3 wraps aws-sdk-go-v2 with Hetzner-friendly defaults:
//   - virtual-hosted vs path-style toggle (Hetzner wants path-style)
//   - default region "eu-central"
//   - endpoint URL stored per-config so we can target fsn1 / nbg1 etc.
//
// The same client serves two callers:
//   - leader: list buckets, list/upload/download/delete objects from the UI
//   - agent : streamed multipart upload of backup tar streams
package s3

import (
	"context"
	"errors"
	"io"
	"time"
)

// Credentials is the minimal set needed to talk to any S3-compatible endpoint.
type Credentials struct {
	AccessKey string
	SecretKey string
}

// EndpointConfig describes one S3 endpoint as stored in the `s3_endpoints`
// table (decrypted).
type EndpointConfig struct {
	Endpoint  string // https://fsn1.your-objectstorage.com
	Region    string // eu-central
	PathStyle bool
	Creds     Credentials
}

// Client wraps an aws-sdk-go-v2 S3 client + uploader.
type Client struct {
	cfg EndpointConfig
	// TODO(phase 8):
	//   raw      *s3.Client
	//   uploader *manager.Uploader (5 MiB parts, 5 concurrent)
}

// NewClient builds an S3 client configured for the given endpoint.
// TODO(phase 8): construct aws.Config with credentials.NewStaticCredentialsProvider,
// region from cfg, BaseEndpoint to cfg.Endpoint, UsePathStyle from cfg.PathStyle.
func NewClient(_ context.Context, cfg EndpointConfig) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("endpoint URL required")
	}
	if cfg.Region == "" {
		cfg.Region = "eu-central"
	}
	return &Client{cfg: cfg}, nil
}

// ListBuckets is the cheap smoke test for an endpoint.
// TODO(phase 8).
func (c *Client) ListBuckets(_ context.Context) ([]string, error) {
	return nil, errors.New("ListBuckets: TODO(phase 8)")
}

// Object is one row returned by ListObjects.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	IsFolder     bool // synthesized from CommonPrefixes
}

// ListObjects lists objects under prefix. Pass continuation="" for the first
// page; pass the returned NextContinuationToken for the next.
// TODO(phase 8).
func (c *Client) ListObjects(_ context.Context, bucket, prefix, continuation string) (objs []Object, next string, err error) {
	_, _, _ = bucket, prefix, continuation
	return nil, "", errors.New("ListObjects: TODO(phase 8)")
}

// PutObject uploads a single object (small files via the UI go through here).
// TODO(phase 8).
func (c *Client) PutObject(_ context.Context, bucket, key string, body io.Reader, contentType string) error {
	_, _, _, _ = bucket, key, body, contentType
	return errors.New("PutObject: TODO(phase 8)")
}

// GetObject opens a streaming reader for download.
// TODO(phase 8).
func (c *Client) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	_, _ = bucket, key
	return nil, errors.New("GetObject: TODO(phase 8)")
}

// DeleteObjects batch-deletes up to 1000 keys per call.
// TODO(phase 8).
func (c *Client) DeleteObjects(_ context.Context, bucket string, keys []string) (deleted int, err error) {
	_, _ = bucket, keys
	return 0, errors.New("DeleteObjects: TODO(phase 8)")
}

// UploadStream pipes body straight to S3 using multipart upload — the
// backup runner's primary call site.
// TODO(phase 9): wire manager.Uploader (5 MiB parts, 5 concurrent).
func (c *Client) UploadStream(_ context.Context, bucket, key string, body io.Reader) (bytesUploaded int64, err error) {
	_, _, _ = bucket, key, body
	return 0, errors.New("UploadStream: TODO(phase 9)")
}
