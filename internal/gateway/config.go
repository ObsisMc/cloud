package gateway

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/wanglongan587/cloud/internal/config"
	"github.com/wanglongan587/cloud/internal/logger"
)

// Session lifetime policy: a positive absolute lifetime, 30 days by default, never above 90 days.
// The database enforces the same ceiling in gateway_sessions.
const (
	DefaultSessionLifetime = 30 * 24 * time.Hour
	MaxSessionLifetime     = 90 * 24 * time.Hour
	// CallbackPath is the fixed OAuth callback route under the public base URL.
	CallbackPath = "/auth/callback"
)

// Config is the complete Gateway process configuration. Secrets are referenced by file path and
// never appear inline; the loaded key material lives only in process memory.
type Config struct {
	Server   config.ServerConfig   `mapstructure:"server"`
	Logger   logger.Config         `mapstructure:"logger"`
	Database config.DatabaseConfig `mapstructure:"database"`
	Public   PublicConfig          `mapstructure:"public"`
	Session  SessionConfig         `mapstructure:"session"`
	Login    LoginConfig           `mapstructure:"login"`
	Cloud    CloudConfig           `mapstructure:"cloud"`
	Tokens   TokenConfig           `mapstructure:"tokens"`
	GitHub   GitHubConfig          `mapstructure:"github"`
}

// PublicConfig fixes the origin browsers see. The callback URL is derived from it, never from
// request headers. Development permits loopback HTTP and drops the Secure cookie attributes.
type PublicConfig struct {
	BaseURL     string `mapstructure:"base_url"`
	Development bool   `mapstructure:"development"`
}

// SessionConfig holds the absolute session lifetime and the bounded cleanup policy.
type SessionConfig struct {
	TTL             time.Duration `mapstructure:"ttl"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`
	Retention       time.Duration `mapstructure:"retention"`
	CleanupBatch    int           `mapstructure:"cleanup_batch"`
}

// LoginConfig bounds login attempts and the unauthenticated start/callback rate.
// DevelopmentProvider registers the "dev" adapter (internal/gateway/devlogin), which lets a
// developer type any identity; it is only accepted together with public.development.
type LoginConfig struct {
	AttemptTTL          time.Duration `mapstructure:"attempt_ttl"`
	RateLimitPerMinute  int           `mapstructure:"rate_limit_per_minute"`
	RateLimitBurst      int           `mapstructure:"rate_limit_burst"`
	PKCEKeyFile         string        `mapstructure:"pkce_key_file"`
	DevelopmentProvider bool          `mapstructure:"development_provider"`
}

