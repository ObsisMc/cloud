package nodeservices

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A project's services are started once and then serve for as long as the deployment runs, so
// every path Node and the Agent take through them must keep passing TLS verification long after
// the certificates were issued. Each path is checked on its own: they fail independently.
func TestServicesKeepWorkingLongAfterStart(t *testing.T) {
	for name, uptime := range map[string]time.Duration{"25 hours": 25 * time.Hour, "3 years": 3 * 365 * 24 * time.Hour} {
		t.Run(name, func(t *testing.T) {
			s := testServiceIssued(t, map[string]Binding{"binding": {Model: testModel, Protocol: OpenAICompletions, Provider: FixtureProvider{}}}, time.Now().Add(-uptime))
			t.Run("a new session clones", func(t *testing.T) {
				// Node clones with the system git, which has its own TLS stack and clock check.
				checkout := filepath.Join(t.TempDir(), "checkout")
				gitCommand(t, t.TempDir(), "-c", "http.sslCAInfo="+s.TLS.CACert, "-c", "http.sslCert="+s.TLS.ClientCert, "-c", "http.sslKey="+s.TLS.ClientKey, "clone", s.URL+"/repo.git", checkout)
				if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
					t.Fatalf("clone has no README.md: %v", err)
				}
			})
			t.Run("Node issues and renews a model grant", func(t *testing.T) {
				node := serviceClient(t, s, true)
				_, id := issueToken(t, s, node, "session")
				status, data := request(t, node, "POST", s.URL+"/internal/v1/model-grants/"+id+"/renew", "", `{"executionId":"session"}`)
				if status != 200 {
					t.Fatalf("renew = %d %s", status, data)
				}
			})
			t.Run("the Agent calls the model", func(t *testing.T) {
				// The grant is created in process so that this path does not depend on Node's.
				if err := s.Permit("agent", "binding"); err != nil {
					t.Fatal(err)
				}
				issued := httptest.NewRecorder()
				s.issue(issued, httptest.NewRequest("POST", "/internal/v1/model-grants", strings.NewReader(`{"bindingId":"binding","executionId":"agent"}`)))
				var grant struct{ Token string }
				if err := json.Unmarshal(issued.Body.Bytes(), &grant); err != nil || grant.Token == "" {
					t.Fatalf("grant = %d %s %v", issued.Code, issued.Body, err)
				}
				status, data := request(t, serviceClient(t, s, false), "POST", s.URL+"/runtime/openai/v1/chat/completions", grant.Token, `{"messages":[{"role":"user","content":"Hi"}]}`)
				if status != 200 {
					t.Fatalf("completion = %d %s", status, data)
				}
			})
		})
	}
}
