package objectstore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Verify checks existence, size and S3's stored SHA-256 using Cloud's private endpoint.
// A false verdict is definitive (missing object or mismatched metadata); an error is transient
// and must not acknowledge the Node event. No URL or infrastructure detail escapes this boundary.
func Verify(ctx context.Context, cfg *Config, key string, size int64, digest string) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("object store is not configured")
	}
	private := *cfg
	private.PublicEndpoint = ""
	grant, err := presign(&private, key, "HEAD", map[string]string{"x-amz-checksum-mode": "ENABLED"}, time.Now())
	if err != nil {
		return false, fmt.Errorf("object store configuration invalid")
	}
	req, err := http.NewRequestWithContext(ctx, grant.Method, grant.URL, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("object verification request invalid")
	}
	for name, value := range grant.Headers {
		req.Header.Set(name, value)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("object verification unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("object verification unavailable")
	}
	actualSize, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return false, nil
	}
	checksum, err := base64.StdEncoding.DecodeString(resp.Header.Get("X-Amz-Checksum-Sha256"))
	return err == nil && actualSize == size && hex.EncodeToString(checksum) == digest, nil
}
