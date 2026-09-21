package devlogin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wanglongan587/cloud/internal/gateway"
)

const origin = "http://localhost:5173"

func newProvider(t *testing.T, now func() time.Time) (*Authenticator, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	a, e := New(origin, now)
	if e != nil {
		t.Fatal(e)
	}
	r := gin.New()
	a.Routes(r)
	return a, r
}

// submit posts the identity form the way a browser would and returns the callback redirect.
func submit(t *testing.T, r http.Handler, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, AuthorizePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestNewRequiresLoopbackHTTPOrigin(t *testing.T) {
	for _, bad := range []string{"https://app.example.com", "http://app.example.com", "http://localhost:5173/app", ""} {
		if _, e := New(bad, time.Now); e == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if _, e := New(origin, nil); e == nil {
		t.Error("nil clock must be refused")
	}
}

func TestAuthorizationURLBindsStateChallengeAndOwnCallback(t *testing.T) {
	a, _ := newProvider(t, time.Now)
	callback := origin + gateway.CallbackPath + "/" + Name
	got, e := a.AuthorizationURL(gateway.AuthorizationRequest{State: "st", CodeChallenge: "ch", CallbackURL: callback})
	if e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(got)
	if e != nil || u.Scheme+"://"+u.Host != origin || u.Path != AuthorizePath || u.Query().Get("state") != "st" || u.Query().Get("code_challenge") != "ch" {
		t.Fatalf("authorize URL = %q", got)
	}
	if _, e = a.AuthorizationURL(gateway.AuthorizationRequest{State: "st", CodeChallenge: "ch", CallbackURL: "http://localhost:5173/auth/callback/github"}); e == nil {
		t.Fatal("another provider's callback must be refused")
	}
	if _, e = a.AuthorizationURL(gateway.AuthorizationRequest{CallbackURL: callback}); e == nil {
		t.Fatal("missing state and challenge must be refused")
	}
}

func TestFormRendersAndSubmitRedirectsWithVerifiableCode(t *testing.T) {
	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a, r := newProvider(t, func() time.Time { return clock })
	verifier := "verifier-known-only-to-the-gateway"
	challenge := gateway.CodeChallenge(verifier)
	callback := origin + gateway.CallbackPath + "/" + Name

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, AuthorizePath+"?state=st&code_challenge="+challenge, http.NoBody))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="subject"`) || !strings.Contains(w.Body.String(), `value="`+challenge+`"`) {
		t.Fatalf("form must render with the challenge bound: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the form carries login state and must not be cached")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, AuthorizePath, http.NoBody))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("form without state must be refused, got %d", w.Code)
	}

	form := url.Values{"state": {"st"}, "code_challenge": {challenge}, "source": {" acme-corp "}, "subject": {"u-42"}, "display_name": {"Ada <b>Lovelace</b>"}}
	w = submit(t, r, form, map[string]string{"Origin": origin})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("submit must redirect, got %d %s", w.Code, w.Body.String())
	}
	target, e := url.Parse(w.Header().Get("Location"))
	if e != nil || target.Scheme+"://"+target.Host+target.Path != callback || target.Query().Get("state") != "st" {
		t.Fatalf("redirect must go to the dev callback with state, got %q", w.Header().Get("Location"))
	}
	code := target.Query().Get("code")

	identity, e := a.Exchange(context.Background(), code, verifier, callback)
	if e != nil {
		t.Fatal(e)
	}
	want := gateway.VerifiedIdentity{Source: "acme-corp", Subject: "u-42", DisplayName: "Ada <b>Lovelace</b>"}
	if identity != want {
		t.Fatalf("identity = %+v, want %+v", identity, want)
	}

	rejections := map[string]func() (gateway.VerifiedIdentity, error){
		"wrong verifier": func() (gateway.VerifiedIdentity, error) {
			return a.Exchange(context.Background(), code, "other", callback)
		},
		"wrong callback": func() (gateway.VerifiedIdentity, error) {
			return a.Exchange(context.Background(), code, verifier, callback+"x")
		},
		"tampered body": func() (gateway.VerifiedIdentity, error) {
			return a.Exchange(context.Background(), "e30."+strings.SplitN(code, ".", 2)[1], verifier, callback)
		},
		"malformed code": func() (gateway.VerifiedIdentity, error) {
			return a.Exchange(context.Background(), "not-a-code", verifier, callback)
		},
		"foreign process": func() (gateway.VerifiedIdentity, error) {
			other, _ := New(origin, time.Now)
			return other.Exchange(context.Background(), code, verifier, callback)
		},
	}
	for name, attempt := range rejections {
		if _, e := attempt(); !errors.Is(e, gateway.ErrProviderRejected) {
			t.Errorf("%s: want ErrProviderRejected, got %v", name, e)
		}
	}
	clock = clock.Add(codeLifetime + time.Second)
	if _, e := a.Exchange(context.Background(), code, verifier, callback); !errors.Is(e, gateway.ErrProviderRejected) {
		t.Fatalf("expired code must be rejected, got %v", e)
	}
}

func TestSubmitRefusesCrossSiteAndInvalidIdentities(t *testing.T) {
	_, r := newProvider(t, time.Now)
	form := url.Values{"state": {"st"}, "code_challenge": {"ch"}, "source": {"dev"}, "subject": {"me"}}
	if w := submit(t, r, form, map[string]string{"Origin": "http://evil.example"}); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site submit must be refused, got %d", w.Code)
	}
	if w := submit(t, r, form, nil); w.Code != http.StatusForbidden {
		t.Fatalf("submit without origin proof must be refused, got %d", w.Code)
	}
	empty := url.Values{"state": {"st"}, "code_challenge": {"ch"}, "source": {"dev"}, "subject": {"   "}}
	if w := submit(t, r, empty, map[string]string{"Origin": origin}); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "required") {
		t.Fatalf("blank subject must re-render the form with an error, got %d", w.Code)
	}
	noState := url.Values{"source": {"dev"}, "subject": {"me"}}
	if w := submit(t, r, noState, map[string]string{"Origin": origin}); w.Code != http.StatusBadRequest {
		t.Fatalf("submit without state must be refused, got %d", w.Code)
	}
}
