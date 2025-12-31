package config

import (
	"os"
	"strconv"
	"strings"
)

type AppEnvironment string

const (
	AppEnvironmentProduction  AppEnvironment = "production"
	AppEnvironmentDevelopment AppEnvironment = "development"
	AppEnvironmentTest        AppEnvironment = "test"
	defaultSqliteString       string         = "file:data/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2500)&_txlock=immediate"
)

type Config struct {
	AppUrl        string
	DatabaseURL   string
	Port          string
	Environment   AppEnvironment
	JWTSecret     string
	EncryptionKey string

	OidcEnabled       bool
	OidcClientID      string
	OidcClientSecret  string
	OidcIssuerURL     string
	OidcScopes        string
	OidcAdminClaim    string
	OidcAdminValue    string
	OidcSkipTlsVerify bool

	DockerHost              string
	LogJson                 bool
	LogLevel                string
	AgentMode               bool
	AgentToken              string
	ManagerApiUrl           string
	UpdateCheckDisabled     bool
	UIConfigurationDisabled bool
	AnalyticsDisabled       bool
	GPUMonitoringEnabled    bool
	GPUType                 string
}

func Load() *Config {
	return &Config{
		AppUrl:        getEnvOrDefault("APP_URL", "http://localhost:3552"),
		DatabaseURL:   getEnvOrDefault("DATABASE_URL", defaultSqliteString),
		Port:          getEnvOrDefault("PORT", "3552"),
		Environment:   getEnvOrDefault("ENVIRONMENT", AppEnvironmentProduction),
		JWTSecret:     getEnvOrDefault("JWT_SECRET", "default-jwt-secret-change-me"),
		EncryptionKey: getEnvOrDefault("ENCRYPTION_KEY", "arcane-dev-key-32-characters!!!"),

		OidcEnabled:       getBoolEnvOrDefault("OIDC_ENABLED", false),
		OidcClientID:      getEnvOrDefault("OIDC_CLIENT_ID", ""),
		OidcClientSecret:  getEnvOrDefault("OIDC_CLIENT_SECRET", ""),
		OidcIssuerURL:     getEnvOrDefault("OIDC_ISSUER_URL", ""),
		OidcScopes:        getEnvOrDefault("OIDC_SCOPES", "openid email profile"),
		OidcAdminClaim:    getEnvOrDefault("OIDC_ADMIN_CLAIM", ""),
		OidcAdminValue:    getEnvOrDefault("OIDC_ADMIN_VALUE", ""),
		OidcSkipTlsVerify: getBoolEnvOrDefault("OIDC_SKIP_TLS_VERIFY", false),

		DockerHost:              getEnvOrDefault("DOCKER_HOST", "unix:///var/run/docker.sock"),
		LogJson:                 getBoolEnvOrDefault("LOG_JSON", false),
		LogLevel:                strings.ToLower(getEnvOrDefault("LOG_LEVEL", "info")),
		AgentMode:               getBoolEnvOrDefault("AGENT_MODE", false),
		AgentToken:              os.Getenv("AGENT_TOKEN"),
		ManagerApiUrl:           os.Getenv("MANAGER_API_URL"),
		UpdateCheckDisabled:     getBoolEnvOrDefault("UPDATE_CHECK_DISABLED", false),
		UIConfigurationDisabled: getBoolEnvOrDefault("UI_CONFIGURATION_DISABLED", false),
		AnalyticsDisabled:       getBoolEnvOrDefault("ANALYTICS_DISABLED", false),
		GPUMonitoringEnabled:    getBoolEnvOrDefault("GPU_MONITORING_ENABLED", false),
		GPUType:                 getEnvOrDefault("GPU_TYPE", "auto"),
	}
}

func getEnvOrDefault[T interface{ ~string }](key string, defaultValue T) T {
	if value := os.Getenv(key); value != "" {
		return T(trimQuotes(value))
	}
	return defaultValue
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

func getBoolEnvOrDefault(key string, defaultValue bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		v = trimQuotes(v)
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}
