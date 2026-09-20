package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeReturnToRejectsEveryOffOriginForm(t *testing.T) {
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"empty defaults to root", "", "/", true},
		{"plain path", "/projects/1?tab=ops#x", "/projects/1?tab=ops#x", true},
		{"root", "/", "/", true},
		{"protocol relative", "//evil.example/x", "", false},
		{"backslash disguised host", "/\\evil.example", "", false},
		{"absolute https", "https://evil.example/", "", false},
		{"scheme without slashes", "javascript:alert(1)", "", false},
		{"relative without slash", "projects", "", false},
		{"embedded backslash", "/a\\b", "", false},
		{"crlf", "/a\r\nSet-Cookie: x", "", false},
		{"too long", "/" + strings.Repeat("a", MaxReturnToLength), "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizeReturnTo(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: NormalizeReturnTo(%q) = (%q,%v) want (%q,%v)", tc.name, tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSameOriginRequiresExactOriginOrSameOriginFetchMetadata(t *testing.T) {
	const origin = "https://app.example.com"
	cases := []struct {
		name          string
		origin, fetch string
		want          bool
	}{
		{"matching origin", origin, "", true},
		{"matching origin with cross-site metadata still trusts origin", origin, "cross-site", true},
		{"different host", "https://evil.example.com", "same-origin", false},
		{"different scheme", "http://app.example.com", "", false},
		{"subdomain", "https://sub.app.example.com", "", false},
		{"null origin", "null", "", false},
		{"missing origin with same-origin metadata", "", "same-origin", true},
		{"missing origin with same-site metadata", "", "same-site", false},
		{"missing both", "", "", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.fetch != "" {
			r.Header.Set("Sec-Fetch-Site", tc.fetch)
		}
		if got := SameOrigin(r, origin); got != tc.want {
			t.Errorf("%s: SameOrigin = %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestCookiePolicyEnforcesPrefixesScopeAndDatabaseBoundedMaxAge(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	prod := CookiePolicy{Secure: true, CallbackPath: CallbackPath}
	session := prod.SessionCookie("tok", now.Add(48*time.Hour), now)
	if session.Name != "__Host-ora_session" || !session.Secure || !session.HttpOnly || session.Path != "/" || session.Domain != "" || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("production session cookie violates the __Host- contract: %+v", session)
	}
	if session.MaxAge != 48*60*60 {
		t.Fatalf("session Max-Age must equal the database lifetime: got %d", session.MaxAge)
	}
	if expired := prod.SessionCookie("tok", now.Add(-time.Hour), now); expired.MaxAge != 1 {
		t.Fatalf("a cookie for an already expired session must not be negative/persistent: %d", expired.MaxAge)
	}
	attempt := prod.AttemptCookie("secret", 10*time.Minute)
	if attempt.Name != "__Secure-ora_login" || attempt.Path != CallbackPath || attempt.SameSite != http.SameSiteLaxMode || attempt.MaxAge != 600 || !attempt.Secure || !attempt.HttpOnly {
		t.Fatalf("attempt cookie must be Lax, callback-scoped and short-lived: %+v", attempt)
	}
	if clear := prod.ClearAttemptCookie(); clear.MaxAge != -1 || clear.Path != CallbackPath || clear.Name != attempt.Name {
		t.Fatalf("clearing must target the same cookie: %+v", clear)
	}
	if clear := prod.ClearSessionCookie(); clear.MaxAge != -1 || clear.Path != "/" || clear.Name != session.Name {
		t.Fatalf("clearing must target the same session cookie: %+v", clear)
	}
	dev := CookiePolicy{Secure: false, CallbackPath: CallbackPath}
	if c := dev.SessionCookie("tok", now.Add(time.Hour), now); c.Name != "ora_session" || c.Secure {
		t.Fatalf("development downgrade must drop the prefix and Secure only: %+v", c)
	}
	if c := dev.AttemptCookie("s", time.Minute); c.Name != "ora_login" || c.Secure {
		t.Fatalf("development attempt cookie: %+v", c)
	}
}

func TestMutatingMethods(t *testing.T) {
	for method, want := range map[string]bool{"GET": false, "HEAD": false, "OPTIONS": false, "POST": true, "PUT": true, "PATCH": true, "DELETE": true} {
		if got := mutating(method); got != want {
			t.Errorf("mutating(%s) = %v want %v", method, got, want)
		}
	}
}
