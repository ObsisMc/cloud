package objectstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AWS Signature Version 4 for the two operations Cloud needs: presigning one PUT (query-string
// authentication, so the Node holds a URL rather than a credential) and signing one HEAD
// (header authentication, because Cloud itself holds the credentials).
//
// The canonical-request construction below follows the S3 profile of the specification:
//
//   - the path is URI-encoded once per segment with the RFC 3986 unreserved set, and `/` is kept as
//     the segment separator; S3 is the one service that must not double-encode it;
//   - query parameters are sorted by name and encoded with the same unreserved set, so a space is
//     `%20` and never `+` (the form-encoding Go's url.Values produces would sign a different string
//     than the one sent);
//   - headers are signed by lowercase name, each value trimmed, each line terminated by a newline;
//   - the payload is `UNSIGNED-PAYLOAD` for the presigned PUT, because the uploader's bytes are not
//     known when the URL is signed, and the empty-body digest for the HEAD, whose body is empty.

const (
	algorithm = "AWS4-HMAC-SHA256"
	service   = "s3"

	// emptyBodySHA256 is SHA-256 of the empty string, the payload hash of every request this package
	// signs that carries no body.
	emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// unsignedPayload tells S3 not to check the body against a digest; the presigned PUT's content is
	// unknown until the Node uploads it, and the checksum the store does validate is the one the
	// uploader declares.
	unsignedPayload = "UNSIGNED-PAYLOAD"

	// signedHost is the only header a presigned URL covers. Signing more would require Cloud to know
	// the Node's own computed checksum when the URL is issued, which it cannot (D2).
	signedHost = "host"

	// checksumModeHeader asks S3 to return the object's stored checksum on a HEAD, and
	// checksumHeader is the response header that carries it (base64, per the S3 API).
	checksumModeHeader = "x-amz-checksum-mode"
	checksumHeader     = "x-amz-checksum-sha256"

	// amzDateLayout and shortDateLayout are the two spellings of the signing instant: the request
	// timestamp and the credential scope's date.
	amzDateLayout   = "20060102T150405Z"
	shortDateLayout = "20060102"

	// unreserved is RFC 3986's unreserved set plus the segment separator, which SigV4 keeps.
	unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
)

// credentials is the deployment's S3 credential pair, held in one place so it cannot be printed by
// accident: the struct has no String method, and nothing in this package formats it.
type credentials struct {
	accessKeyID     string
	secretAccessKey string
}

// presignedRequest is the signed result of signPresignedPut: the absolute URL a Node PUTs to, and
// the headers the upload must carry unchanged.
type presignedRequest struct {
	url     string
	headers map[string]string
}

// signPresignedPut builds the query-string-authenticated URL for one PUT of one object key.
func (c *Client) signPresignedPut(objectKey string, ttl time.Duration, now time.Time) presignedRequest {
	amzDate, date := stamp(now)
	scope := credentialScope(date, c.region)
	path := resourcePath(c.public.Path, c.bucket, objectKey, c.pathStyle)

	query := [][2]string{
		{"X-Amz-Algorithm", algorithm},
		{"X-Amz-Credential", c.credentials.accessKeyID + "/" + scope},
		{"X-Amz-Date", amzDate},
		{"X-Amz-Expires", strconv.FormatInt(int64(ttl/time.Second), 10)},
		{"X-Amz-SignedHeaders", signedHost},
	}
	canonicalQuery := canonicalQuery(query)
	canonicalRequest := request(http.MethodPut, path, canonicalQuery, "host:"+c.public.Host+"\n", signedHost, unsignedPayload)
	signature := c.signature(date, scope, amzDate, canonicalRequest)

	return presignedRequest{
		url: c.public.Scheme + "://" + c.public.Host + path + "?" + canonicalQuery + "&X-Amz-Signature=" + signature,
		// No header is fixed for the uploader: the one header this contract requires it to send,
		// `x-amz-checksum-sha256`, is the digest it computes itself and Cloud cannot know it when the
		// URL is signed. The store validates that digest, and Verify confirms the stored object
		// independently (D4 step 2), so the requirement is enforced twice over without the grant
		// naming a value it would have to guess.
		headers: map[string]string{},
	}
}

// headRequest builds the header-authenticated HEAD Cloud uses to verify an uploaded object. The
// checksum mode is what makes the response carry the stored digest, so a store that never received a
// checksum cannot pass this verification by staying silent.
func (c *Client) headRequest(ctx context.Context, objectKey string) (*http.Request, error) {
	now := c.now().UTC()
	amzDate, date := stamp(now)
	scope := credentialScope(date, c.region)
	path := resourcePath(c.endpoint.Path, c.bucket, objectKey, c.pathStyle)

	signed := map[string]string{
		"host":                 c.endpoint.Host,
		checksumModeHeader:     "ENABLED",
		"x-amz-content-sha256": emptyBodySHA256,
		"x-amz-date":           amzDate,
	}
	names := make([]string, 0, len(signed))
	for name := range signed {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + strings.TrimSpace(signed[name]) + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonicalRequest := request(http.MethodHead, path, "", canonicalHeaders.String(), signedHeaders, emptyBodySHA256)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.endpoint.Scheme+"://"+c.endpoint.Host+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("object store: build HEAD request: %w", err)
	}
	for name, value := range signed {
		req.Header.Set(name, value)
	}
	// The Host header is set from the URL, and the signature covers exactly that value.
	req.Host = c.endpoint.Host
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, c.credentials.accessKeyID, scope, signedHeaders, c.signature(date, scope, amzDate, canonicalRequest)))
	return req, nil
}

