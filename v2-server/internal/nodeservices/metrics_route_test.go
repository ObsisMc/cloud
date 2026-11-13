package nodeservices_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
	"github.com/wanglongan587/cloud/v2-server/internal/requestmetrics"
)

type attempts struct {
	mu       sync.Mutex
	finished []requestmetrics.Attempt
}

func (a *attempts) BeginModelRequest(_ context.Context, _ execution.Ref, at requestmetrics.Attempt) (requestmetrics.Attempt, error) {
	at.ID = "attempt"
	return at, nil
}

func (a *attempts) FinishModelRequest(_ context.Context, at requestmetrics.Attempt) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finished = append(a.finished, at)
	return nil
}

// The production wiring wraps every Agent call in request metrics. A stream cut off there must
// still reach the Agent as a failed response, and be recorded as interrupted.
func TestCutOffThroughRequestMetricsAbortsAndRecords(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	remote, err := nodeservices.NewRemoteProvider(upstream.URL, "backend-key", upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.SetLimits(300*time.Millisecond, time.Minute); err != nil {
		t.Fatal(err)
	}
	recorded := &attempts{}
	root := t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", args...).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	bare := filepath.Join(root, "repo.git")
	git("init", "-q", "--bare", bare)
	pkg := filepath.Join(root, "agent.orax")
	if err := os.WriteFile(pkg, []byte("package bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("package bytes"))
	model := nodeservices.Model{ID: "model", Name: "Model", ContextWindow: 1000, MaxTokens: 100}
	s, err := nodeservices.Start(nodeservices.Options{
		StateDirectory: root, Repository: bare, GitBackend: filepath.Join(git("--exec-path"), "git-http-backend"),
		Packages: []nodeservices.Package{{Name: "agent", Path: pkg, SHA256: hex.EncodeToString(sum[:])}},
		Bindings: map[string]nodeservices.Binding{"binding": {Model: model, Protocol: nodeservices.OpenAICompletions, Provider: remote}},
		ProviderFor: func(_ string, b nodeservices.Binding) nodeservices.Provider {
			return requestmetrics.Wrap(recorded, execution.Ref{SessionID: "session", TurnID: "turn"}, b)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ca, _ := os.ReadFile(s.TLS.CACert)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	pair, err := tls.LoadX509KeyPair(s.TLS.ClientCert, s.TLS.ClientKey)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}}}
	if err := s.Permit("execution", "binding"); err != nil {
		t.Fatal(err)
	}
	resp, err := c.Post(s.URL+"/internal/v1/model-grants", "application/json", strings.NewReader(`{"bindingId":"binding","executionId":"execution"}`))
	if err != nil {
		t.Fatal(err)
	}
	var grant struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&grant)
	resp.Body.Close()
	req, _ := http.NewRequest("POST", s.URL+"/runtime/openai/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || err == nil || errors.Is(err, io.EOF) || !strings.Contains(string(data), `"content":"x"`) {
		t.Fatalf("want a broken 200 stream, got %d err=%v body=%q", resp.StatusCode, err, data)
	}
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	if len(recorded.finished) != 1 || recorded.finished[0].Outcome != "interrupted" || recorded.finished[0].ErrorCode != "model_response_interrupted" {
		t.Fatalf("recorded=%+v", recorded.finished)
	}
}
