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
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

// staticEndpointResolver bypasses the aws-sdk-go-v2 endpoint-rules engine
// entirely. The rules engine validates region as a "DNS name" and refuses
// anything that doesn't match (e.g. "eu-central" fails with
// `Invalid region: region was not a valid DNS name`). For S3-compatible
// providers (Hetzner, Backblaze, MinIO, …) we always know the exact URL
// up-front — the rules engine is just noise.
type staticEndpointResolver struct {
	uri        url.URL
	bucketSub  bool // virtual-hosted (false = path style)
}

func newStaticResolver(rawURL string, pathStyle bool) (*staticEndpointResolver, error) {
	// Tolerate stored values without a scheme — Hetzner's docs show endpoints
	// as bare hostnames (nbg1.your-objectstorage.com), and users naturally
	// paste them that way. Default to https.
	rawURL = strings.TrimSpace(rawURL)
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint %q: %w", rawURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("endpoint must be a full URL (scheme + host), got %q", rawURL)
	}
	// Strip path + query + fragment. The R2 dashboard hands users a URL
	// like https://<account>.r2.cloudflarestorage.com/<bucket> — users
	// (correctly per its UI) paste the whole thing. With the bucket in
	// the endpoint path, path-style requests build /<bucket>/<bucket>/<key>
	// which HeadBucket *sometimes* tolerates but PutObject / multipart
	// uploads sign+route against, producing 403 SignatureDoesNotMatch.
	// The S3 endpoint URL is by definition the host only.
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &staticEndpointResolver{uri: *u, bucketSub: !pathStyle}, nil
}

// ResolveEndpoint returns our fixed URL. For path-style we always return the
// same host (the bucket goes in the path). For virtual-hosted we prepend the
// bucket as a subdomain.
func (r *staticEndpointResolver) ResolveEndpoint(_ context.Context, params awss3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	out := r.uri
	if r.bucketSub && params.Bucket != nil && *params.Bucket != "" {
		out.Host = *params.Bucket + "." + out.Host
	}
	return smithyendpoints.Endpoint{URI: out}, nil
}

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
	cfg      EndpointConfig
	raw      *awss3.Client
	uploader *manager.Uploader
}

// NewClient builds an S3 client configured for the given endpoint.
//
// The region is only used for SigV4 signing — endpoint resolution is fully
// bypassed via staticEndpointResolver, so values like "eu-central" or
// custom location codes ("nbg1") don't trip the aws-sdk-go-v2 endpoint
// rules engine.
func NewClient(ctx context.Context, ep EndpointConfig) (*Client, error) {
	if ep.Endpoint == "" {
		return nil, errors.New("endpoint URL required")
	}
	if ep.Region == "" {
		// us-east-1 is the universal default that every S3-compatible
		// provider accepts in SigV4. Hetzner, Backblaze, MinIO, Wasabi
		// all happily sign with this.
		ep.Region = "us-east-1"
	}
	resolver, err := newStaticResolver(ep.Endpoint, ep.PathStyle)
	if err != nil {
		return nil, err
	}
	awscfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(ep.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ep.Creds.AccessKey, ep.Creds.SecretKey, "")),
		// Critical for S3-compatible providers (Cloudflare R2, Hetzner,
		// Backblaze, MinIO). aws-sdk-go-v2 v1.30+ defaults to
		// "WhenSupported" which adds an x-amz-checksum-crc32 header to
		// every PUT / multipart upload. R2 + Hetzner reject the unknown
		// header — and worse, return 403 AccessDenied instead of 400
		// BadRequest, making the cause invisible. "WhenRequired" only
		// sends a checksum when the caller explicitly sets one, which
		// is what these providers expect.
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	raw := awss3.NewFromConfig(awscfg, func(o *awss3.Options) {
		o.EndpointResolverV2 = resolver
		o.UsePathStyle = ep.PathStyle
		o.Region = ep.Region
	})
	up := manager.NewUploader(raw, func(u *manager.Uploader) {
		u.PartSize = 8 * 1024 * 1024 // 8 MiB parts
		u.Concurrency = 4
	})
	return &Client{cfg: ep, raw: raw, uploader: up}, nil
}

