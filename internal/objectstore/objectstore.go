// Package objectstore is Cloud's S3-compatible object-store client for Revision objects. It signs
// the single-key, single-method upload grants a Controller hands to a Node (Cloud Revision D2/D3) and
// verifies an uploaded object by HEAD before Cloud registers a Revision (D4 step 2).
//
// Credentials are the deployment's own (D1): they arrive as infrastructure references, are held only
// in this process, and never reach a Controller, a Node, the database or a log. What a Node receives
// is a time-limited capability for one object key — not a credential (invariant 1).
//
// Signing is AWS Signature Version 4, implemented here rather than pulled in as a dependency: the
// delivery path needs exactly two operations (presign one PUT, sign one HEAD) and the repository
// prefers the standard library over a new module for that.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wanglongan587/cloud/internal/core"
)

// PresignExpiryLimit is S3's own ceiling on a presigned URL's lifetime (seven days). A configured
// TTL above it is clamped rather than sent, because the store would refuse the request after
// accepting the signature and the failure would surface as an unexplained 403 at the Node.
const PresignExpiryLimit = 7 * 24 * time.Hour

// requestTimeout bounds one HEAD verification. It is deliberately short: the verification runs
// outside the takeover transaction and a Controller is waiting for the answer, so a store that does
// not answer promptly is a store this delivery should fail and retry (D1) rather than wait on.
const requestTimeout = 10 * time.Second

// Config is the deployment's object-store configuration (Cloud Revision D1). Endpoint, Region,
// Bucket, PathStyle and the two credential values come from the `object_store` section; nothing here
// is per-run.
type Config struct {
	// Endpoint is the S3-compatible endpoint Cloud itself talks to. Its scheme and host are part of
	// the SigV4 signature, so an endpoint reached through a rewriting proxy must be configured as the
	// proxy's own address.
	Endpoint string

	// PublicEndpoint, when set, is the endpoint presigned URLs name instead of Endpoint: a deployment
	// whose Nodes resolve a different address for the store (a different DNS name, a public front
	// door) signs the URL for the address the Node will actually connect to, which is the address the
	// store itself must see.
	PublicEndpoint string

	// Region is the signing region. It is not discovered: S3-compatible stores accept any region
	// string, but the signature must name the same one the client and the store agree on.
	Region string

	// Bucket is the bucket Revision objects live in. Cloud never creates it (D1).
	Bucket string

	// PathStyle selects path-style addressing (`<endpoint>/<bucket>/<key>`) over virtual-host style
	// (`<bucket>.<endpoint>/<key>`), which MinIO and most local deployments need.
	PathStyle bool

	// AccessKeyIDFile and SecretAccessKeyFile are PATHS to the two credential files (D1: credentials
	// are infrastructure references). New reads them, and the values then live only inside this
	// client: they are never persisted, logged, or returned in a grant, and they have no spelling in
	// configuration that could be committed by accident.
	AccessKeyIDFile     string
	SecretAccessKeyFile string

	// HTTPClient and Now are optional seams: the default HTTP client bounds every request with
	// requestTimeout, and the default clock is the wall clock. Tests inject both.
	HTTPClient *http.Client
	Now        func() time.Time
}

// Client signs upload grants and verifies uploaded objects against one configured bucket. It is
// immutable after construction and safe for concurrent use.
type Client struct {
	endpoint    *url.URL
	public      *url.URL
	region      string
	bucket      string
	pathStyle   bool
	credentials credentials
	http        *http.Client
	now         func() time.Time
}

// New validates one object-store configuration and returns a client for it. A malformed endpoint or
// a missing credential value is a configuration error the process must not start with; an endpoint
// that is merely unreachable is not (D1), which is why nothing here talks to the store.
//
// The configuration is taken by value on purpose: it is a description of a store, not shared state,
// so a client can never be changed by a later mutation of the caller's struct.
//
//nolint:gocritic // hugeParam: the copy is the point, see above.
func New(cfg Config) (*Client, error) {
	endpoint, err := parseEndpoint("object_store.endpoint", cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	public := endpoint
	if cfg.PublicEndpoint != "" {
		if public, err = parseEndpoint("object_store.public_endpoint", cfg.PublicEndpoint); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, errors.New("object_store.region is required")
	}
	if !validBucket(cfg.Bucket) {
		return nil, fmt.Errorf("object_store.bucket %q is not a valid bucket name", cfg.Bucket)
	}
	credentials, err := readCredentials(cfg.AccessKeyIDFile, cfg.SecretAccessKeyFile)
	if err != nil {
		return nil, err
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		endpoint: endpoint, public: public, region: cfg.Region, bucket: cfg.Bucket, pathStyle: cfg.PathStyle,
		credentials: credentials, http: httpClient, now: now,
	}, nil
}

// credentialFileLimit bounds a credential file. A real access key and secret are tens of bytes; a
// file this size means the path names something else, and reading it would pull an unrelated file
// into the process.
const credentialFileLimit = 4096

// readCredentials reads the deployment's S3 credential pair from the two referenced files. Each
// value is trimmed of the trailing newline an editor or a secret mount leaves behind, and an empty
// or oversized file is a startup error: a client that cannot sign must not be constructed, because
// every delivery would then fail for a reason no failure code names.
func readCredentials(accessKeyIDFile, secretAccessKeyFile string) (credentials, error) {
	accessKeyID, err := readCredentialValue("object_store.access_key_id_file", accessKeyIDFile)
	if err != nil {
		return credentials{}, err
	}
	secretAccessKey, err := readCredentialValue("object_store.secret_access_key_file", secretAccessKeyFile)
	if err != nil {
		return credentials{}, err
	}
	return credentials{accessKeyID: accessKeyID, secretAccessKey: secretAccessKey}, nil
}

func readCredentialValue(field, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s is required when object_store is configured", field)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	if info.Size() == 0 || info.Size() > credentialFileLimit {
		return "", fmt.Errorf("%s: %s must be a non-empty file of at most %d bytes", field, path, credentialFileLimit)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is deployment configuration, not request input.
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", fmt.Errorf("%s: %s contains no credential value", field, path)
	}
	return value, nil
}

