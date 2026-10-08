package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/objectstore"
)

// A capability obtained before settlement must not invalidate the objects Cloud already verified.
func TestRevisionStoredObjectsRejectLiveGrantOverwrite(t *testing.T) {
	cfg := revisionStorage(t)
	f := setup(t)
	_, terminal := f.registeredDelivery(t, cfg)
	history := terminal.Result.GetRevisionUnchanged().History
	client := controlpb.NewAgentRunServiceClient(f.controlConn)
	ctx := asController(f.client.Subject)
	original := []byte("{\"kind\":\"assistant\",\"text\":\"echo\"}\n")
	replacement := []byte("replacement that must never overwrite a verified Revision\n")
	sum := sha256.Sum256(replacement)
	grants := make([]*controlpb.UploadGrant, 0, 2)
	for _, digest := range []string{history.Sha256, hex.EncodeToString(sum[:])} {
		response, err := client.GrantRevisionUpload(ctx, &controlpb.GrantRevisionUploadRequest{Epoch: f.controller.Epoch, ExecutionId: terminal.ExecutionId, Checksums: map[string]string{history.Key: digest}})
		must(t, err)
		grants = append(grants, response.Grants[0])
	}
	legacy, err := client.GrantRevisionUpload(ctx, &controlpb.GrantRevisionUploadRequest{Epoch: f.controller.Epoch, ExecutionId: terminal.ExecutionId})
	must(t, err)
	var legacyGrant *controlpb.UploadGrant
	for _, grant := range legacy.GetGrants() {
		if grant.GetObjectKey() == history.Key {
			legacyGrant = grant
		}
	}
	if legacyGrant == nil {
		t.Fatal("legacy grant omitted the fixed history object")
	}
	put := func(grant *controlpb.UploadGrant, data []byte, omitCondition bool) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), grant.Method, grant.Url, bytes.NewReader(data))
		must(t, err)
		for name, value := range grant.Headers {
			req.Header.Set(name, value)
		}
		if omitCondition {
			req.Header.Del("If-None-Match")
		}
		response, err := http.DefaultClient.Do(req)
		must(t, err)
		response.Body.Close()
		return response.StatusCode
	}
	if status := put(grants[0], original, false); status != http.StatusOK {
		t.Fatal("initial object creation failed", status)
	}
	_, err = f.executions.TakeOverNodeEvent(ctx, terminal)
	must(t, err)
	if f.scalar("SELECT count(*) FROM revisions") != 1 {
		t.Fatal("original object was not verified and registered")
	}
	if status := put(grants[1], replacement, false); status != http.StatusPreconditionFailed {
		t.Fatal("a still-live grant overwrote a verified object", status)
	}
	if status := put(grants[0], original, false); status != http.StatusPreconditionFailed {
		t.Fatal("repeated PUT did not preserve the first object", status)
	}
	if status := put(legacyGrant, replacement, false); status != http.StatusPreconditionFailed {
		t.Fatal("a still-live legacy grant overwrote a verified object", status)
	}
	if status := put(grants[1], replacement, true); status >= 200 && status < 300 {
		t.Fatal("removing the signed condition allowed an overwrite")
	}
	valid, err := objectstore.Verify(t.Context(), cfg, history.Key, int64(history.Size), history.Sha256)
	must(t, err)
	if !valid {
		t.Fatal("live capabilities changed the registered object's metadata")
	}
}
