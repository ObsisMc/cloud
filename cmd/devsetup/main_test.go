package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if e := os.WriteFile(path, []byte(body), 0o600); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestLoadConfigRejectsMissingFileUnknownKeysAndEmptyValues(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown key", "[database]\ndsn='x'\n[github]\nclient_id='a'\nclient_secert='b'\n", "client_secert"},
		{"empty secret", "[database]\ndsn='x'\n[github]\nclient_id='a'\nclient_secret='  '\n", "must be set together"},
		{"secret without id", "[database]\ndsn='x'\n[github]\nclient_secret='b'\n", "must be set together"},
		{"everything empty", "[database]\n[github]\n", "database.dsn must be set"},
		{"idaas secret without id", "[database]\ndsn='x'\n[idaas]\nclient_secret='b'\n", "must be set together"},
		{"two external providers", "[database]\ndsn='x'\n[github]\nclient_id='a'\nclient_secret='b'\n[idaas]\nclient_id='c'\nclient_secret='d'\n", "not both"},
		{"not toml", "database: {dsn: x}\n", "parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := loadConfig(writeConfig(t, tc.body))
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, e)
			}
		})
	}
	_, e := loadConfig(filepath.Join(t.TempDir(), "config.toml"))
	if e == nil || !strings.Contains(e.Error(), "copy config.toml.template") {
		t.Fatalf("missing file must point at the template, got %v", e)
	}
	cfg, e := loadConfig(writeConfig(t, "[database]\ndsn = 'host=h dbname=d'\n[github]\nclient_id = 'id'\nclient_secret = 'sec'\n"))
	if e != nil || cfg.Database.DSN != "host=h dbname=d" || cfg.GitHub.ClientID != "id" || cfg.GitHub.ClientSecret != "sec" || cfg.externalProvider() != "github" {
		t.Fatalf("valid config not decoded: %+v %v", cfg, e)
	}
	cfg, e = loadConfig(writeConfig(t, "[database]\ndsn = 'host=h dbname=d'\n"))
	if e != nil || cfg.externalProvider() != "" {
		t.Fatalf("config without an external provider must be valid for the development login: %+v %v", cfg, e)
	}
	cfg, e = loadConfig(writeConfig(t, "[database]\ndsn = 'host=h dbname=d'\n[idaas]\nbase_url = 'https://uniportal-beta.huawei.com'\nclient_id = 'app'\nclient_secret = 'sec'\ndisplay_name_field = 'userName'\n"))
	if e != nil || cfg.externalProvider() != "huawei-idaas" || cfg.IDaaS.BaseURL != "https://uniportal-beta.huawei.com" {
		t.Fatalf("IDaaS config not decoded: %+v %v", cfg, e)
	}
}

func TestTemplateIsCompleteWithGitHubLeftForTheDeveloper(t *testing.T) {
	cfg, e := loadConfig(filepath.Join("..", "..", "config.toml.template"))
	if e != nil || cfg.externalProvider() != "" {
		t.Fatalf("template must be usable as-is with no external provider, got %+v %v", cfg, e)
	}
	if !strings.Contains(cfg.Database.DSN, "port=55432") {
		t.Fatalf("template DSN must match compose.yaml, got %q", cfg.Database.DSN)
	}
}

func TestRenderEnvQuotesValuesForTask(t *testing.T) {
	var cfg devConfig
	cfg.Database.DSN = `host=127.0.0.1 password=p"a\ss dbname=ora`
	cfg.GitHub.ClientID = "Iv1.abc"
	got := renderEnv(&cfg)
	cfg.GitHub.ClientSecret = "s"
	for _, line := range []string{
		`CLOUD_DATABASE_DSN="host=127.0.0.1 password=p\"a\\ss dbname=ora"`,
		`GATEWAY_DATABASE_DSN="host=127.0.0.1 password=p\"a\\ss dbname=ora"`,
		`TEST_DATABASE_URL="host=127.0.0.1 password=p\"a\\ss dbname=ora"`,
		`GATEWAY_LOGIN_PROVIDER="github"`,
		`GATEWAY_GITHUB_CLIENT_ID="Iv1.abc"`,
		`GATEWAY_IDAAS_CLIENT_ID=""`,
	} {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("env file missing %s:\n%s", line, got)
		}
	}
}

