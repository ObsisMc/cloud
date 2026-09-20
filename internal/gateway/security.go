package gateway

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxReturnToLength bounds the stored redirect path; it matches the gateway_login_attempts check.
const MaxReturnToLength = 2048

// NormalizeReturnTo accepts only a same-origin relative path. It rejects schemes, hosts,
// protocol-relative (`//host`) and backslash-disguised (`/\host`) forms before anything is stored;
// the callback later redirects only to the stored value, never to a request parameter.
func NormalizeReturnTo(raw string) (string, bool) {
	if raw == "" {
		return "/", true
	}
	if len(raw) > MaxReturnToLength || raw[0] != '/' {
		return "", false
	}
	if len(raw) > 1 && (raw[1] == '/' || raw[1] == '\\') {
		return "", false
	}
	if strings.ContainsAny(raw, "\\\r\n\x00") {
		return "", false
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "", false
	}
	return raw, true
}

// SameOrigin reports whether a state-changing browser request proves it came from the public
// origin. A present Origin must match exactly; when Origin is absent, Sec-Fetch-Site: same-origin
// is accepted as equivalent evidence. Anything else is rejected.
func SameOrigin(r *http.Request, publicOrigin string) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == publicOrigin
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// mutating reports whether the request method can change state and therefore needs origin proof.
func mutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// CookiePolicy fixes the browser-side attributes for the two Gateway cookies. In production the
// session cookie carries the __Host- prefix so the browser itself enforces Secure, Path=/ and the
// absence of Domain; the attempt cookie is scoped to the callback path so the attempt secret never
// travels with ordinary requests. Development on loopback HTTP is the only permitted downgrade.
type CookiePolicy struct {
	Secure       bool
	CallbackPath string
}

// SessionCookieName is the name browsers hold the session token under.
func (p CookiePolicy) SessionCookieName() string {
	if p.Secure {
		return "__Host-ora_session"
	}
	return "ora_session"
}

// AttemptCookieName is the name browsers hold the login attempt secret under.
func (p CookiePolicy) AttemptCookieName() string {
	if p.Secure {
		return "__Secure-ora_login"
	}
	return "ora_login"
}

// SessionCookie builds the persistent session cookie. Max-Age never exceeds the database expiry.
func (p CookiePolicy) SessionCookie(token string, expiresAt, now time.Time) *http.Cookie {
	maxAge := int(expiresAt.Sub(now) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	return &http.Cookie{Name: p.SessionCookieName(), Value: token, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: p.Secure, SameSite: http.SameSiteLaxMode}
}

// ClearSessionCookie expires the session cookie in the browser.
func (p CookiePolicy) ClearSessionCookie() *http.Cookie {
	return &http.Cookie{Name: p.SessionCookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: p.Secure, SameSite: http.SameSiteLaxMode}
}

// AttemptCookie binds the browser to one login attempt. SameSite must be Lax, not Strict: the
// provider callback is a cross-site top-level navigation and a Strict cookie would never arrive.
func (p CookiePolicy) AttemptCookie(secret string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{Name: p.AttemptCookieName(), Value: secret, Path: p.CallbackPath, MaxAge: int(ttl / time.Second), HttpOnly: true, Secure: p.Secure, SameSite: http.SameSiteLaxMode}
}

// ClearAttemptCookie removes the attempt secret after the callback completes either way.
func (p CookiePolicy) ClearAttemptCookie() *http.Cookie {
	return &http.Cookie{Name: p.AttemptCookieName(), Value: "", Path: p.CallbackPath, MaxAge: -1, HttpOnly: true, Secure: p.Secure, SameSite: http.SameSiteLaxMode}
}
