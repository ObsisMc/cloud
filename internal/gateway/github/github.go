// Package github adapts the GitHub OAuth App authorization code flow to the Gateway's
// provider-neutral Authenticator contract. It is the only code that understands GitHub's endpoints
// and user document; it cannot grant any Ora permission.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wanglongan587/cloud/internal/gateway"
)

// Default public GitHub endpoints. Overrides serve GitHub Enterprise Server and tests.
const (
	DefaultAuthorizeURL = "https://github.com/login/oauth/authorize"
	DefaultTokenURL     = "https://github.com/login/oauth/access_token" // #nosec G101 -- endpoint, not a credential.
	DefaultUserURL      = "https://api.github.com/user"
	DefaultSource       = "github.com"
	// maxResponseBytes bounds both provider responses; the user document is a few kilobytes.
	maxResponseBytes = 64 << 10
)

// Authenticator implements gateway.Authenticator for one GitHub OAuth App.
type Authenticator struct {
	clientID, clientSecret          string
	authorizeURL, tokenURL, userURL string
	source                          string
	http                            *http.Client
}

// Options configures the adapter. Empty endpoint and source fields use the public GitHub defaults.
type Options struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	UserURL      string
	Source       string
	HTTP         *http.Client
}

// New validates the adapter configuration. The HTTP client must carry a timeout because provider
// calls happen on the login path and must never hang a Gateway worker.
func New(o *Options) (*Authenticator, error) {
	if o == nil || o.ClientID == "" || o.ClientSecret == "" {
		return nil, errors.New("github client ID and secret are required")
	}
	if o.HTTP == nil || o.HTTP.Timeout <= 0 {
		return nil, errors.New("github HTTP client with a timeout is required")
	}
	a := &Authenticator{clientID: o.ClientID, clientSecret: o.ClientSecret, authorizeURL: o.AuthorizeURL, tokenURL: o.TokenURL, userURL: o.UserURL, source: o.Source, http: o.HTTP}
	if a.authorizeURL == "" {
		a.authorizeURL = DefaultAuthorizeURL
	}
	if a.tokenURL == "" {
		a.tokenURL = DefaultTokenURL
	}
	if a.userURL == "" {
		a.userURL = DefaultUserURL
	}
	if a.source == "" {
		a.source = DefaultSource
	}
	for _, endpoint := range []string{a.authorizeURL, a.tokenURL, a.userURL} {
		u, e := url.Parse(endpoint)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("invalid github endpoint %q", endpoint)
		}
	}
	return a, nil
}

// Source is the identity namespace this adapter produces.
func (a *Authenticator) Source() string { return a.source }

// AuthorizationURL requests only the minimal scope (none) and binds state and the S256 challenge.
// prompt=select_account makes GitHub show its account picker every time: without it GitHub silently
// reuses whichever github.com session the browser holds, so a member could never sign in to Ora with
// a different GitHub account than the one already authorized.
func (a *Authenticator) AuthorizationURL(request gateway.AuthorizationRequest) (string, error) {
	if request.State == "" || request.CodeChallenge == "" || request.CallbackURL == "" {
		return "", errors.New("state, code challenge and callback URL are required")
	}
	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", request.CallbackURL)
	q.Set("state", request.State)
	q.Set("code_challenge", request.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("allow_signup", "false")
	q.Set("prompt", "select_account")
	return a.authorizeURL + "?" + q.Encode(), nil
}

// Exchange redeems the code with the verifier, reads the authenticated user, and discards the access
// token. The token and the full user document never leave this function.
func (a *Authenticator) Exchange(ctx context.Context, code, codeVerifier, callbackURL string) (gateway.VerifiedIdentity, error) {
	token, e := a.exchangeCode(ctx, code, codeVerifier, callbackURL)
	if e != nil {
		return gateway.VerifiedIdentity{}, e
	}
	return a.readUser(ctx, token)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
}

func (a *Authenticator) exchangeCode(ctx context.Context, code, codeVerifier, callbackURL string) (string, error) {
	form := url.Values{}
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	form.Set("code", code)
	form.Set("code_verifier", codeVerifier)
	form.Set("redirect_uri", callbackURL)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return "", fmt.Errorf("build token request: %w", e)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var out tokenResponse
	status, e := a.do(req, &out)
	if e != nil {
		return "", e
	}
	if status != http.StatusOK || out.Error != "" || out.AccessToken == "" || !strings.EqualFold(out.TokenType, "bearer") {
		// GitHub answers 200 with an error object for a bad code or verifier; both are provider rejections.
		return "", fmt.Errorf("%w: token exchange status %d", gateway.ErrProviderRejected, status)
	}
	return out.AccessToken, nil
}

type userResponse struct {
	ID    json.Number `json:"id"`
	Login string      `json:"login"`
	Name  string      `json:"name"`
}

func (a *Authenticator) readUser(ctx context.Context, token string) (gateway.VerifiedIdentity, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, a.userURL, http.NoBody)
	if e != nil {
		return gateway.VerifiedIdentity{}, fmt.Errorf("build user request: %w", e)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	var out userResponse
	status, e := a.do(req, &out)
	if e != nil {
		return gateway.VerifiedIdentity{}, e
	}
	if status != http.StatusOK {
		return gateway.VerifiedIdentity{}, fmt.Errorf("%w: user status %d", gateway.ErrProviderRejected, status)
	}
	// The numeric ID is the only stable identity key; login and name may change and are display only.
	id, e := strconv.ParseInt(out.ID.String(), 10, 64)
	if e != nil || id <= 0 {
		return gateway.VerifiedIdentity{}, fmt.Errorf("%w: missing numeric user id", gateway.ErrProviderRejected)
	}
	name := strings.TrimSpace(out.Name)
	if name == "" {
		name = strings.TrimSpace(out.Login)
	}
	return gateway.VerifiedIdentity{Source: a.source, Subject: strconv.FormatInt(id, 10), DisplayName: name}, nil
}

// do performs one bounded provider call and decodes JSON. Network and timeout failures are
// infrastructure errors, not provider rejections, so the orchestration can distinguish them.
func (a *Authenticator) do(req *http.Request, out any) (int, error) {
	resp, e := a.http.Do(req)
	if e != nil {
		return 0, fmt.Errorf("github request: %w", e)
	}
	defer func() { _ = resp.Body.Close() }()
	body, e := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if e != nil {
		return 0, fmt.Errorf("read github response: %w", e)
	}
	if len(body) > maxResponseBytes {
		return 0, fmt.Errorf("%w: response too large", gateway.ErrProviderRejected)
	}
	if e = json.Unmarshal(body, out); e != nil {
		return resp.StatusCode, fmt.Errorf("%w: malformed response", gateway.ErrProviderRejected)
	}
	return resp.StatusCode, nil
}

// NewHTTPClient is the recommended provider client: no environment proxy surprises, a total timeout,
// and no redirects, since neither endpoint legitimately redirects.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
