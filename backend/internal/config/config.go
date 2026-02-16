package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
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
// Available options: file, toLower, trimTrailingSlash
type Config struct {
	AppUrl        string         `env:"APP_URL" default:"http://localhost:3552"`
	DatabaseURL   string         `env:"DATABASE_URL" default:"file:data/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2500)&_txlock=immediate" options:"file"`
	Port          string         `env:"PORT" default:"3552"`
	Listen        string         `env:"LISTEN" default:""`
	Environment   AppEnvironment `env:"ENVIRONMENT" default:"production"`
	JWTSecret     string         `env:"JWT_SECRET" default:"default-jwt-secret-change-me" options:"file"`
	EncryptionKey string         `env:"ENCRYPTION_KEY" default:"arcane-dev-key-32-characters!!!" options:"file"`

	OidcEnabled                bool   `env:"OIDC_ENABLED" default:"false"`
	OidcClientID               string `env:"OIDC_CLIENT_ID" default:"" options:"file"`
	OidcClientSecret           string `env:"OIDC_CLIENT_SECRET" default:"" options:"file"`
	OidcIssuerURL              string `env:"OIDC_ISSUER_URL" default:""`
	OidcScopes                 string `env:"OIDC_SCOPES" default:"openid email profile"`
	OidcAdminClaim             string `env:"OIDC_ADMIN_CLAIM" default:""`
	OidcAdminValue             string `env:"OIDC_ADMIN_VALUE" default:""`
	OidcSkipTlsVerify          bool   `env:"OIDC_SKIP_TLS_VERIFY" default:"false"`
	OidcAutoRedirectToProvider bool   `env:"OIDC_AUTO_REDIRECT_TO_PROVIDER" default:"false"`
	OidcProviderName           string `env:"OIDC_PROVIDER_NAME" default:""`
	OidcProviderLogoUrl        string `env:"OIDC_PROVIDER_LOGO_URL" default:""`

	DockerHost              string `env:"DOCKER_HOST" default:"unix:///var/run/docker.sock"`
	ProjectsDirectory       string `env:"PROJECTS_DIRECTORY" default:"/app/data/projects"`
	LogJson                 bool   `env:"LOG_JSON" default:"false"`
	LogLevel                string `env:"LOG_LEVEL" default:"info" options:"toLower"`
	AgentMode               bool   `env:"AGENT_MODE" default:"false"`
	AgentToken              string `env:"AGENT_TOKEN" default:"" options:"file"`
	ManagerApiUrl           string `env:"MANAGER_API_URL" default:""`
	UpdateCheckDisabled     bool   `env:"UPDATE_CHECK_DISABLED" default:"false"`
	UIConfigurationDisabled bool   `env:"UI_CONFIGURATION_DISABLED" default:"false"`
	AnalyticsDisabled       bool   `env:"ANALYTICS_DISABLED" default:"false"`
	GPUMonitoringEnabled    bool   `env:"GPU_MONITORING_ENABLED" default:"false"`
	GPUType                 string `env:"GPU_TYPE" default:"auto"`
	EdgeAgent               bool   `env:"EDGE_AGENT" default:"false"`
	EdgeReconnectInterval   int    `env:"EDGE_RECONNECT_INTERVAL" default:"5"` // seconds

	FilePerm   os.FileMode `env:"FILE_PERM" default:"0644"`
	DirPerm    os.FileMode `env:"DIR_PERM" default:"0755"`
	GitWorkDir string      `env:"GIT_WORK_DIR" default:"data/git"`

	DockerAPITimeout       int    `env:"DOCKER_API_TIMEOUT" default:"0"`
	DockerImagePullTimeout int    `env:"DOCKER_IMAGE_PULL_TIMEOUT" default:"0"`
	TrivyScanTimeout       int    `env:"TRIVY_SCAN_TIMEOUT" default:"0"`
	GitOperationTimeout    int    `env:"GIT_OPERATION_TIMEOUT" default:"0"`
	HTTPClientTimeout      int    `env:"HTTP_CLIENT_TIMEOUT" default:"0"`
	RegistryTimeout        int    `env:"REGISTRY_TIMEOUT" default:"0"`
	ProxyRequestTimeout    int    `env:"PROXY_REQUEST_TIMEOUT" default:"0"`
	BackupVolumeName       string `env:"ARCANE_BACKUP_VOLUME_NAME" default:"arcane-backups"`

	// Timezone for cron job scheduling. Uses IANA timezone names (e.g., "America/New_York", "Europe/London").
	// "Local" uses the system's local timezone, "UTC" for Coordinated Universal Time.
	Timezone string `env:"TZ" default:"Local"`

	// BuildablesConfig contains feature-specific configuration that can be conditionally compiled
	BuildablesConfig
}

func Load() *Config {
	cfg := &Config{}
	loadFromEnv(cfg)
	applyOptions(cfg)
	applyAgentModeDefaults(cfg)

	// Set global file permissions
	common.FilePerm = cfg.FilePerm
	common.DirPerm = cfg.DirPerm

	return cfg
}

func applyAgentModeDefaults(cfg *Config) {
	if cfg.EdgeAgent {
		cfg.AgentMode = true
	}
}

// loadFromEnv uses reflection to load configuration from environment variables.
func loadFromEnv(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	visitConfigFields(v, func(field reflect.Value, fieldType reflect.StructField) {
		envTag := fieldType.Tag.Get("env")
		if envTag == "" {
			return
		}

		defaultValue := fieldType.Tag.Get("default")

		// Get the environment value directly first
		envValue := trimQuotes(os.Getenv(envTag))
		if envValue == "" {
			envValue = defaultValue
		}

		setFieldValue(field, envValue)
	})
}

