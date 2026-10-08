package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests exercise the client against an in-process S3 double and against the structure of the
// requests it produces. They deliberately do NOT claim that a real S3-compatible store accepts the
// signatures: no MinIO or AWS endpoint was reachable when this package was written, and a double that
// answers whatever it is asked cannot prove an authentication scheme. What is asserted here is what a
// store's own response could not tell us anyway — that a grant names exactly the requested key, method
// and lifetime, that the signature is bound to that key, and that the verification refuses every way a
// stored object can disagree with the declaration.

const (
	testAccessKeyID     = "ora-test-access-key"
	testSecretAccessKey = "ora-test-secret-key"
	testBucket          = "ora-revisions"
	testKey             = "revisions/tenant-1/run-1/attempt-1/revision.bundle"
)

// testInstant is the fixed clock every signing test uses, so a presigned URL and a HEAD signature are
// asserted as values rather than against "roughly now".
var testInstant = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// newTestClient builds a client from real credential files — the only credential input the
// constructor accepts — with a stopped clock and the given endpoint.
func newTestClient(t *testing.T, endpoint string, options ...func(*Config)) *Client {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		Endpoint:            endpoint,
		Region:              "us-east-1",
		Bucket:              testBucket,
		PathStyle:           true,
		AccessKeyIDFile:     writeCredential(t, dir, "access-key-id", testAccessKeyID),
		SecretAccessKeyFile: writeCredential(t, dir, "secret-access-key", testSecretAccessKey),
		Now:                 func() time.Time { return testInstant },
	}
	for _, option := range options {
		option(&cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// writeCredential writes one credential file and returns its path. The trailing newline is
// intentional: it is what a secret mount or an editor leaves behind, and reading it as part of the
// secret would produce a signature no store accepts.
func writeCredential(t *testing.T, dir, name, value string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatalf("write credential %s: %v", name, err)
	}
	return path
}

// objectServer answers every request with `status`, a Content-Length of len(body), the given checksum
// header (omitted when empty) and `body` itself, and records the last request behind the returned
// accessor (the handler runs on the server's goroutine, so the recorded request is read through a
// closure rather than a value captured when the server was built). A real S3 reports the stored
// object's length on a HEAD, so the double declares it explicitly rather than relying on the server's
// body-length inference, which a HEAD response does not perform.
func objectServer(status int, checksum string, body []byte) (*httptest.Server, func() *http.Request) {
	var seen *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Clone(context.Background())
		if checksum != "" {
			w.Header().Set(checksumHeader, checksum)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	return server, func() *http.Request { return seen }
}

// TestUploadGrantNamesOneKeyOneMethodAndTheConfiguredLifetime pins what a grant IS: a capability for
// exactly one object key and exactly one method, bounded in time (Cloud Revision D2/D3, invariant 2).
// The properties are read off the URL itself because the URL is the whole capability — anything it
// does not name, the Node cannot do.
func TestUploadGrantNamesOneKeyOneMethodAndTheConfiguredLifetime(t *testing.T) {
	client := newTestClient(t, "http://store.invalid:9000")
	grant, err := client.PresignPut(testKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if grant.ObjectKey != testKey {
		t.Errorf("grant object key = %q, want %q", grant.ObjectKey, testKey)
	}
	if grant.Method != http.MethodPut {
		t.Errorf("grant method = %q, want PUT", grant.Method)
	}
	if want := testInstant.Add(15 * time.Minute); !grant.ExpiresAt.Equal(want) {
		t.Errorf("grant expires at %s, want %s", grant.ExpiresAt, want)
	}
	if len(grant.Headers) != 0 {
		t.Errorf("grant headers = %v, want none: the one header the upload must carry is the digest the Node computes after the URL is signed", grant.Headers)
	}
	parsed, err := url.Parse(grant.URL)
	if err != nil {
		t.Fatalf("parse grant URL: %v", err)
	}
	if want := "/" + testBucket + "/" + testKey; parsed.Path != want {
		t.Errorf("grant path = %q, want %q", parsed.Path, want)
	}
	query := parsed.Query()
	for name, want := range map[string]string{
		"X-Amz-Algorithm":     algorithm,
		"X-Amz-Expires":       "900",
		"X-Amz-SignedHeaders": signedHost,
		"X-Amz-Date":          "20261008T120000Z",
		"X-Amz-Credential":    testAccessKeyID + "/20261008/us-east-1/s3/aws4_request",
	} {
		if got := query.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	signature := query.Get("X-Amz-Signature")
	if len(signature) != 64 || strings.Trim(signature, "0123456789abcdef") != "" {
		t.Errorf("X-Amz-Signature = %q, want 64 lowercase hex digits", signature)
	}
	if strings.Contains(grant.URL, testSecretAccessKey) {
		t.Error("the grant URL contains the secret access key")
	}
}

// TestUploadGrantIsBoundToTheKeyAndDeterministic proves the signature covers the object key: a URL
// signed for one attempt's object cannot be replayed against another key, which is what keeps a
// failed attempt's capability from overwriting a registered Revision's objects (D2). Signing the same
// key at the same instant must reproduce the same URL, or the signature would not be a function of
// what it claims to cover.
func TestUploadGrantIsBoundToTheKeyAndDeterministic(t *testing.T) {
	client := newTestClient(t, "http://store.invalid:9000")
	first, err := client.PresignPut(testKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	other, err := client.PresignPut(strings.Replace(testKey, "attempt-1", "attempt-2", 1), 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if first.URL == other.URL {
		t.Fatal("two different object keys produced the same presigned URL")
	}
	repeat, err := client.PresignPut(testKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if repeat.URL != first.URL {
		t.Errorf("the same key at the same instant produced a different URL:\n%s\n%s", first.URL, repeat.URL)
	}
}

// TestUploadGrantLifetimeIsBoundedAndPositive checks both edges of the lifetime: a non-positive TTL is
// a caller error rather than a grant that expires as it is issued, and a TTL beyond S3's own ceiling
// is clamped rather than signed — a signature the store would refuse after accepting it surfaces as an
// unexplained 403 at the Node.
func TestUploadGrantLifetimeIsBoundedAndPositive(t *testing.T) {
	client := newTestClient(t, "http://store.invalid:9000")
	if _, err := client.PresignPut(testKey, 0); err == nil {
		t.Error("PresignPut with a zero TTL succeeded; want an error")
	}
	if _, err := client.PresignPut(testKey, -time.Minute); err == nil {
		t.Error("PresignPut with a negative TTL succeeded; want an error")
	}
	grant, err := client.PresignPut(testKey, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	parsed, err := url.Parse(grant.URL)
	if err != nil {
		t.Fatalf("parse grant URL: %v", err)
	}
	if got := parsed.Query().Get("X-Amz-Expires"); got != "604800" {
		t.Errorf("X-Amz-Expires = %q, want 604800 (S3's seven-day ceiling)", got)
	}
	if want := testInstant.Add(PresignExpiryLimit); !grant.ExpiresAt.Equal(want) {
		t.Errorf("clamped grant expires at %s, want %s", grant.ExpiresAt, want)
	}
}

// TestUploadGrantRefusesKeysOutsideOneBucket is the injection boundary: a key that climbs out of the
// bucket, needs escaping, or carries a control character is a bug or an attack, never a Revision
// object, and is refused before it can be signed.
func TestUploadGrantRefusesKeysOutsideOneBucket(t *testing.T) {
	client := newTestClient(t, "http://store.invalid:9000")
	for _, key := range []string{
		"",
		"/revisions/run-1/revision.bundle",
		"revisions/run-1/revision.bundle/",
		"revisions//run-1/revision.bundle",
		"revisions/../secrets/revision.bundle",
		"revisions/./run-1/revision.bundle",
		"revisions/run-1/revision bundle",
		"revisions/run-1/revision\nbundle",
		"revisions/run-1/revision?bundle",
		"revisions/run-1/revision#bundle",
		"revisions/run-1/revision\"bundle",
		strings.Repeat("a", 1025),
	} {
		if _, err := client.PresignPut(key, 15*time.Minute); err == nil {
			t.Errorf("PresignPut(%q) succeeded; want a refusal", key)
		}
	}
}

// TestPublicEndpointIsUsedOnlyForGrants keeps the two addresses apart: the Node must be handed a URL
// for the address it can reach, while Cloud's own verification must talk to the address Cloud can
// reach (D1's `public_endpoint`).
func TestPublicEndpointIsUsedOnlyForGrants(t *testing.T) {
	digest := sha256.Sum256(nil)
	server, seen := objectServer(http.StatusOK, base64.StdEncoding.EncodeToString(digest[:]), nil)
	defer server.Close()
	client := newTestClient(t, server.URL, func(cfg *Config) { cfg.PublicEndpoint = "http://node-visible.invalid:9000" })
	grant, err := client.PresignPut(testKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	parsed, err := url.Parse(grant.URL)
	if err != nil {
		t.Fatalf("parse grant URL: %v", err)
	}
	if parsed.Host != "node-visible.invalid:9000" {
		t.Errorf("grant host = %q, want the public endpoint", parsed.Host)
	}
	if err := client.Verify(context.Background(), testKey, 0, hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := strings.TrimPrefix(server.URL, "http://"); seen().Host != want {
		t.Errorf("HEAD went to host %q, want the configured endpoint %q", seen().Host, want)
	}
}

// TestVerifyAcceptsOnlyTheDeclaredSizeAndDigest is D4 step 2: the object must exist, and both the size
// and the SHA-256 must equal what the Node declared. Every way of disagreeing — an absent object, a
// wrong size, a wrong digest, a store that never received a checksum — is a failure, and none of them
// is distinguished, because for the delivery they all mean the same thing.
func TestVerifyAcceptsOnlyTheDeclaredSizeAndDigest(t *testing.T) {
	content := []byte("revision bundle bytes")
	digest := sha256.Sum256(content)
	hexDigest := hex.EncodeToString(digest[:])
	base64Digest := base64.StdEncoding.EncodeToString(digest[:])
	size := int64(len(content))

	cases := []struct {
		name     string
		status   int
		checksum string
		body     []byte
		size     int64
		sha256   string
		ok       bool
	}{
		{name: "matching size and digest", status: http.StatusOK, checksum: base64Digest, body: content, size: size, sha256: hexDigest, ok: true},
		// The store's base64 spelling is compared after decoding, so a declaration in a different case
		// is the same digest and must not fail a delivery that is correct.
		{name: "uppercase declared digest", status: http.StatusOK, checksum: base64Digest, body: content, size: size, sha256: strings.ToUpper(hexDigest), ok: true},
		{name: "absent object", status: http.StatusNotFound, checksum: base64Digest, body: content, size: size, sha256: hexDigest},
		{name: "wrong size", status: http.StatusOK, checksum: base64Digest, body: content, size: size + 1, sha256: hexDigest},
		{name: "wrong digest", status: http.StatusOK, checksum: base64Digest, body: content, size: size, sha256: strings.Repeat("0", 64)},
		{name: "store holds no checksum", status: http.StatusOK, body: content, size: size, sha256: hexDigest},
		{name: "store returns a checksum that is not a digest", status: http.StatusOK, checksum: "not base64!", body: content, size: size, sha256: hexDigest},
		{name: "store returns the wrong length of checksum", status: http.StatusOK, checksum: base64.StdEncoding.EncodeToString([]byte("short")), body: content, size: size, sha256: hexDigest},
		{name: "declared digest is not a digest", status: http.StatusOK, checksum: base64Digest, body: content, size: size, sha256: "not-a-digest"},
		{name: "declared digest is absent", status: http.StatusOK, checksum: base64Digest, body: content, size: size},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := objectServer(tc.status, tc.checksum, tc.body)
			defer server.Close()
			err := newTestClient(t, server.URL).Verify(context.Background(), testKey, tc.size, tc.sha256)
			if tc.ok && err != nil {
				t.Fatalf("Verify: %v; want success", tc.ok)
			}
			if !tc.ok && err == nil {
				t.Fatal("Verify succeeded; want a failure")
			}
		})
	}
}

// TestVerifyRefusesANegativeDeclaredSize: the declaration arrives from the wire, so a size no object
// can have is rejected before a request is made rather than compared against a store's answer.
func TestVerifyRefusesANegativeDeclaredSize(t *testing.T) {
	client := newTestClient(t, "http://store.invalid:9000")
	if err := client.Verify(context.Background(), testKey, -1, strings.Repeat("0", 64)); err == nil {
		t.Error("Verify with a negative declared size succeeded; want a failure")
	}
}

// TestVerifyAsksTheStoreForTheChecksum checks the one header that makes the verification meaningful:
// without `x-amz-checksum-mode: ENABLED` the store returns no digest, and "the object exists" would
// be mistaken for "the object is what the Node said it is".
func TestVerifyAsksTheStoreForTheChecksum(t *testing.T) {
	content := []byte("bytes")
	digest := sha256.Sum256(content)
	server, seen := objectServer(http.StatusOK, base64.StdEncoding.EncodeToString(digest[:]), content)
	defer server.Close()
	client := newTestClient(t, server.URL)
	if err := client.Verify(context.Background(), testKey, int64(len(content)), hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	request := seen()
	if got := request.Header.Get(checksumModeHeader); got != "ENABLED" {
		t.Errorf("%s = %q, want ENABLED", checksumModeHeader, got)
	}
	if got := request.Header.Get("x-amz-date"); got != "20261008T120000Z" {
		t.Errorf("x-amz-date = %q, want 20261008T120000Z", got)
	}
	if got := request.Header.Get("x-amz-content-sha256"); got != emptyBodySHA256 {
		t.Errorf("x-amz-content-sha256 = %q, want the empty-body digest", got)
	}
	authorization := request.Header.Get("Authorization")
	for _, want := range []string{
		algorithm,
		"Credential=" + testAccessKeyID + "/20261008/us-east-1/s3/aws4_request",
		"SignedHeaders=host;x-amz-checksum-mode;x-amz-content-sha256;x-amz-date",
		"Signature=",
	} {
		if !strings.Contains(authorization, want) {
			t.Errorf("Authorization %q does not contain %q", authorization, want)
		}
	}
	if strings.Contains(authorization, testSecretAccessKey) {
		t.Error("the Authorization header contains the secret access key")
	}
}

// TestVerifyReportsAnUnreachableStore: an endpoint that never answers is a failed verification, not a
// startup failure and not a silent success (D1).
func TestVerifyReportsAnUnreachableStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	client := newTestClient(t, endpoint, func(cfg *Config) { cfg.HTTPClient = &http.Client{Timeout: time.Second} })
	if err := client.Verify(context.Background(), testKey, 5, strings.Repeat("0", 64)); err == nil {
		t.Fatal("Verify against a closed endpoint succeeded; want a failure")
	}
}

// TestNewRejectsMalformedConfiguration is the startup contract: a configured section that cannot
// produce a client must fail before the process serves anything, while an endpoint that is merely
// unreachable must NOT (D1). Only the first is testable here, and it is the one that would otherwise
// fail silently on every delivery.
func TestNewRejectsMalformedConfiguration(t *testing.T) {
	dir := t.TempDir()
	keyFile := writeCredential(t, dir, "access-key-id", testAccessKeyID)
	secretFile := writeCredential(t, dir, "secret-access-key", testSecretAccessKey)
	emptyFile := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyFile, nil, 0o600); err != nil {
		t.Fatalf("write empty credential: %v", err)
	}
	oversizedFile := filepath.Join(dir, "oversized")
	if err := os.WriteFile(oversizedFile, make([]byte, credentialFileLimit+1), 0o600); err != nil {
		t.Fatalf("write oversized credential: %v", err)
	}
	base := func() Config {
		return Config{
			Endpoint: "http://store.invalid:9000", Region: "us-east-1", Bucket: testBucket,
			AccessKeyIDFile: keyFile, SecretAccessKeyFile: secretFile,
		}
	}
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "endpoint absent", mutate: func(c *Config) { c.Endpoint = "" }},
		{name: "endpoint is not a URL", mutate: func(c *Config) { c.Endpoint = "store.invalid:9000" }},
		{name: "endpoint carries a query", mutate: func(c *Config) { c.Endpoint = "http://store.invalid:9000/?x=1" }},
		{name: "endpoint carries userinfo", mutate: func(c *Config) { c.Endpoint = "http://user@store.invalid:9000" }},
		{name: "endpoint is not http", mutate: func(c *Config) { c.Endpoint = "ftp://store.invalid" }},
		{name: "public endpoint is malformed", mutate: func(c *Config) { c.PublicEndpoint = "ftp://store.invalid" }},
		{name: "region absent", mutate: func(c *Config) { c.Region = " " }},
		{name: "bucket absent", mutate: func(c *Config) { c.Bucket = "" }},
		{name: "bucket is not DNS-compatible", mutate: func(c *Config) { c.Bucket = "Ora_Revisions" }},
		{name: "bucket is too short", mutate: func(c *Config) { c.Bucket = "or" }},
		{name: "access key file absent", mutate: func(c *Config) { c.AccessKeyIDFile = "" }},
		{name: "secret file missing", mutate: func(c *Config) { c.SecretAccessKeyFile = filepath.Join(dir, "nope") }},
		{name: "secret file empty", mutate: func(c *Config) { c.SecretAccessKeyFile = emptyFile }},
		{name: "credential file oversized", mutate: func(c *Config) { c.SecretAccessKeyFile = oversizedFile }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("New succeeded; want a configuration error")
			}
		})
	}
}

// TestReadCredentialTrimsOnlyWhitespace: the value is what the file says, minus the surrounding
// whitespace an editor or a secret mount adds — nothing else is normalized, because a credential is
// compared byte for byte by the store.
func TestReadCredentialTrimsOnlyWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("  secret with spaces inside  \n"), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	value, err := readCredentialValue("object_store.secret_access_key_file", path)
	if err != nil {
		t.Fatalf("readCredentialValue: %v", err)
	}
	if value != "secret with spaces inside" {
		t.Errorf("credential = %q, want the trimmed file content", value)
	}
}
