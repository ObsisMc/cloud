// Package devlogin is the development-only identity provider: an Authenticator that plays the
// external provider itself. The Gateway sends the browser to a small form served from this
// package, the developer types the identity they want to be, and the form hands a signed code
// back to the ordinary /auth/callback/dev route. Attempts, state, PKCE, cookies, sessions, and
// credential issuance are the real ones; only the "who are you" step is faked.
//
// It is registered solely when login.development_provider is set, which the configuration
// accepts only together with public.development (loopback HTTP). Nothing here may reach a
// deployment with a public origin.
package devlogin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wanglongan587/cloud/internal/gateway"
)

// Route names. The authorize page lives under /auth so the Vite proxy and the same-origin policy
// treat it like every other Gateway route.
const (
	// Name is the provider identifier browsers pass to POST /auth/login.
	Name = gateway.ProviderDevelopment
	// AuthorizePath serves the identity form (GET) and turns it into a code (POST).
	AuthorizePath = "/auth/dev/authorize"
	// DefaultSource is the identity namespace the form proposes; it never collides with
	// "github.com", so developer identities and real ones stay distinct in Cloud.
	DefaultSource = "dev"
	// codeLifetime bounds how long a code may sit between the form and the callback.
	codeLifetime = 5 * time.Minute
)

// Authenticator implements gateway.Authenticator and serves the authorize form. Codes are
// self-contained: an HMAC over the chosen identity, the PKCE challenge, and an expiry, keyed by a
// random per-process secret so nothing outside this process can mint one.
type Authenticator struct {
	publicOrigin string
	callbackURL  string
	key          []byte
	now          func() time.Time
}

// New creates the provider for one public origin. The origin must be the loopback development
// origin the Gateway validated; the callback is derived from it exactly as Login derives it.
func New(publicOrigin string, now func() time.Time) (*Authenticator, error) {
	origin, e := gateway.PublicOrigin(publicOrigin, true)
	if e != nil {
		return nil, e
	}
	if !strings.HasPrefix(origin, "http://") {
		return nil, errors.New("the development provider is limited to loopback http origins")
	}
	if now == nil {
		return nil, errors.New("clock is required")
	}
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return nil, e
	}
	return &Authenticator{publicOrigin: origin, callbackURL: origin + gateway.CallbackPath + "/" + Name, key: key, now: now}, nil
}

// AuthorizationURL points the browser at the identity form, carrying state and the PKCE challenge
// the way a real provider's authorize endpoint would.
func (a *Authenticator) AuthorizationURL(request gateway.AuthorizationRequest) (string, error) {
	if request.State == "" || request.CodeChallenge == "" {
		return "", errors.New("state and code challenge are required")
	}
	if request.CallbackURL != a.callbackURL {
		return "", fmt.Errorf("callback %q does not belong to this provider", request.CallbackURL)
	}
	q := url.Values{"state": {request.State}, "code_challenge": {request.CodeChallenge}}
	return a.publicOrigin + AuthorizePath + "?" + q.Encode(), nil
}

// Exchange verifies the code's signature, expiry, and PKCE binding and returns the identity the
// developer chose. Every failure is a provider rejection; the orchestration maps it to login_failed.
func (a *Authenticator) Exchange(_ context.Context, code, codeVerifier, callbackURL string) (gateway.VerifiedIdentity, error) {
	if callbackURL != a.callbackURL {
		return gateway.VerifiedIdentity{}, fmt.Errorf("%w: wrong callback", gateway.ErrProviderRejected)
	}
	payload, e := a.open(code)
	if e != nil {
		return gateway.VerifiedIdentity{}, fmt.Errorf("%w: %w", gateway.ErrProviderRejected, e)
	}
	if !a.now().Before(time.Unix(payload.Expires, 0)) {
		return gateway.VerifiedIdentity{}, fmt.Errorf("%w: code expired", gateway.ErrProviderRejected)
	}
	if subtle.ConstantTimeCompare([]byte(payload.Challenge), []byte(gateway.CodeChallenge(codeVerifier))) != 1 {
		return gateway.VerifiedIdentity{}, fmt.Errorf("%w: PKCE verifier mismatch", gateway.ErrProviderRejected)
	}
	return gateway.VerifiedIdentity{Source: payload.Source, Subject: payload.Subject, DisplayName: payload.DisplayName}, nil
}

// Routes registers the authorize form on the Gateway engine.
func (a *Authenticator) Routes(r gin.IRouter) {
	r.GET(AuthorizePath, a.form)
	r.POST(AuthorizePath, a.submit)
}

// codePayload is what a code signs. The challenge binds the code to the attempt whose verifier
// the Gateway alone can derive, mirroring what a real provider enforces.
type codePayload struct {
	Source      string `json:"source"`
	Subject     string `json:"subject"`
	DisplayName string `json:"displayName"`
	Challenge   string `json:"challenge"`
	Expires     int64  `json:"expires"`
}