// applyOptions processes special options for Config fields after initial load.
func applyOptions(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	visitConfigFields(v, func(field reflect.Value, fieldType reflect.StructField) {
		optionsTag := fieldType.Tag.Get("options")
		if optionsTag == "" {
			return
		}

		options := strings.Split(optionsTag, ",")
		for _, option := range options {
			switch strings.TrimSpace(option) {
			case "file":
				resolveFileBasedEnvVariable(field, fieldType)
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
	for i := 0; i < v.NumField(); i++ {
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

// resolveFileBasedEnvVariable checks if an environment variable with the suffix "_FILE" is set,
// reads the content of the file specified by that variable, and sets the corresponding field's value.
func resolveFileBasedEnvVariable(field reflect.Value, fieldType reflect.StructField) {
	// Only process string and []byte fields
	isString := field.Kind() == reflect.String
	isByteSlice := field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.Uint8
	if !isString && !isByteSlice {
		return
	}

	// Only process fields with the "env" tag
	envTag := fieldType.Tag.Get("env")
	if envTag == "" {
		return
	}

	// Check both double underscore (__FILE) and single underscore (_FILE) variants
	// Double underscore takes precedence
	var filePath string
	for _, suffix := range []string{"__FILE", "_FILE"} {
		if fp := os.Getenv(envTag + suffix); fp != "" {
			filePath = fp
			break
		}
	}

	if filePath == "" {
		return
	}

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		slog.Warn("Failed to read secret from file, falling back to direct env var",
			"env_var", envTag+"_FILE", "file_path", filePath, "error", err)
		return
	}

	// Log when file value overrides a direct env var
	if os.Getenv(envTag) != "" {
		slog.Debug("Using secret from file, overriding direct env var", "env_var", envTag, "file_path", filePath)
	}

	if isString {
		field.SetString(strings.TrimSpace(string(fileContent)))
	} else {
		field.SetBytes(fileContent)
	}
}

// setFieldValue sets a reflect.Value from a string based on the field's type.
func setFieldValue(field reflect.Value, value string) {
	if !field.CanSet() {
		return
	}

	if field.Kind() == reflect.String {
		field.SetString(value)
		return
	}

	if field.Kind() == reflect.Bool {
		if b, err := strconv.ParseBool(value); err == nil {
			field.SetBool(b)
		}
		return
	}

	if field.Kind() == reflect.Uint32 {
		// Handle os.FileMode (which is uint32)
		if i, err := strconv.ParseUint(value, 8, 32); err == nil {
			field.SetUint(i)
		}
		return
	}

	if field.Kind() == reflect.Int {
		if i, err := strconv.Atoi(value); err == nil {
			field.SetInt(int64(i))
		}
		return
	}

	// Handle custom types based on underlying kind
	if field.Type().ConvertibleTo(reflect.TypeFor[string]()) {
		// String-based types like AppEnvironment
		field.Set(reflect.ValueOf(value).Convert(field.Type()))
	} else if field.Type() == reflect.TypeFor[os.FileMode]() {
		// os.FileMode
		if i, err := strconv.ParseUint(value, 8, 32); err == nil {
			field.Set(reflect.ValueOf(os.FileMode(i)))
		}
	}
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func (a AppEnvironment) IsProdEnvironment() bool {
	return a == AppEnvironmentProduction
}

func (a AppEnvironment) IsTestEnvironment() bool {
	return a == AppEnvironmentTest
}

// ListenAddr returns the effective address for the HTTP server to bind to.
// It uses LISTEN as the host (if set) and PORT for the port.
func (c *Config) ListenAddr() string {
	host := strings.TrimSpace(c.Listen)
	port := c.Port
	if port == "" {
		port = "3552"
	}
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
		slog.Warn("Failed to load timezone, falling back to UTC", "timezone", tz, "error", err)
		return time.UTC
	}
	return loc
}

// GetManagerBaseURL returns the base URL of the manager application.
// It strips any trailing slashes or /api suffix from MANAGER_API_URL.
func (c *Config) GetManagerBaseURL() string {
	if c.ManagerApiUrl == "" {
		return ""
	}
	managerURL := strings.TrimRight(c.ManagerApiUrl, "/")
	managerURL = strings.TrimSuffix(managerURL, "/api")
	return managerURL
}

// GetAppURL returns the effective application URL.
// If in agent mode and APP_URL is not explicitly set, it returns the manager's URL.
func (c *Config) GetAppURL() string {
	// If APP_URL is explicitly set to something other than the default, use it
	if os.Getenv("APP_URL") != "" {
		return c.AppUrl
	}

	// If in agent mode and we have a manager URL, use the manager URL
	if c.AgentMode {
		if managerBase := c.GetManagerBaseURL(); managerBase != "" {
			return managerBase
		}
	}

	return c.AppUrl
}

// MaskSensitive returns a copy of the config with sensitive fields masked.
// Useful for logging configuration without exposing secrets.
func (c *Config) MaskSensitive() map[string]any {
	result := make(map[string]any)
	v := reflect.ValueOf(c).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		envTag := fieldType.Tag.Get("env")
		if envTag == "" {
			envTag = fieldType.Name
		}

		// Fields with "file" option are considered sensitive
		optionsTag := fieldType.Tag.Get("options")
		isSensitive := strings.Contains(optionsTag, "file")

		if isSensitive {
			// Mask sensitive values
			strVal := fmt.Sprintf("%v", field.Interface())
			if len(strVal) > 0 {
				result[envTag] = "****"
			} else {
				result[envTag] = "(empty)"
			}
		} else {
			result[envTag] = field.Interface()
		}
	}

	return result
}
