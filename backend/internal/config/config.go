package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

type AppEnvironment string

const (
	AppEnvironmentProduction  AppEnvironment = "production"
	AppEnvironmentDevelopment AppEnvironment = "development"
	AppEnvironmentTest        AppEnvironment = "test"
)

// Config holds all application configuration.
// Fields tagged with `env` will be loaded from the corresponding environment variable.
// Fields with `options:"file"` support Docker secrets via the _FILE suffix.
type Config struct {
	BuildablesConfig

	AppUrl                string         `env:"ARCANE_APP_URL" default:""`
	DatabaseURL           string         `env:"ARCANE_DATABASE_URL" default:"file:data/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_txlock=immediate" options:"file"`
	AllowDowngrade        bool           `env:"ARCANE_ALLOW_DOWNGRADE" default:"false"`
	AllowCLIPasswordReset bool           `env:"ARCANE_ALLOW_CLI_PASSWORD_RESET" default:"false"`
	AllowCLIMFAReset      bool           `env:"ARCANE_ALLOW_CLI_MFA_RESET" default:"false"`
	ActorPort             string         `env:"ARCANE_ACTOR_PORT" default:"3551"`
	Port                  string         `env:"ARCANE_PORT" default:"3552"`
	Listen                string         `env:"ARCANE_LISTEN" default:""`
	TLSEnabled            bool           `env:"ARCANE_TLS_ENABLED" default:"false"`
	TLSCertFile           string         `env:"ARCANE_TLS_CERT_FILE" default:""`
	TLSKeyFile            string         `env:"ARCANE_TLS_KEY_FILE" default:""`
	Environment           AppEnvironment `env:"ARCANE_ENVIRONMENT" default:"production"`
	JWTRefreshExpiry      time.Duration  `env:"ARCANE_JWT_REFRESH_EXPIRY" default:"168h"`
	EncryptionKey         string         `env:"ARCANE_ENCRYPTION_KEY" default:"" options:"file"`
	AdminStaticAPIKey     string         `env:"ARCANE_ADMIN_STATIC_API_KEY" default:"" options:"file"`
	ApnsRelayUrl          string         `env:"ARCANE_APNS_RELAY_URL" default:"https://apns.getarcane.app" options:"trimTrailingSlash"`
	ApnsKeyAlgorithm      string         `env:"ARCANE_APNS_KEY_ALGORITHM" default:"ed25519" options:"toLower"`

	OidcRoleMappings string `env:"ARCANE_OIDC_ROLE_MAPPINGS" default:"" options:"file"`

	PUID                    string `env:"PUID" default:""`
	PGID                    string `env:"PGID" default:""`
	DockerConfig            string `env:"DOCKER_CONFIG" default:""`
	InContainer             bool   `env:"ARCANE_IN_CONTAINER" default:"false"`
	LogJson                 bool   `env:"ARCANE_LOG_JSON" default:"false"`
	LogLevel                string `env:"ARCANE_LOG_LEVEL" default:"info" options:"toLower"`
	AgentMode               bool   `env:"ARCANE_AGENT_MODE" default:"false"`
	AgentToken              string `env:"ARCANE_AGENT_TOKEN" default:"" options:"file"`
	ManagerApiUrl           string `env:"ARCANE_MANAGER_API_URL" default:""`
	UIConfigurationDisabled bool   `env:"ARCANE_UI_CONFIGURATION_DISABLED" default:"false"`

	EdgeAgent             bool   `env:"ARCANE_EDGE_AGENT" default:"false"`
	EdgeTransport         string `env:"ARCANE_EDGE_TRANSPORT" default:"auto" options:"toLower"`
	EdgeReconnectInterval int    `env:"ARCANE_EDGE_RECONNECT_INTERVAL" default:"5"` // seconds
	EdgeMTLSMode          string `env:"ARCANE_EDGE_MTLS_MODE" default:"disabled" options:"toLower"`
	EdgeMTLSCAFile        string `env:"ARCANE_EDGE_MTLS_CA_FILE" default:""`
	EdgeMTLSCertFile      string `env:"ARCANE_EDGE_MTLS_CERT_FILE" default:""`
	EdgeMTLSKeyFile       string `env:"ARCANE_EDGE_MTLS_KEY_FILE" default:""`
	EdgeMTLSServerName    string `env:"ARCANE_EDGE_MTLS_SERVER_NAME" default:""`
	EdgeMTLSAssetsDir     string `env:"ARCANE_EDGE_MTLS_ASSETS_DIR" default:""`

	TrustedProxies       string      `env:"ARCANE_TRUSTED_PROXIES" default:""`
	FilePerm             os.FileMode `env:"ARCANE_FILE_PERM" default:"0644"`
	DirPerm              os.FileMode `env:"ARCANE_DIR_PERM" default:"0755"`
	GitWorkDir           string      `env:"ARCANE_GIT_WORK_DIR" default:"data/git"`
	BackupVolumeName     string      `env:"ARCANE_BACKUP_VOLUME_NAME" default:"arcane-backups"`
	UpgradeKeepContainer bool        `env:"ARCANE_UPGRADE_KEEP_CONTAINER" default:"false"`
	HTTPProxy            string      `env:"HTTP_PROXY" default:""`
	HTTPSProxy           string      `env:"HTTPS_PROXY" default:""`
	NoProxy              string      `env:"NO_PROXY" default:""`
	Timezone             string      `env:"TZ" default:"Local"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := loadFromEnv(cfg); err != nil {
		return nil, err
	}
	applyOptions(cfg)
	cfg.AgentMode = cfg.AgentMode || cfg.EdgeAgent
	applyProxyDefaults(cfg)

	key := strings.TrimSpace(cfg.EncryptionKey)
	if cfg.Environment.IsProdEnvironment() && !cfg.AgentMode && key != "" &&
		!strings.HasPrefix(key, "hex:") && !strings.HasPrefix(key, "base64:") && len(strings.TrimPrefix(key, "raw:")) < 32 {
		return nil, errors.New("ARCANE_ENCRYPTION_KEY passphrase must be at least 32 characters in production (or use a hex:/base64: encoded 32-byte key)")
	}

	// Set global file permissions
	utils.FilePerm = cfg.FilePerm
	utils.DirPerm = cfg.DirPerm

	return cfg, nil
}

func EnvNames() []string {
	var names []string
	visitConfigFields(reflect.ValueOf(&Config{}).Elem(), func(_ reflect.Value, fieldType reflect.StructField) {
		if envTag := fieldType.Tag.Get("env"); envTag != "" {
			names = append(names, envTag)
		}
	})
	return names
}

// applyProxyDefaults auto-trusts loopback proxies when LISTEN binds to a
// loopback address. This is the canonical convenience-script setup
// (LISTEN=127.0.0.1 + local reverse proxy like Caddy/Nginx/Traefik); auto-
// detecting it lets X-Forwarded-* headers work without operators having to
// discover TRUSTED_PROXIES.
func applyProxyDefaults(cfg *Config) {
	if cfg.TrustedProxies != "" {
		return
	}
	host := strings.Trim(strings.TrimSpace(cfg.Listen), "[]")
	if host == "" {
		return
	}
	// Require an explicit loopback IP literal. Hostnames like "localhost" are
	// intentionally not auto-trusted: /etc/hosts can map them to non-loopback
	// addresses, in which case auto-trusting would silently trust external
	// X-Forwarded-* headers. Operators using a hostname must set
	// TRUSTED_PROXIES explicitly.
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return
	}
	cfg.TrustedProxies = "127.0.0.0/8,::1/128"
	slog.InfoContext(context.Background(), //nolint:forbidigo // Configuration loads before any request context exists.
		"ARCANE_LISTEN bound to loopback; trusting loopback proxies for X-Forwarded headers",
		"listen", cfg.Listen, "trustedProxies", cfg.TrustedProxies)
}

// loadFromEnv uses reflection to load configuration from environment variables.
func loadFromEnv(cfg *Config) error {
	var errs []error
	v := reflect.ValueOf(cfg).Elem()
	visitConfigFields(v, func(field reflect.Value, fieldType reflect.StructField) {
		envTag := fieldType.Tag.Get("env")
		if envTag == "" {
			return
		}

		envValue := os.Getenv(envTag)
		var fromFile bool
		if hasOption(fieldType, "file") {
			envValue, _, fromFile = utils.LookupEnvOrFile(envTag)
		}
		if !fromFile {
			envValue = kit.TrimQuotes(envValue)
			if envValue == "" {
				envValue = fieldType.Tag.Get("default")
			}
		}

		if err := setFieldValue(field, envValue); err != nil {
			errs = append(errs, fmt.Errorf("invalid environment variable %s: %w", envTag, err))
		}
	})
	return errors.Join(errs...)
}

// hasOption reports whether the field's `options` tag lists option.
func hasOption(fieldType reflect.StructField, option string) bool {
	return slices.Contains(strings.Split(fieldType.Tag.Get("options"), ","), option)
}

// applyOptions processes special options for Config fields after initial load.
func applyOptions(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	visitConfigFields(v, func(field reflect.Value, fieldType reflect.StructField) {
		optionsTag := fieldType.Tag.Get("options")
		if optionsTag == "" {
			return
		}

		options := strings.SplitSeq(optionsTag, ",")
		for option := range options {
			switch strings.TrimSpace(option) {
			case "toLower":
				if field.Kind() == reflect.String {
					field.SetString(strings.ToLower(field.String()))
				}
			case "trimTrailingSlash":
				if field.Kind() == reflect.String {
					field.SetString(strings.TrimRight(field.String(), "/"))
				}
			}
		}
	})
}

func visitConfigFields(v reflect.Value, fn func(reflect.Value, reflect.StructField)) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()
	for i := range v.NumField() {
		field := v.Field(i)
		fieldType := t.Field(i)

		if fieldType.Anonymous {
			if field.Kind() == reflect.Struct {
				visitConfigFields(field, fn)
				continue
			}
			if field.Kind() == reflect.Pointer && field.Type().Elem().Kind() == reflect.Struct {
				if field.IsNil() {
					if field.CanSet() {
						field.Set(reflect.New(field.Type().Elem()))
					} else {
						continue
					}
				}
				visitConfigFields(field.Elem(), fn)
				continue
			}
		}

		fn(field, fieldType)
	}
}

// setFieldValue sets a reflect.Value from a string based on the field's type.
func setFieldValue(field reflect.Value, value string) error {
	if !field.CanSet() {
		return nil
	}

	switch {
	case field.Kind() == reflect.String:
		field.SetString(value)
	case field.Kind() == reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case field.Kind() == reflect.Uint32:
		i, err := strconv.ParseUint(value, 8, 32)
		if err != nil {
			return fmt.Errorf("expected an octal file mode: %w", err)
		}
		field.SetUint(i)
	case field.Type() == reflect.TypeFor[time.Duration]():
		d, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if d <= 0 {
			return fmt.Errorf("duration %q must be positive", value)
		}
		field.SetInt(int64(d))
	case field.Kind() == reflect.Int:
		i, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		field.SetInt(int64(i))
	case field.Type().ConvertibleTo(reflect.TypeFor[string]()):
		field.Set(reflect.ValueOf(value).Convert(field.Type()))
	}
	return nil
}

func (a AppEnvironment) IsProdEnvironment() bool {
	return a == AppEnvironmentProduction
}

func (a AppEnvironment) IsTestEnvironment() bool {
	return a == AppEnvironmentTest
}

// ListenAddr returns the effective address for the HTTP server to bind to.
func (c *Config) ListenAddr() string {
	host := strings.TrimSpace(c.Listen)
	port := cmp.Or(c.Port, "3552")
	if host == "" {
		return ":" + port
	}
	return net.JoinHostPort(host, port)
}

// GetLocation returns the timezone location for cron scheduling.
// It parses the Timezone config (TZ env var) into a *time.Location.
// Returns the system's local timezone if Timezone is "Local".
// Defaults to UTC if not set or if the timezone cannot be loaded.
func (c *Config) GetLocation() *time.Location {
	tz := strings.TrimSpace(c.Timezone)
	if tz == "" {
		return time.UTC
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		slog.WarnContext(context.Background(), "Failed to load timezone, falling back to UTC", "timezone", tz, "error", err) //nolint:forbidigo // Timezone resolution is a process-wide setting lookup.
		return time.UTC
	}
	return loc
}

// GetAppURL returns the effective application URL.
func (c *Config) GetAppURL() string {
	return cmp.Or(c.AppUrl, kit.Ternary(c.AgentMode, httpx.ManagerBaseURL(c.ManagerApiUrl), ""), "http://localhost:"+c.Port)
}