// CloudConfig names the single fixed upstream; requests can never select another.
type CloudConfig struct {
	Upstream string        `mapstructure:"upstream"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// TokenConfig describes the two internal signing keys and the claims Cloud verifies.
type TokenConfig struct {
	Issuer                string        `mapstructure:"issuer"`
	Audience              string        `mapstructure:"audience"`
	ServiceSubject        string        `mapstructure:"service_subject"`
	ServiceKeyID          string        `mapstructure:"service_key_id"`
	ServicePrivateKeyFile string        `mapstructure:"service_private_key_file"`
	UserKeyID             string        `mapstructure:"user_key_id"`
	UserPrivateKeyFile    string        `mapstructure:"user_private_key_file"`
	Lifetime              time.Duration `mapstructure:"lifetime"`
}

// GitHubConfig configures the GitHub adapter; an empty client_id leaves it unregistered. Endpoint
// overrides exist for GitHub Enterprise Server and tests; the client secret is read from a file.
type GitHubConfig struct {
	ClientID         string `mapstructure:"client_id"`
	ClientSecretFile string `mapstructure:"client_secret_file"`
	AuthorizeURL     string `mapstructure:"authorize_url"`
	TokenURL         string `mapstructure:"token_url"`
	UserURL          string `mapstructure:"user_url"`
	Source           string `mapstructure:"source"`
}

// LoadConfig reads the Gateway configuration file and GATEWAY_* environment overrides, then applies
// defaults and validates every policy bound so the process fails at startup rather than at first use.
func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("gateway")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath("../configs")
		v.AddConfigPath(".")
	}
	v.SetEnvPrefix("GATEWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if e := v.ReadInConfig(); e != nil {
		return nil, e
	}
	var cfg Config
	if e := v.Unmarshal(&cfg); e != nil {
		return nil, e
	}
	if e := cfg.applyDefaults(); e != nil {
		return nil, e
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() error {
	if c.Session.TTL == 0 {
		c.Session.TTL = DefaultSessionLifetime
	}
	if c.Session.CleanupInterval == 0 {
		c.Session.CleanupInterval = 10 * time.Minute
	}
	if c.Session.Retention == 0 {
		c.Session.Retention = 7 * 24 * time.Hour
	}
	if c.Session.CleanupBatch == 0 {
		c.Session.CleanupBatch = 500
	}
	if c.Login.AttemptTTL == 0 {
		c.Login.AttemptTTL = 10 * time.Minute
	}
	if c.Login.RateLimitPerMinute == 0 {
		c.Login.RateLimitPerMinute = 30
	}
	if c.Login.RateLimitBurst == 0 {
		c.Login.RateLimitBurst = c.Login.RateLimitPerMinute
	}
	if c.Cloud.Timeout == 0 {
		c.Cloud.Timeout = 30 * time.Second
	}
	if c.Tokens.Lifetime == 0 {
		c.Tokens.Lifetime = time.Minute
	}
	if c.GitHub.Source == "" {
		c.GitHub.Source = "github.com"
	}
	return c.Validate()
}

// Validate rejects any configuration that would weaken a security bound at runtime.
func (c *Config) Validate() error {
	if c.Server.ReadTimeout <= 0 || c.Server.WriteTimeout <= 0 || c.Database.ConnMaxLifetime <= 0 {
		return fmt.Errorf("server timeouts and database.conn_max_lifetime must be positive durations")
	}
	if _, e := PublicOrigin(c.Public.BaseURL, c.Public.Development); e != nil {
		return e
	}
	if c.Session.TTL <= 0 || c.Session.TTL > MaxSessionLifetime {
		return fmt.Errorf("session.ttl must be positive and at most %s", MaxSessionLifetime)
	}
	if c.Session.CleanupInterval <= 0 || c.Session.Retention < 0 || c.Session.CleanupBatch <= 0 {
		return fmt.Errorf("session cleanup interval and batch must be positive and retention non-negative")
	}
	if c.Login.AttemptTTL < time.Minute || c.Login.AttemptTTL > time.Hour {
		return fmt.Errorf("login.attempt_ttl must be between 1 minute and 1 hour")
	}
	if c.Login.RateLimitPerMinute <= 0 || c.Login.RateLimitBurst <= 0 || c.Login.PKCEKeyFile == "" {
		return fmt.Errorf("login rate limit must be positive and login.pkce_key_file is required")
	}
	upstream, e := url.Parse(c.Cloud.Upstream)
	if e != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.Host == "" || upstream.Path != "" || upstream.RawQuery != "" {
		return fmt.Errorf("cloud.upstream must be an http(s) origin without path or query")
	}
	if c.Cloud.Timeout <= 0 {
		return fmt.Errorf("cloud.timeout must be positive")
	}
	if c.Tokens.Issuer == "" || c.Tokens.Audience == "" || c.Tokens.ServiceSubject == "" || c.Tokens.ServiceKeyID == "" || c.Tokens.UserKeyID == "" || c.Tokens.ServicePrivateKeyFile == "" || c.Tokens.UserPrivateKeyFile == "" {
		return fmt.Errorf("tokens issuer, audience, service_subject, key IDs and private key files are required")
	}
	if c.Tokens.Lifetime <= 0 || c.Tokens.Lifetime > MaxCredentialLifetime {
		return fmt.Errorf("tokens.lifetime must be positive and at most %s", MaxCredentialLifetime)
	}
	if c.Login.DevelopmentProvider && !c.Public.Development {
		return fmt.Errorf("login.development_provider requires public.development")
	}
	if c.GitHub.ClientID != "" && c.GitHub.ClientSecretFile == "" {
		return fmt.Errorf("github.client_secret_file is required with github.client_id")
	}
	if c.GitHub.ClientID == "" && !c.Login.DevelopmentProvider {
		return fmt.Errorf("at least one login provider is required: github.client_id or login.development_provider")
	}
	return nil
}

// PublicOrigin validates the public base URL and returns its origin (scheme://host). Production
// requires HTTPS; development may use loopback HTTP because providers permit it for local testing.
func PublicOrigin(baseURL string, development bool) (string, error) {
	u, e := url.Parse(baseURL)
	if e != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("public.base_url must be an origin such as https://app.example.com")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && development && isLoopback(u.Hostname()):
	default:
		return "", fmt.Errorf("public.base_url must use https, or http on loopback with public.development enabled")
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
