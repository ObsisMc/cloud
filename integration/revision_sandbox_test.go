package integration

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/wanglongan587/cloud/internal/controlpb"
)

// The curl process stands in for a sandbox uploader, without Cloud's private network or keys.
func TestS3PublicGrantIsUsableFromSandboxNetwork(t *testing.T) {
	cfg := revisionStorage(t)
	network, endpoint := os.Getenv("TEST_S3_SANDBOX_NETWORK"), os.Getenv("TEST_S3_PUBLIC_ENDPOINT")
	if network == "" || endpoint == "" {
		if os.Getenv("REQUIRE_S3_SANDBOX") == "1" {
			t.Fatal("sandbox-network acceptance requires TEST_S3_SANDBOX_NETWORK and TEST_S3_PUBLIC_ENDPOINT")
		}
		t.Skip("sandbox-network acceptance: set TEST_S3_SANDBOX_NETWORK and TEST_S3_PUBLIC_ENDPOINT")
	}
	cfg.PublicEndpoint = endpoint
	f := setup(t)
	_, req := f.registeredDelivery(t, cfg)
	history := req.Result.GetRevisionUnchanged().History
	response, err := controlpb.NewAgentRunServiceClient(f.controlConn).GrantRevisionUpload(asController(f.client.Subject), &controlpb.GrantRevisionUploadRequest{Epoch: f.controller.Epoch, ExecutionId: req.ExecutionId, Checksums: map[string]string{history.Key: history.Sha256}})
	must(t, err)
	if len(response.Grants) != 1 || response.Grants[0].ObjectKey != history.Key {
		t.Fatal("checksum request did not return precisely the frozen history capability")
	}
	grant := response.Grants[0]
	lines := []string{"url = " + strconv.Quote(grant.Url), "request = " + strconv.Quote(grant.Method), "data-binary = " + strconv.Quote("{\"kind\":\"assistant\",\"text\":\"echo\"}\n")}
	for name, value := range grant.Headers {
		lines = append(lines, "header = "+strconv.Quote(name+": "+value))
	}
	// The bearer URL travels over stdin only: no command argument, environment, file or log.
	cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "-i", "--network", network, "--entrypoint", "/usr/bin/curl", "rustfs/rustfs:latest", "--silent", "--output", "/dev/null", "--write-out", "%{http_code}", "--config", "-") // #nosec G204 -- explicit opt-in isolated sandbox-network acceptance.
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	status, err := cmd.Output()
	must(t, err)
	if string(status) != "200" {
		t.Fatal("sandbox could not upload using Cloud's public capability", string(status))
	}
	_, err = f.executions.TakeOverNodeEvent(asController(f.client.Subject), req)
	must(t, err)
	if f.scalar("SELECT count(*) FROM revisions") != 1 || storedGrantCount(t, f) != 0 {
		t.Fatal("private verification failed or persisted a public capability")
	}
}
