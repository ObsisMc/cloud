package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/internal/core"
)

func generateKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return pub, key
}

func newTestIssuer(t *testing.T, now func() time.Time) (*Issuer, *core.Authenticator) {
	t.Helper()
	servicePub, serviceKey := generateKey(t)
	userPub, userKey := generateKey(t)
	issuer, e := NewIssuer("ora-internal-issuer", "ora-cloud", "gateway-a", SigningKey{ID: "svc", Key: serviceKey}, SigningKey{ID: "usr", Key: userKey}, time.Minute, now)
	if e != nil {
		t.Fatal(e)
	}
	verifier, e := core.NewAuthenticator("ora-cloud", []core.TrustedKey{
		{ID: "svc", Issuer: "ora-internal-issuer", Kind: "service", Role: "gateway", Key: servicePub},
		{ID: "usr", Issuer: "ora-internal-issuer", Kind: "user", Key: userPub},
	})
	if e != nil {
		t.Fatal(e)
	}
	verifier.Now = now
	return issuer, verifier
}

func TestIssuedCredentialsSatisfyCloudVerification(t *testing.T) {
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	issuer, verifier := newTestIssuer(t, func() time.Time { return base })
	creds, e := issuer.Issue(VerifiedIdentity{Source: "github.com", Subject: "42", DisplayName: "Ray"})
	if e != nil {
		t.Fatal(e)
	}
	service, e := verifier.Verify(creds.Service, "service")
	if e != nil || service.Role != "gateway" || service.Subject != "gateway-a" {
		t.Fatalf("service credential rejected by Cloud rules: %v %+v", e, service)
	}
	user, e := verifier.Verify(creds.User, "user")
	if e != nil || user.Caller != service.Subject || user.Source != "github.com" || user.Subject != "42" || user.DisplayName != "Ray" {
		t.Fatalf("user credential rejected or unbound: %v %+v", e, user)
	}
	if user.ExpiresAt.Sub(user.IssuedAt.Time) != time.Minute {
		t.Fatalf("credential lifetime must be the configured short lifetime, got %s", user.ExpiresAt.Sub(user.IssuedAt.Time))
	}
	// Purpose binding: a service key must not verify as a user credential and vice versa.
	if _, e = verifier.Verify(creds.Service, "user"); e == nil {
		t.Fatal("service credential accepted as user credential")
	}
	if _, e = verifier.Verify(creds.User, "service"); e == nil {
		t.Fatal("user credential accepted as service credential")
	}
	verifier.Now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, e = verifier.Verify(creds.User, "user"); e == nil {
		t.Fatal("expired internal credential must not verify; browser session lifetime must not leak into JWTs")
	}
}

func TestNewIssuerRejectsSharedKeysAndLongLifetimes(t *testing.T) {
	_, key := generateKey(t)
	_, other := generateKey(t)
	if _, e := NewIssuer("iss", "aud", "gw", SigningKey{ID: "a", Key: key}, SigningKey{ID: "b", Key: key}, time.Minute, nil); e == nil {
		t.Fatal("one private key for both purposes must be rejected")
	}
	if _, e := NewIssuer("iss", "aud", "gw", SigningKey{ID: "a", Key: key}, SigningKey{ID: "a", Key: other}, time.Minute, nil); e == nil {
		t.Fatal("duplicate key IDs must be rejected")
	}
	if _, e := NewIssuer("iss", "aud", "gw", SigningKey{ID: "a", Key: key}, SigningKey{ID: "b", Key: other}, MaxCredentialLifetime+time.Second, nil); e == nil {
		t.Fatal("lifetime above Cloud's ceiling must be rejected")
	}
	if _, e := NewIssuer("iss", "aud", "", SigningKey{ID: "a", Key: key}, SigningKey{ID: "b", Key: other}, time.Minute, nil); e == nil {
		t.Fatal("empty service subject must be rejected")
	}
}

func TestLoadPrivateKeyAcceptsOnlyPKCS8Ed25519(t *testing.T) {
	dir := t.TempDir()
	_, key := generateKey(t)
	der, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	good := filepath.Join(dir, "good.pem")
	if e = os.WriteFile(good, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadPrivateKey(good)
	if e != nil || !loaded.Equal(key) {
		t.Fatalf("PKCS#8 Ed25519 key must load: %v", e)
	}
	bad := filepath.Join(dir, "bad.pem")
	if e = os.WriteFile(bad, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadPrivateKey(bad); e == nil {
		t.Fatal("wrong PEM type must be rejected")
	}
	if _, e = LoadPrivateKey(filepath.Join(dir, "missing.pem")); e == nil {
		t.Fatal("missing file must be rejected")
	}
}
