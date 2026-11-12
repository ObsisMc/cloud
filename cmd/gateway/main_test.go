package main

import (
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

func TestBuildProviderLogsIDaaSEndpointsAtStartup(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "idaas-client-secret")
	if err := os.WriteFile(secret, []byte("synthetic-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zapcore.InfoLevel)
	cfg := &gateway.Config{
		Login: gateway.LoginConfig{Provider: gateway.ProviderHuaweiIDaaS},
		IDaaS: gateway.IDaaSConfig{BaseURL: "https://uniportal-beta.huawei.com", ClientID: "synthetic-client", ClientSecretFile: secret, Timeout: time.Second},
	}
	if _, err := buildProvider(cfg, zap.New(core)); err != nil {
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
	for key, value := range fields {
		if text, ok := value.(string); ok && strings.Contains(text, "synthetic-secret") {
			t.Fatalf("field %q leaked the client secret", key)
		}
	}
}

// The startup line is the deployment's only way to see which endpoints the process really uses
// after an override silently fails to reach it. It must name the effective targets and nothing
// that identifies or authenticates the application.
func TestIDaaSEndpointLogNamesEffectiveTargetsWithoutSecrets(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	cfg := &gateway.Config{IDaaS: gateway.IDaaSConfig{
		BaseURL: "https://uniportal.huawei.com", AuthorizeURL: "https://sso.example.org/authorize",
		ClientID: "synthetic-client", ClientSecretFile: "/run/ora-auth/provider-client-secret",
	}}
	logIDaaSEndpoints(zap.New(core), cfg, idaas.Endpoints{
		Authorize: "https://sso.example.org/authorize",
		Token:     "https://uniportal.huawei.com/saaslogin1/oauth2/v1/token",
		UserInfo:  "https://uniportal.huawei.com/saaslogin1/oauth2/v1/userinfo",
	})
	entries := logs.All()
	if len(entries) != 1 || entries[0].Message != "IDaaS endpoints resolved" {
		t.Fatalf("startup line = %+v", entries)
	}
	fields := entries[0].ContextMap()
	if fields["authorize"] != "https://sso.example.org/authorize" ||
		fields["token"] != "https://uniportal.huawei.com/saaslogin1/oauth2/v1/token" ||
		fields["userinfo"] != "https://uniportal.huawei.com/saaslogin1/oauth2/v1/userinfo" {
		t.Fatalf("endpoint fields = %v", fields)
	}
	if fields["authorizeOverride"] != true || fields["tokenOverride"] != false || fields["userinfoOverride"] != false {
		t.Fatalf("override flags = %v, a silently ignored override must be visible here", fields)
	}
	for key, value := range fields {
		if text, ok := value.(string); ok && (strings.Contains(text, "synthetic-client") || strings.Contains(text, "provider-client-secret")) {
			t.Fatalf("field %q leaks application identity or a secret path: %q", key, text)
		}
	}
}
