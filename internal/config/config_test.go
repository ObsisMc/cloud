package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadParsesDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("server:\n  read_timeout: 10s\n  write_timeout: 15s\ndatabase:\n  conn_max_lifetime: 1h\ncontrol:\n  grpc_addr: 127.0.0.1:8082\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ReadTimeout != 10*time.Second || cfg.Server.WriteTimeout != 15*time.Second || cfg.Database.ConnMaxLifetime != time.Hour {
		t.Fatalf("durations were not parsed: %+v %+v", cfg.Server, cfg.Database)
	}
	if cfg.Control.GRPCAddr != "127.0.0.1:8082" {
		t.Fatalf("control address was not parsed: %+v", cfg.Control)
	}
}

// The control listener has no safe default address: a Controller must be pointed at it explicitly.
func TestLoadRequiresControlAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("server:\n  read_timeout: 10s\n  write_timeout: 15s\ndatabase:\n  conn_max_lifetime: 1h\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("missing control.grpc_addr must fail configuration loading")
	}
}

// The `object_store` section is optional by decision (Cloud Revision D1): a deployment that leaves it
// out runs without object storage, and that is a legal state rather than a startup failure — every
// upload grant is then refused as UNAVAILABLE and each delivery fails until IssueRun D5's give-up
// window releases the run. What it must NOT do is acquire a lifetime it never asked for, or lose the
// approved default once it is configured.
func TestObjectStoreSectionIsOptionalAndCarriesTheApprovedDefaultLifetime(t *testing.T) {
	base := "server:\n  read_timeout: 10s\n  write_timeout: 15s\ndatabase:\n  conn_max_lifetime: 1h\ncontrol:\n  grpc_addr: 127.0.0.1:8082\n"

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("a deployment without object storage must still load: %v", err)
	}
	if cfg.ObjectStore.Configured() {
		t.Fatalf("an absent section must stay unconfigured, got %+v", cfg.ObjectStore)
	}
	if cfg.ObjectStore.UploadGrantTTL != 0 {
		t.Fatalf("an unconfigured section must not acquire a lifetime, got %s", cfg.ObjectStore.UploadGrantTTL)
	}

	// A configured one gets D1's approved default when the key is omitted, and its own value verbatim
	// when it is set — never a zero-length grant that expires as it is issued.
	configured := base + "object_store:\n  endpoint: \"http://127.0.0.1:9000\"\n  region: \"us-east-1\"\n  bucket: \"ora-revisions\"\n"
	if err := os.WriteFile(path, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ObjectStore.Configured() || cfg.ObjectStore.UploadGrantTTL != time.Minute*15 {
		t.Fatalf("a configured section must get D1's default lifetime, got %+v", cfg.ObjectStore)
	}

	explicit := configured + "  upload_grant_ttl: 30s\n"
	if err := os.WriteFile(path, []byte(explicit), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ObjectStore.UploadGrantTTL != 30*time.Second {
		t.Fatalf("an explicit lifetime must be used verbatim, got %s", cfg.ObjectStore.UploadGrantTTL)
	}
}

func TestEnvironmentOverridesKeysAbsentFromTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// No auth or control section and no database.dsn: all must still arrive from the environment.
	yaml := "server: {port: 8080, mode: test, read_timeout: 5s, write_timeout: 5s}\n" +
		"logger: {level: info, filename: " + filepath.ToSlash(filepath.Join(dir, "app.log")) + ", max_size: 1, max_backups: 1, max_age: 1, compress: false, enable_console: false}\n" +
		"database: {driver: postgres, max_idle_conns: 1, max_open_conns: 1, conn_max_lifetime: 1h}\n"
	if e := os.WriteFile(path, []byte(yaml), 0o600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CLOUD_DATABASE_DSN", "host=env-only")
	t.Setenv("CLOUD_AUTH_AUDIENCE", "ora-cloud")
	t.Setenv("CLOUD_CONTROL_GRPC_ADDR", "127.0.0.1:8082")
	cfg, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if cfg.Database.DSN != "host=env-only" || cfg.Auth.Audience != "ora-cloud" || cfg.Control.GRPCAddr != "127.0.0.1:8082" {
		t.Fatalf("environment must supply keys absent from the file: %+v", cfg)
	}
}
