package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/wanglongan587/cloud/internal/gateway"
	"github.com/wanglongan587/cloud/internal/gateway/idaas"
)

// The startup line is the deployment's only way to see which endpoints the process really uses
// after an override silently fails to reach it. It must name the effective targets and nothing
// that identifies or authenticates the application.
func TestIDaaSEndpointLogNamesEffectiveTargetsWithoutSecrets(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logIDaaSEndpoints(zap.New(core), idaas.Endpoints{
		Authorize: idaas.Endpoint{URL: "https://sso.example.org/authorize", Override: true},
		Token:     idaas.Endpoint{URL: "https://tokens.example.org/custom/token", Override: true},
		UserInfo:  idaas.Endpoint{URL: "https://profile.example.org/custom/userinfo"},
	})
	entries := logs.All()
	if len(entries) != 1 || entries[0].Message != "IDaaS endpoints resolved" {
		t.Fatalf("startup line = %+v", entries)
	}
	fields := entries[0].ContextMap()
	if fields["authorize"] != "https://sso.example.org/authorize" ||
		fields["token"] != "https://tokens.example.org/custom/token" ||
		fields["userinfo"] != "https://profile.example.org/custom/userinfo" {
		t.Fatalf("endpoint fields = %v", fields)
	}
	if fields["authorizeOverride"] != true || fields["tokenOverride"] != true || fields["userinfoOverride"] != false {
		t.Fatalf("override flags = %v, a silently ignored override must be visible here", fields)
	}
}

// buildProvider is the call site that makes the feature exist; a test of the helper alone keeps
// passing when the call disappears.
func TestBuildProviderLogsIDaaSEndpointsAtStartup(t *testing.T) {
	var raw bytes.Buffer
	observed, logs := observer.New(zapcore.InfoLevel)
	jsonCore := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&raw), zapcore.InfoLevel)
	secret := filepath.Join(t.TempDir(), "idaas-client-secret")
	if err := os.WriteFile(secret, []byte("synthetic-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &gateway.Config{
		Login: gateway.LoginConfig{Provider: gateway.ProviderHuaweiIDaaS},
		IDaaS: gateway.IDaaSConfig{BaseURL: "https://uniportal-beta.huawei.com", ClientID: "synthetic-client", ClientSecretFile: secret, Timeout: time.Second},
	}
	if _, err := buildProvider(cfg, zap.New(zapcore.NewTee(observed, jsonCore))); err != nil {
		t.Fatal(err)
	}
	entries := logs.FilterMessage("IDaaS endpoints resolved").All()
	if len(entries) != 1 {
		t.Fatalf("startup must log the effective endpoints exactly once, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["authorize"] != "https://uniportal-beta.huawei.com/saaslogin1/oauth2/v1/authorize" || fields["authorizeOverride"] != false {
		t.Fatalf("defaults and their provenance = %v", fields)
	}
	// Assert on the encoded record, not only on string fields: a future structured field must not
	// be able to carry the application identity or the secret.
	for _, forbidden := range []string{"synthetic-secret", "synthetic-client", secret} {
		if strings.Contains(raw.String(), forbidden) {
			t.Fatalf("startup line leaked %q: %s", forbidden, raw.String())
		}
	}
}

func TestBuildProviderReportsDeploymentOverrides(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "idaas-client-secret")
	if err := os.WriteFile(secret, []byte("synthetic-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zapcore.InfoLevel)
	cfg := &gateway.Config{
		Login: gateway.LoginConfig{Provider: gateway.ProviderHuaweiIDaaS},
		IDaaS: gateway.IDaaSConfig{
			BaseURL: "https://uniportal.huawei.com", AuthorizeURL: "https://sso.example.org/authorize",
			UserInfoURL: "https://profile.example.org/custom/userinfo",
			ClientID:    "synthetic-client", ClientSecretFile: secret, Timeout: time.Second,
		},
	}
	if _, err := buildProvider(cfg, zap.New(core)); err != nil {
		t.Fatal(err)
	}
	fields := logs.FilterMessage("IDaaS endpoints resolved").All()[0].ContextMap()
	if fields["authorize"] != "https://sso.example.org/authorize" || fields["userinfo"] != "https://profile.example.org/custom/userinfo" ||
		fields["authorizeOverride"] != true || fields["tokenOverride"] != false || fields["userinfoOverride"] != true {
		t.Fatalf("end-to-end provenance = %v", fields)
	}
}

// A rejected override fails startup; it must not leave a line that looks like a working
// configuration.
func TestBuildProviderFailureLogsNoEndpoints(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	cfg := &gateway.Config{
		Login: gateway.LoginConfig{Provider: gateway.ProviderHuaweiIDaaS},
		IDaaS: gateway.IDaaSConfig{BaseURL: "https://uniportal.huawei.com", ClientSecretFile: "unused", Timeout: time.Second},
	}
	if _, err := buildProvider(cfg, zap.New(core)); err == nil {
		t.Fatal("an IDaaS adapter without a client ID must fail")
	}
	if entries := logs.FilterMessage("IDaaS endpoints resolved").All(); len(entries) != 0 {
		t.Fatalf("failed startup logged endpoints: %+v", entries)
	}
}

func TestBuildProviderDoesNotLogEndpointsForGitHub(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "github-client-secret")
	if err := os.WriteFile(secret, []byte("synthetic-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zapcore.InfoLevel)
	cfg := &gateway.Config{
		Login:  gateway.LoginConfig{Provider: gateway.ProviderGitHub},
		GitHub: gateway.GitHubConfig{ClientID: "synthetic-client", ClientSecretFile: secret},
		Cloud:  gateway.CloudConfig{Timeout: time.Second},
	}
	if _, err := buildProvider(cfg, zap.New(core)); err != nil {
		t.Fatal(err)
	}
	if entries := logs.FilterMessage("IDaaS endpoints resolved").All(); len(entries) != 0 {
		t.Fatalf("GitHub startup logged IDaaS endpoints: %+v", entries)
	}
}
