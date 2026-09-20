package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/internal/gateway"
)

// fakeGitHub mimics the two provider endpoints the adapter uses and records what it received.
type fakeGitHub struct {
	t           *testing.T
	tokenForm   url.Values
	userAuth    string
	tokenStatus int
	tokenBody   string
	userStatus  int
	userBody    string
}

func (f *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Accept") != "application/json" {
			f.t.Errorf("token exchange must be a JSON-accepting POST: %s %s", r.Method, r.Header.Get("Accept"))
		}
		if e := r.ParseForm(); e != nil {
			f.t.Error(e)
		}
		f.tokenForm = r.PostForm
		w.WriteHeader(f.tokenStatus)
		_, _ = w.Write([]byte(f.tokenBody))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		f.userAuth = r.Header.Get("Authorization")
		w.WriteHeader(f.userStatus)
		_, _ = w.Write([]byte(f.userBody))
	})
	return mux
}

func newAdapter(t *testing.T, f *fakeGitHub) *Authenticator {
	t.Helper()
	server := httptest.NewServer(f.handler())
	t.Cleanup(server.Close)
	a, e := New(&Options{ClientID: "client", ClientSecret: "secret", AuthorizeURL: server.URL + "/login/oauth/authorize", TokenURL: server.URL + "/login/oauth/access_token", UserURL: server.URL + "/user", HTTP: NewHTTPClient(2 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestAuthorizationURLBindsStateChallengeAndCallbackWithoutScopes(t *testing.T) {
	a := newAdapter(t, &fakeGitHub{t: t})
	raw, e := a.AuthorizationURL(gateway.AuthorizationRequest{State: "st", CodeChallenge: "ch", CallbackURL: "https://app.example.com/auth/callback/github"})
	if e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	if q.Get("client_id") != "client" || q.Get("state") != "st" || q.Get("code_challenge") != "ch" || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "https://app.example.com/auth/callback/github" {
		t.Fatalf("authorization parameters incomplete: %s", raw)
	}
	if q.Has("scope") || strings.Contains(raw, "secret") {
		t.Fatalf("no scope may be requested and the secret must never appear: %s", raw)
	}
	if _, e = a.AuthorizationURL(gateway.AuthorizationRequest{State: "st"}); e == nil {
		t.Fatal("missing challenge or callback must be rejected")
	}
}

func TestExchangeUsesVerifierReadsNumericIDAndDiscardsToken(t *testing.T) {
	f := &fakeGitHub{t: t, tokenStatus: 200, tokenBody: `{"access_token":"gho_secret","token_type":"bearer"}`, userStatus: 200, userBody: `{"id":71996633,"login":"obsismc","name":"  Ray Zhang  ","email":"hidden@example.com"}`}
	a := newAdapter(t, f)
	identity, e := a.Exchange(context.Background(), "code-1", "verifier-1", "https://app.example.com/auth/callback/github")
	if e != nil {
		t.Fatal(e)
	}
	want := gateway.VerifiedIdentity{Source: "github.com", Subject: "71996633", DisplayName: "Ray Zhang"}
	if identity != want {
		t.Fatalf("identity = %+v want %+v", identity, want)
	}
	if f.tokenForm.Get("code") != "code-1" || f.tokenForm.Get("code_verifier") != "verifier-1" || f.tokenForm.Get("client_secret") != "secret" || f.tokenForm.Get("redirect_uri") != "https://app.example.com/auth/callback/github" {
		t.Fatalf("token exchange form incomplete: %v", f.tokenForm)
	}
	if f.userAuth != "Bearer gho_secret" {
		t.Fatalf("user lookup must use the exchanged token: %q", f.userAuth)
	}
}

func TestExchangeFallsBackToLoginAndRejectsBadProviderAnswers(t *testing.T) {
	okToken := `{"access_token":"tok","token_type":"bearer"}`
	cases := []struct {
		name                    string
		tokenStatus, userStatus int
		tokenBody, userBody     string
		want                    gateway.VerifiedIdentity
		rejected                bool
	}{
		{"login used when name empty", 200, 200, okToken, `{"id":"7","login":"seven","name":""}`, gateway.VerifiedIdentity{Source: "github.com", Subject: "7", DisplayName: "seven"}, false},
		{"github error object with 200", 200, 200, `{"error":"bad_verification_code"}`, `{}`, gateway.VerifiedIdentity{}, true},
		{"token endpoint 401", 401, 200, `{}`, `{}`, gateway.VerifiedIdentity{}, true},
		{"non bearer token type", 200, 200, `{"access_token":"tok","token_type":"mac"}`, `{}`, gateway.VerifiedIdentity{}, true},
		{"user endpoint 403", 200, 403, okToken, `{"message":"forbidden"}`, gateway.VerifiedIdentity{}, true},
		{"missing id", 200, 200, okToken, `{"login":"x"}`, gateway.VerifiedIdentity{}, true},
		{"non numeric id", 200, 200, okToken, `{"id":"abc","login":"x"}`, gateway.VerifiedIdentity{}, true},
		{"zero id", 200, 200, okToken, `{"id":0,"login":"x"}`, gateway.VerifiedIdentity{}, true},
		{"malformed user json", 200, 200, okToken, `not json`, gateway.VerifiedIdentity{}, true},
		{"oversized user document", 200, 200, okToken, `{"id":1,"login":"` + strings.Repeat("x", maxResponseBytes) + `"}`, gateway.VerifiedIdentity{}, true},
	}
	for _, tc := range cases {
		a := newAdapter(t, &fakeGitHub{t: t, tokenStatus: tc.tokenStatus, tokenBody: tc.tokenBody, userStatus: tc.userStatus, userBody: tc.userBody})
		got, e := a.Exchange(context.Background(), "code", "verifier", "https://app.example.com/auth/callback/github")
		if tc.rejected {
			if !errors.Is(e, gateway.ErrProviderRejected) {
				t.Errorf("%s: want ErrProviderRejected, got %v (%+v)", tc.name, e, got)
			}
			continue
		}
		if e != nil || got != tc.want {
			t.Errorf("%s: got (%+v,%v) want %+v", tc.name, got, e, tc.want)
		}
	}
}

func TestExchangeReportsInfrastructureFailuresDistinctly(t *testing.T) {
	release := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(blocked.Close)
	t.Cleanup(func() { close(release) })
	a, e := New(&Options{ClientID: "c", ClientSecret: "s", TokenURL: blocked.URL + "/t", UserURL: blocked.URL + "/u", HTTP: NewHTTPClient(200 * time.Millisecond)})
	if e != nil {
		t.Fatal(e)
	}
	_, e = a.Exchange(context.Background(), "code", "verifier", "https://app.example.com/cb")
	if e == nil || errors.Is(e, gateway.ErrProviderRejected) {
		t.Fatalf("a timeout is an infrastructure failure, not a provider rejection: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = a.Exchange(ctx, "code", "verifier", "https://app.example.com/cb"); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation must propagate: %v", e)
	}
}

func TestNewRejectsIncompleteOptions(t *testing.T) {
	cases := map[string]Options{
		"missing client id":      {ClientSecret: "s", HTTP: NewHTTPClient(time.Second)},
		"missing secret":         {ClientID: "c", HTTP: NewHTTPClient(time.Second)},
		"client without timeout": {ClientID: "c", ClientSecret: "s", HTTP: &http.Client{}},
		"invalid endpoint":       {ClientID: "c", ClientSecret: "s", HTTP: NewHTTPClient(time.Second), TokenURL: "ftp://x"},
	}
	for name, o := range cases {
		if _, e := New(&o); e == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	a, e := New(&Options{ClientID: "c", ClientSecret: "s", HTTP: NewHTTPClient(time.Second)})
	if e != nil || a.Source() != DefaultSource {
		t.Fatalf("defaults must apply: %v %q", e, a.Source())
	}
}