// ListBuckets returns every bucket on the endpoint — cheap smoke test.
func (c *Client) ListBuckets(ctx context.Context) ([]string, error) {
	out, err := c.raw.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		if b.Name != nil {
			names = append(names, *b.Name)
		}
	}
	return names, nil
}

// HeadBucket is a cheap "does this bucket exist and can I access it" probe.
// Used as a connectivity fallback when ListBuckets is forbidden (typical
// for Hetzner Object Storage credentials that are scoped to one bucket).
func (c *Client) HeadBucket(ctx context.Context, bucket string) error {
	_, err := c.raw.HeadBucket(ctx, &awss3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	return err
}

// Object is one row returned by ListObjects.
type Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	IsFolder     bool      `json:"is_folder"`
}

// ListObjects lists objects under prefix using delimiter "/" so the UI
// renders a familiar folder hierarchy. Returns the next continuation token.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix, continuation string) ([]Object, string, error) {
	in := &awss3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int32(1000),
	}
	if continuation != "" {
		in.ContinuationToken = aws.String(continuation)
	}
	out, err := c.raw.ListObjectsV2(ctx, in)
	if err != nil {
		return nil, "", err
	}
	objs := make([]Object, 0, len(out.Contents)+len(out.CommonPrefixes))
	for _, cp := range out.CommonPrefixes {
		if cp.Prefix == nil {
			continue
		}
		objs = append(objs, Object{Key: *cp.Prefix, IsFolder: true})
	}
	for _, o := range out.Contents {
		if o.Key == nil {
			continue
		}
		// Skip the marker for the current prefix itself.
		if *o.Key == prefix {
			continue
		}
		ob := Object{Key: *o.Key}
		if o.Size != nil {
			ob.Size = *o.Size
		}
		if o.LastModified != nil {
			ob.LastModified = *o.LastModified
		}
		objs = append(objs, ob)
	}
	next := ""
	if out.IsTruncated != nil && *out.IsTruncated && out.NextContinuationToken != nil {
		next = *out.NextContinuationToken
	}
	return objs, next, nil
}

// PutObject uploads a single object via the multipart uploader (works for
// any size; small files get one part).
func (c *Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, contentType string) error {
	in := &awss3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	_, err := c.uploader.Upload(ctx, in)
	return err
}

// GetObject opens a streaming reader for download.
func (c *Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	out, err := c.raw.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

// DeleteObjects batch-deletes up to 1000 keys per call.
func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	objs := make([]s3types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		k := k
		objs = append(objs, s3types.ObjectIdentifier{Key: aws.String(k)})
	}
	out, err := c.raw.DeleteObjects(ctx, &awss3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3types.Delete{Objects: objs, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return 0, err
	}
	if len(out.Errors) > 0 {
		return len(keys) - len(out.Errors), fmt.Errorf("partial delete: %d/%d failed", len(out.Errors), len(keys))
	}
	return len(keys), nil
}

// ─── Backup uploader (agent-side) ───────────────────────────────────────────

// UploaderConfig is what the agent passes to NewUploader — comes from
// RunBackupReq.
type UploaderConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

// Uploader is the agent's narrow interface for streaming a single tar
// straight to S3 via multipart upload.
type Uploader struct {
	c      *Client
	bucket string
}

// NewUploader builds an S3 uploader pointed at one bucket.
func NewUploader(cfg UploaderConfig) (*Uploader, error) {
	c, err := NewClient(context.Background(), EndpointConfig{
		Endpoint:  cfg.Endpoint,
		Region:    cfg.Region,
		PathStyle: cfg.PathStyle,
		Creds:     Credentials{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey},
	})
	if err != nil {
		return nil, err
	}
	return &Uploader{c: c, bucket: cfg.Bucket}, nil
}

// Upload streams body to s3://bucket/key using multipart upload. Returns the
// total bytes successfully uploaded. The body MUST be the producer side of
// the pipe; if upload fails mid-way the producer should see the read end
// close and stop writing.
func (u *Uploader) Upload(ctx context.Context, key string, body io.Reader) (uint64, error) {
	in := &awss3.PutObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	out, err := u.c.uploader.Upload(ctx, in)
	if err != nil {
		return 0, err
	}
	// The SDK doesn't expose total bytes directly; tag from response doesn't
	// help either. The caller (agent backup runner) tracks its own count and
	// is the source of truth for "bytes_processed".
	_ = out
	return 0, nil
}