func TestMaterializeKeepsKeysAndRewritesDerivedFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "gateway")
	envPath := filepath.Join(root, "dev.env")
	var cfg devConfig
	cfg.Database.DSN = "host=h dbname=d"
	cfg.GitHub.ClientID = "id"
	cfg.GitHub.ClientSecret = "first-secret"
	if e := materialize(&cfg, dir, envPath); e != nil {
		t.Fatal(e)
	}
	read := func(name string) []byte {
		t.Helper()
		b, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	privPEM, pubPEM := read(filepath.Join(dir, "gateway-service.key")), read(filepath.Join(dir, "gateway-service.pem"))
	privBlock, _ := pem.Decode(privPEM)
	priv, e := x509.ParsePKCS8PrivateKey(privBlock.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	pubBlock, _ := pem.Decode(pubPEM)
	pub, e := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	if !priv.(ed25519.PrivateKey).Public().(ed25519.PublicKey).Equal(pub) {
		t.Fatal("public key must belong to the private key Cloud is asked to trust")
	}
	if pkce := read(filepath.Join(dir, "gateway-pkce.key")); len(pkce) != 32 {
		t.Fatalf("PKCE key must be 32 bytes, got %d", len(pkce))
	}
	if got := string(read(filepath.Join(dir, "github-client-secret"))); got != "first-secret" {
		t.Fatalf("secret file = %q", got)
	}
	for _, name := range []string{filepath.Join(dir, "gateway-service.key"), filepath.Join(dir, "github-client-secret"), envPath} {
		info, e := os.Stat(name)
		if e != nil {
			t.Fatal(e)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s mode = %o, want 600", name, perm)
		}
	}

	cfg.GitHub.ClientSecret = "rotated-secret"
	cfg.GitHub.ClientID = "id2"
	if e := materialize(&cfg, dir, envPath); e != nil {
		t.Fatal(e)
	}
	if string(read(filepath.Join(dir, "gateway-service.key"))) != string(privPEM) {
		t.Fatal("rerun must not regenerate a key the running Cloud trusts")
	}
	if got := string(read(filepath.Join(dir, "github-client-secret"))); got != "rotated-secret" {
		t.Fatalf("rerun must rewrite the secret from config.toml, got %q", got)
	}
	if !strings.Contains(string(read(envPath)), `GATEWAY_GITHUB_CLIENT_ID="id2"`) {
		t.Fatal("rerun must rewrite the env file from config.toml")
	}

	cfg.GitHub.ClientID, cfg.GitHub.ClientSecret = "", ""
	if e := materialize(&cfg, dir, envPath); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dir, "github-client-secret")); !errors.Is(e, fs.ErrNotExist) {
		t.Fatalf("dropping [github] must remove the stale secret file, got %v", e)
	}
	if env := string(read(envPath)); !strings.Contains(env, `GATEWAY_GITHUB_CLIENT_ID=""`) || !strings.Contains(env, `GATEWAY_LOGIN_PROVIDER=""`) {
		t.Fatal("dropping [github] must leave an empty client id and provider in the env file")
	}

	cfg.IDaaS.BaseURL, cfg.IDaaS.ClientID, cfg.IDaaS.ClientSecret = "https://uniportal-beta.huawei.com", "app", "idaas-secret"
	if e := materialize(&cfg, dir, envPath); e != nil {
		t.Fatal(e)
	}
	if got := string(read(filepath.Join(dir, "idaas-client-secret"))); got != "idaas-secret" {
		t.Fatalf("idaas secret file = %q", got)
	}
	if env := string(read(envPath)); !strings.Contains(env, `GATEWAY_LOGIN_PROVIDER="huawei-idaas"`) || !strings.Contains(env, `GATEWAY_IDAAS_BASE_URL="https://uniportal-beta.huawei.com"`) {
		t.Fatalf("IDaaS config must reach the env file:\n%s", env)
	}

	if e := os.Remove(filepath.Join(dir, "user-identity.pem")); e != nil {
		t.Fatal(e)
	}
	if e := materialize(&cfg, dir, envPath); e == nil || !strings.Contains(e.Error(), "must exist together") {
		t.Fatalf("a half-present pair must be refused, got %v", e)
	}
}