func (a *Authenticator) seal(p codePayload) (string, error) {
	body, e := json.Marshal(p)
	if e != nil {
		return "", e
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *Authenticator) open(code string) (codePayload, error) {
	body, sig, ok := strings.Cut(code, ".")
	if !ok {
		return codePayload{}, errors.New("malformed code")
	}
	raw, e := base64.RawURLEncoding.DecodeString(body)
	if e != nil {
		return codePayload{}, errors.New("malformed code")
	}
	got, e := base64.RawURLEncoding.DecodeString(sig)
	if e != nil {
		return codePayload{}, errors.New("malformed code")
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(raw)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return codePayload{}, errors.New("code signature mismatch")
	}
	var p codePayload
	if e := json.Unmarshal(raw, &p); e != nil {
		return codePayload{}, errors.New("malformed code")
	}
	return p, nil
}

// formData feeds the template; every value is escaped by html/template.
type formData struct {
	State       string
	Challenge   string
	Source      string
	Subject     string
	DisplayName string
	Error       string
}

func (a *Authenticator) form(c *gin.Context) {
	state, challenge := c.Query("state"), c.Query("code_challenge")
	if state == "" || challenge == "" {
		c.String(http.StatusBadRequest, "missing state or code_challenge; start the login from the application")
		return
	}
	a.render(c, http.StatusOK, &formData{State: state, Challenge: challenge, Source: DefaultSource, Subject: "developer", DisplayName: "Developer"})
}

// submit turns the chosen identity into a signed code and sends the browser to the callback. The
// form is same-origin only so a cross-site page cannot complete a login on the developer's behalf.
func (a *Authenticator) submit(c *gin.Context) {
	if !gateway.SameOrigin(c.Request, a.publicOrigin) {
		c.String(http.StatusForbidden, "cross-site form submission refused")
		return
	}
	data := formData{
		State:       c.PostForm("state"),
		Challenge:   c.PostForm("code_challenge"),
		Source:      strings.TrimSpace(c.PostForm("source")),
		Subject:     strings.TrimSpace(c.PostForm("subject")),
		DisplayName: strings.TrimSpace(c.PostForm("display_name")),
	}
	if data.State == "" || data.Challenge == "" {
		c.String(http.StatusBadRequest, "missing state or code_challenge; start the login from the application")
		return
	}
	identity, e := gateway.Normalize(gateway.VerifiedIdentity{Source: data.Source, Subject: data.Subject, DisplayName: data.DisplayName})
	if e != nil {
		data.Error = "source and subject are required (at most 128 and 512 bytes)"
		a.render(c, http.StatusUnprocessableEntity, &data)
		return
	}
	code, e := a.seal(codePayload{Source: identity.Source, Subject: identity.Subject, DisplayName: identity.DisplayName, Challenge: data.Challenge, Expires: a.now().Add(codeLifetime).Unix()})
	if e != nil {
		c.String(http.StatusInternalServerError, "could not issue code")
		return
	}
	q := url.Values{"state": {data.State}, "code": {code}}
	c.Redirect(http.StatusSeeOther, a.callbackURL+"?"+q.Encode())
}

func (a *Authenticator) render(c *gin.Context, status int, data *formData) {
	c.Header("Cache-Control", "no-store")
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if e := page.Execute(c.Writer, data); e != nil {
		_ = c.Error(e)
	}
}

var page = template.Must(template.New("dev-login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Ora development sign-in</title>
<style>
  body { font: 15px/1.5 system-ui, sans-serif; background: #f6f6f7; color: #1a1a1a; margin: 0; display: flex; min-height: 100vh; align-items: center; justify-content: center; }
  form { background: #fff; border: 1px solid #e3e3e6; border-radius: 12px; padding: 28px; width: min(380px, 92vw); display: grid; gap: 14px; }
  h1 { font-size: 18px; margin: 0; }
  p { margin: 0; color: #666; font-size: 13px; }
  label { display: grid; gap: 4px; font-size: 13px; font-weight: 600; }
  input { font: inherit; padding: 8px 10px; border: 1px solid #cfcfd4; border-radius: 8px; }
  button { font: inherit; font-weight: 600; padding: 10px; border: 0; border-radius: 8px; background: #1a1a1a; color: #fff; cursor: pointer; }
  .error { color: #b3261e; }
</style>
</head>
<body>
<form method="post" action="{{.Action}}">
  <h1>Development sign-in</h1>
  <p>Local only. Whatever you type becomes the signed-in identity; the same source and subject always map to the same Ora user.</p>
  <input type="hidden" name="state" value="{{.State}}">
  <input type="hidden" name="code_challenge" value="{{.Challenge}}">
  <label>Source <input name="source" value="{{.Source}}" required></label>
  <label>Subject <input name="subject" value="{{.Subject}}" required autofocus></label>
  <label>Display name <input name="display_name" value="{{.DisplayName}}"></label>
  {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
  <button type="submit">Continue</button>
</form>
</body>
</html>
`))

// Action is the form target; a method on formData keeps the template free of package state.
func (formData) Action() string { return AuthorizePath }