// PresignPut returns the upload grant for exactly one object key: a PUT to that key, valid until
// ExpiresAt, carrying no credential and no other capability (Cloud Revision D2/D3). Cloud signs the
// `host` header alone, so the grant authorizes the key, the method and the lifetime and nothing
// else; the digest the object must have is the uploader's own duty, enforced by the store's checksum
// validation and, independently, by Verify.
func (c *Client) PresignPut(objectKey string, ttl time.Duration) (core.UploadGrant, error) {
	if err := validateObjectKey(objectKey); err != nil {
		return core.UploadGrant{}, err
	}
	if ttl <= 0 {
		return core.UploadGrant{}, errors.New("object store: upload grant ttl must be positive")
	}
	if ttl > PresignExpiryLimit {
		ttl = PresignExpiryLimit
	}
	now := c.now().UTC()
	signed := c.signPresignedPut(objectKey, ttl, now)
	return core.UploadGrant{
		ObjectKey: objectKey,
		URL:       signed.url,
		Method:    http.MethodPut,
		Headers:   signed.headers,
		ExpiresAt: now.Add(ttl),
	}, nil
}

// Verify confirms that the object at objectKey exists with exactly the declared size and SHA-256
// (Cloud Revision D4 step 2). `sha256` is lowercase hex; the store answers with its own base64
// checksum, which is compared after decoding, so a store that was never handed a checksum — and so
// cannot vouch for the content — fails the verification instead of passing it.
//
// Every failure is returned as an error and none is distinguished by the caller: the ADR treats a
// missing object, a differing digest and an unreachable endpoint alike, because all three mean the
// same thing for the delivery (D1: the delivery fails and IssueRun D5 retries it).
func (c *Client) Verify(ctx context.Context, objectKey string, size int64, sha256 string) error {
	if err := validateObjectKey(objectKey); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("object %s: declared size %d is negative", objectKey, size)
	}
	want, err := normalizeSHA256(sha256)
	if err != nil {
		return fmt.Errorf("object %s: %w", objectKey, err)
	}
	request, err := c.headRequest(ctx, objectKey)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("object %s: HEAD %s: %w", objectKey, c.endpoint.Redacted(), err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("object %s: HEAD returned %s", objectKey, response.Status)
	}
	if response.ContentLength != size {
		return fmt.Errorf("object %s: declared size %d, stored size %d", objectKey, size, response.ContentLength)
	}
	stored, err := decodeChecksum(response.Header.Get(checksumHeader))
	if err != nil {
		return fmt.Errorf("object %s: %w", objectKey, err)
	}
	if stored != want {
		return fmt.Errorf("object %s: declared sha256 %s, stored sha256 %s", objectKey, want, stored)
	}
	return nil
}

// parseEndpoint validates one configured endpoint URL. Only http and https are accepted, and the URL
// must carry nothing but a scheme, a host and an optional path prefix: a query or fragment would be
// dropped from the signed request and silently change what the signature covers.
func parseEndpoint(name, raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a URL: %w", name, raw, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%s %q must be an http(s) URL with a host", name, raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, fmt.Errorf("%s %q must not carry a query, fragment or userinfo", name, raw)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed, nil
}

// validateObjectKey rejects anything that is not a plain object key under the bucket. Keys are
// assembled by Cloud from a run identity and an attempt id, so a key that needs escaping, climbs out
// of the bucket, or carries a control character is a bug or an injection attempt at this boundary —
// never a legitimate Revision object.
func validateObjectKey(key string) error {
	if key == "" || len(key) > 1024 {
		return fmt.Errorf("object store: object key %q must be 1..1024 bytes", key)
	}
	if strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") || strings.Contains(key, "//") {
		return fmt.Errorf("object store: object key %q must be a relative path of non-empty segments", key)
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("object store: object key %q must not contain %q segments", key, segment)
		}
	}
	for _, r := range key {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`\?#"<>|*`, r) {
			return fmt.Errorf("object store: object key %q must not contain %q", key, r)
		}
	}
	return nil
}

// validBucket applies the DNS-compatible bucket naming S3 itself enforces, so a typo is a startup
// error rather than a signature that can never address anything.
func validBucket(bucket string) bool {
	if len(bucket) < 3 || len(bucket) > 63 {
		return false
	}
	for i, r := range bucket {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' || r == '.':
			if i == 0 || i == len(bucket)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
