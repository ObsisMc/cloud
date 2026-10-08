// Package config handles application configuration loading and parsing.
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/wanglongan587/cloud/internal/core"
	"github.com/wanglongan587/cloud/internal/logger"
)

// Config holds all configuration of the application.
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Logger        logger.Config       `mapstructure:"logger"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Auth          AuthConfig          `mapstructure:"auth"`
	Collaboration CollaborationConfig `mapstructure:"collaboration"`
	Directory     DirectoryConfig     `mapstructure:"directory"`
	Control       ControlConfig       `mapstructure:"control"`
	Plugins       PluginConfig        `mapstructure:"plugins"`
	IssueRuns     IssueRunsConfig     `mapstructure:"issue_runs"`
	ObjectStore   ObjectStoreConfig   `mapstructure:"object_store"`
}

// ObjectStoreConfig is Cloud's own object-store access (Cloud Revision D1). Leaf keys are bound to
// CLOUD_OBJECT_STORE_* environment overrides like every other section.
//
// The whole section is optional: a deployment that leaves it empty runs without object storage, which
// the approved decision makes a legal state rather than a startup failure. What that deployment
// cannot do is save a Revision — every upload grant is refused as UNAVAILABLE and the delivery fails
// deterministically until IssueRun D5's give-up window closes the run out — so an empty section is
// reported at startup and never silently substituted for a working one.
type ObjectStoreConfig struct {
	// Endpoint is the S3-compatible endpoint Cloud talks to. Required once any other key is set.
	Endpoint string `mapstructure:"endpoint"`

	// PublicEndpoint optionally replaces Endpoint in presigned URLs, for deployments whose Nodes
	// resolve the store at a different address than Cloud does. Empty means the two are the same.
	PublicEndpoint string `mapstructure:"public_endpoint"`

	// Region is the SigV4 signing region. It is not discovered: the signature must name the same
	// region the store is configured with.
	Region string `mapstructure:"region"`

	// Bucket is the bucket Revision objects live in. Cloud never creates it (D1).
	Bucket string `mapstructure:"bucket"`

	// PathStyle selects path-style addressing (`<endpoint>/<bucket>/<key>`) over virtual-host style,
	// which MinIO and most local deployments need.
	PathStyle bool `mapstructure:"path_style"`

	// AccessKeyIDFile and SecretAccessKeyFile are PATHS to the credential files; the values never
	// appear in configuration, logs or the database (D1).
	AccessKeyIDFile     string `mapstructure:"access_key_id_file"`
	SecretAccessKeyFile string `mapstructure:"secret_access_key_file"`

	// UploadGrantTTL is how long one presigned upload stays usable. Zero or negative selects D1's
	// approved default (15 minutes).
	UploadGrantTTL time.Duration `mapstructure:"upload_grant_ttl"`
}

// Configured reports whether the deployment asked for object storage at all. Any key set means the
// section was meant to be used, so a partially filled one is a configuration error rather than an
// empty section — see Load.
//
//nolint:gocritic // hugeParam: a predicate over one section's own fields takes no shared state.
func (o ObjectStoreConfig) Configured() bool {
	return o.Endpoint != "" || o.PublicEndpoint != "" || o.Region != "" || o.Bucket != "" ||
		o.AccessKeyIDFile != "" || o.SecretAccessKeyFile != "" || o.UploadGrantTTL != 0
}

// IssueRunsConfig holds the Thread lifecycle policy that belongs to Cloud's own dispatch loop
// rather than to any individual run. Leaf keys are bound to CLOUD_ISSUE_RUNS_* environment
// overrides like every other section.
type IssueRunsConfig struct {
	// ThreadIdleTimeout is Thread D4's idle window (`issue_runs.thread_idle_timeout` in the plan's
	// spelling): how long a Thread may sit `idle` before Cloud asks the session to end. It is a
	// duration configured per deployment, never a per-run column, and the comparison that judges it
	// runs on database time — no Cloud, Controller or Node process clock decides that a Thread is
	// idle. Zero or negative selects the approved default (15 minutes) rather than a zero-length
	// window that would end every Thread the moment it went idle.
	ThreadIdleTimeout time.Duration `mapstructure:"thread_idle_timeout"`

	// DeliveryGiveUpAfter is IssueRun D5's continuous-delivery-failure window: how long a Revision
	// delivery may keep failing before Cloud abandons it (`deliveryState = failed`) and releases the
	// run's Workspace. It is the limit that keeps a Workspace from being held indefinitely by a
	// delivery that will never succeed. Zero or negative selects the approved default (2 hours).
	DeliveryGiveUpAfter time.Duration `mapstructure:"delivery_give_up_after"`

	// DeliveryUnreachableAfter is IssueRun D5's unreachability window: how long the run Workspace's
	// Node may be in an unknown state before Cloud treats the Workspace as unreachable and abandons
	// the delivery the same way, without waiting out the full failure window. Zero or negative
	// selects the approved default (30 minutes).
	DeliveryUnreachableAfter time.Duration `mapstructure:"delivery_unreachable_after"`
}

// PluginConfig is the cloud-side plugin marketplace configuration. Leaf keys
// are bound to CLOUD_PLUGINS_* environment overrides like every other section.
type PluginConfig struct {
	// MarketplaceURL is the git repository the marketplace catalog is synced from.
	MarketplaceURL string `mapstructure:"marketplace_url"`
	// MarketplaceBranch is the branch checked out and fast-forwarded on every sync.
	MarketplaceBranch string `mapstructure:"marketplace_branch"`
	// SyncInterval is the period between catalog syncs; the first sync runs at startup.
	SyncInterval time.Duration `mapstructure:"sync_interval"`
	// SyncEnabled turns the catalog sync loop on. When false the config section may
	// stay empty: no marketplace source is seeded and no sync loop runs.
	SyncEnabled bool `mapstructure:"sync_enabled"`
}

// CollaborationConfig gates optional collaboration-capability wiring on the Store.
type CollaborationConfig struct {
	// DevelopmentFixtures installs the in-memory dev/demo Agent/Team/Workflow fixtures
	// (internal/collab) onto the Store's collaboration ports. Development-only and explicitly
	// enabled: production must leave it false (default), in which case only human targets are
	// served. It must never be coupled to whether an external login provider is enabled.
	DevelopmentFixtures bool `mapstructure:"development_fixtures"`
}

// DirectoryConfig holds the fixed Tianzhou machine endpoint and secret file.
// Empty endpoint disables corporate lookup on public deployments.
type DirectoryConfig struct {
	Endpoint    string `mapstructure:"endpoint"`
	HWID        string `mapstructure:"hw_id"`
	Environment string `mapstructure:"environment"`
	AppKeyFile  string `mapstructure:"app_key_file"`
}

// ControlConfig binds the Controller-facing gRPC listener. Until the authentication ADR adds TLS,
// the address must stay on a loopback or private network; the contract carries bearer credentials
// only.
type ControlConfig struct {
	GRPCAddr string `mapstructure:"grpc_addr"`
}

// AuthConfig contains only internal verification keys, never an external login SDK.
type AuthConfig struct {
	Audience string            `mapstructure:"audience"`
	Keys     []core.TrustedKey `mapstructure:"keys"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	Mode         string        `mapstructure:"mode"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// DatabaseConfig holds database connection parameters.
type DatabaseConfig struct {
	Driver          string        `mapstructure:"driver"`
	DSN             string        `mapstructure:"dsn"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// Load reads configuration from file and environment variables.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath("../configs")
		v.AddConfigPath(".")
	}

	// Read environment variables (e.g., CLOUD_SERVER_PORT=8080)
	v.SetEnvPrefix("CLOUD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if err := BindEnvKeys(v, Config{}); err != nil {
		return nil, err
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if cfg.Server.ReadTimeout <= 0 || cfg.Server.WriteTimeout <= 0 || cfg.Database.ConnMaxLifetime <= 0 {
		return nil, fmt.Errorf("server timeouts and database.conn_max_lifetime must be positive durations")
	}
	if cfg.Control.GRPCAddr == "" {
		return nil, fmt.Errorf("control.grpc_addr is required")
	}
	if err := validatePlugins(cfg.Plugins); err != nil {
		return nil, err
	}
	// Thread D4's approved default. Applied here rather than left at the zero value so an operator
	// who omits the key (or sets a nonsensical value) gets the approved window instead of a
	// zero-length one that would end every idle Thread immediately.
	if cfg.IssueRuns.ThreadIdleTimeout <= 0 {
		cfg.IssueRuns.ThreadIdleTimeout = DefaultThreadIdleTimeout
	}
	// IssueRun D5's two approved defaults, applied for the same reason: an operator who omits the keys
	// gets the limits the decision fixes, not a zero-length window that would abandon every delivery
	// immediately.
	if cfg.IssueRuns.DeliveryGiveUpAfter <= 0 {
		cfg.IssueRuns.DeliveryGiveUpAfter = DefaultDeliveryGiveUpAfter
	}
	if cfg.IssueRuns.DeliveryUnreachableAfter <= 0 {
		cfg.IssueRuns.DeliveryUnreachableAfter = DefaultDeliveryUnreachableAfter
	}
	// Cloud Revision D1's approved upload-grant lifetime, applied for the same reason as the two
	// defaults above. Validation of the section's *contents* (a URL that parses, credentials that
	// exist) belongs to the object-store client's own constructor, which the wiring calls: a
	// deployment error there must fail startup before any delivery is attempted, but an unreachable
	// or not-yet-created bucket must not (D1), and only the client can tell the two apart.
	if cfg.ObjectStore.Configured() && cfg.ObjectStore.UploadGrantTTL <= 0 {
		cfg.ObjectStore.UploadGrantTTL = core.DefaultRevisionUploadTTL
	}
	return &cfg, nil
}