// request assembles the canonical request the specification defines. Every part is passed in fully
// formed — the caller owns the escaping — so this function cannot re-encode a component differently
// from the request it is signing.
func request(method, path, query, canonicalHeaders, signedHeaders, payloadHash string) string {
	return strings.Join([]string{method, path, query, canonicalHeaders, signedHeaders, payloadHash}, "\n")
}

// signature derives the request signature from the signing key of the credential's date and region.
// The date is the caller's already-formatted signing instant, never a second read of the clock: the
// credential scope, the timestamp header and the signing key must all name the same moment.
func (c *Client) signature(date, scope, amzDate, canonicalRequest string) string {
	key := signingKey(c.credentials.secretAccessKey, date, c.region)
	stringToSign := strings.Join([]string{algorithm, amzDate, scope, hexSHA256(canonicalRequest)}, "\n")
	return hex.EncodeToString(hmacSHA256(key, stringToSign))
}

// signingKey is the specification's key derivation chain: the secret prefixed with `AWS4`, then one
// HMAC per scope component.
func signingKey(secret, date, region string) []byte {
	key := hmacSHA256([]byte("AWS4"+secret), date)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	return hmacSHA256(key, "aws4_request")
}

func credentialScope(date, region string) string {
	return strings.Join([]string{date, region, service, "aws4_request"}, "/")
}

func stamp(now time.Time) (amzDate, date string) {
	return now.Format(amzDateLayout), now.Format(shortDateLayout)
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

func hexSHA256(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

// normalizeSHA256 accepts a declared digest in either hex case and returns it in the lowercase form
// the ADR compares in; anything that is not a 64-digit hex digest is refused rather than compared.
func normalizeSHA256(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != 64 {
		return "", fmt.Errorf("declared sha256 %q is not a 64-digit hex digest", value)
	}
	for i := 0; i < len(normalized); i++ {
		c := normalized[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("declared sha256 %q is not a 64-digit hex digest", value)
		}
	}
	return normalized, nil
}

// decodeChecksum converts the base64 checksum S3 returns into the same lowercase hex form, so the
// two spellings of the same digest are compared as digests rather than as strings.
func decodeChecksum(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("stored object carries no %s checksum", checksumHeader)
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw) != sha256.Size {
		return "", fmt.Errorf("stored %s %q is not a base64 SHA-256 digest", checksumHeader, value)
	}
	return hex.EncodeToString(raw), nil
}

// resourcePath renders the request path: the endpoint's own prefix, then the bucket when addressing
// is path-style, then the object key's segments, each encoded with the unreserved set alone.
func resourcePath(prefix, bucket, objectKey string, pathStyle bool) string {
	segments := make([]string, 0, strings.Count(objectKey, "/")+2)
	if pathStyle {
		segments = append(segments, bucket)
	}
	segments = append(segments, strings.Split(objectKey, "/")...)
	return prefix + "/" + strings.Join(escapeSegments(segments), "/")
}

func escapeSegments(segments []string) []string {
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, escapeSegment(segment))
	}
	return escaped
}

// canonicalQuery renders the query parameters the way the signature covers them: sorted by name,
// each name and value encoded with the unreserved set alone.
func canonicalQuery(params [][2]string) string {
	sorted := make([][2]string, len(params))
	copy(sorted, params)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	parts := make([]string, 0, len(sorted))
	for _, param := range sorted {
		parts = append(parts, escapeSegment(param[0])+"="+escapeSegment(param[1]))
	}
	return strings.Join(parts, "&")
}

// escapeSegment percent-encodes everything outside RFC 3986's unreserved set, byte by byte, so the
// signed string and the transmitted one are the same string.
func escapeSegment(segment string) string {
	var b strings.Builder
	b.Grow(len(segment))
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		const hexDigits = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}