// DefaultThreadIdleTimeout is Thread D4's first-version idle window, applied when
// issue_runs.thread_idle_timeout is absent or not positive.
const DefaultThreadIdleTimeout = 15 * time.Minute

// DefaultDeliveryGiveUpAfter and DefaultDeliveryUnreachableAfter are IssueRun D5's first-version
// give-up limits ("这两个上限是第一版默认值（Cloud 配置项）"), applied when the corresponding key is
// absent or not positive.
const (
	DefaultDeliveryGiveUpAfter      = 2 * time.Hour
	DefaultDeliveryUnreachableAfter = 30 * time.Minute
)

// validatePlugins enforces the plugin marketplace configuration contract. An
// empty section is legal while sync is disabled; once sync is enabled the
// marketplace address and branch must be present and well-formed so a typo
// fails at startup instead of surfacing as a silently empty catalog.
func validatePlugins(p PluginConfig) error {
	if !p.SyncEnabled {
		return nil
	}
	if p.MarketplaceURL == "" {
		return fmt.Errorf("plugins.marketplace_url is required when plugins.sync_enabled is true")
	}
	parsed, err := url.Parse(p.MarketplaceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("plugins.marketplace_url must be an https git URL without query or fragment")
	}
	if p.MarketplaceBranch == "" {
		return fmt.Errorf("plugins.marketplace_branch must not be empty when plugins.sync_enabled is true")
	}
	if !validBranch(p.MarketplaceBranch) {
		return fmt.Errorf("plugins.marketplace_branch %q is not a valid git branch name", p.MarketplaceBranch)
	}
	if p.SyncInterval <= 0 {
		return fmt.Errorf("plugins.sync_interval must be a positive duration when plugins.sync_enabled is true")
	}
	return nil
}

// validBranch rejects branch names that a clone/checkout could mistake for
// something else: path separators, parent traversal, leading dashes (option
// injection), whitespace and control characters. It is deliberately stricter
// than git's own ref rules because the value comes from deployment
// configuration and is passed to a shelled-out git process.
func validBranch(s string) bool {
	if s == "" || len(s) > 200 || strings.HasPrefix(s, "-") || strings.Contains(s, "..") {
		return false
	}
	return !strings.ContainsAny(s, "/\\ \t\r\n\x00~^:?*[")
}
