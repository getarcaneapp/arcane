package configschema

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_EnvConfigIncludesTaggedFields(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	assert.Equal(t, expectedEnvConfigVars, envNames(doc.EnvConfig))

	entries := mapEnvEntries(doc.EnvConfig)

	appURL, ok := entries["APP_URL"]
	require.True(t, ok)
	assert.Equal(t, "AppUrl", appURL.Field)
	assert.Equal(t, "string", appURL.Type)
	assert.Equal(t, "http://localhost:3552", appURL.DefaultValue)
	assert.Equal(t, "config.Config", appURL.Source)
	assert.Equal(t, sourceFileConfig, appURL.SourceFile)

	encryptionKey, ok := entries["ENCRYPTION_KEY"]
	require.True(t, ok)
	assert.True(t, encryptionKey.SupportsFile)
	assert.Contains(t, encryptionKey.Options, "file")

	buildableUsername, ok := entries["AUTO_LOGIN_USERNAME"]
	require.True(t, ok)
	assert.True(t, buildableUsername.Conditional)
	assert.Equal(t, []string{"buildables"}, buildableUsername.BuildTags)
	assert.Equal(t, sourceFileBuildablesConfig, buildableUsername.SourceFile)
	assert.Equal(t, "config.BuildablesConfig", buildableUsername.Source)
}

func TestGenerate_SettingEnvOverridesMatchModelMetadata(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	expectedCount := countDocumentedSettingOverrides()
	assert.Len(t, doc.SettingEnvOverrides, expectedCount)
	assert.Equal(t, expectedSettingOverrideKeys, sortedSettingKeys(doc.SettingEnvOverrides))

	entries := mapSettingOverrideEntries(doc.SettingEnvOverrides)

	dockerTimeout, ok := entries["dockerApiTimeout"]
	require.True(t, ok)
	assert.Equal(t, "DOCKER_API_TIMEOUT", dockerTimeout.Env)
	assert.Equal(t, "30", dockerTimeout.DefaultValue)
	assert.Equal(t, "number", dockerTimeout.Type)
	assert.Equal(t, "timeouts", dockerTimeout.Category)
	assert.Equal(t, "models.Settings + services.DefaultSettingsConfig", dockerTimeout.Source)

	oidcSecret, ok := entries["oidcClientSecret"]
	require.True(t, ok)
	assert.True(t, oidcSecret.Sensitive)
	assert.Equal(t, "OIDC_CLIENT_SECRET", oidcSecret.Env)

	legacyOIDC, ok := entries["authOidcConfig"]
	require.True(t, ok)
	assert.True(t, legacyOIDC.Deprecated)
	assert.NotEmpty(t, legacyOIDC.Note)
	assert.Contains(t, legacyOIDC.Requires, "AGENT_MODE=true")
	assert.Empty(t, legacyOIDC.DefaultValue)

	trivyImage, ok := entries["trivyImage"]
	require.True(t, ok)
	assert.Contains(t, trivyImage.Requires, "UI_CONFIGURATION_DISABLED=true")

	assert.Empty(t, oidcSecret.DefaultValue)

	_, hasRuntimeDerived := entries["uiConfigDisabled"]
	assert.False(t, hasRuntimeDerived)
}

func TestGenerate_DocumentIncludesEnvOnlyModeNote(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	notes := strings.Join(doc.Notes, " ")
	assert.Contains(t, notes, "AGENT_MODE=true")
	assert.Contains(t, notes, "UI_CONFIGURATION_DISABLED=true")
}

func TestGenerate_OutputOrderingIsStable(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	assert.Equal(t, envNames(doc.EnvConfig), sortedStrings(envNames(doc.EnvConfig)))
	assert.Equal(t, categoriesThenEnv(doc.SettingEnvOverrides), sortedCategoryEnvPairs(doc.SettingEnvOverrides))
}

func mapEnvEntries(entries []ConfigEntry) map[string]ConfigEntry {
	result := make(map[string]ConfigEntry, len(entries))
	for _, entry := range entries {
		result[entry.Env] = entry
	}
	return result
}

func mapSettingOverrideEntries(entries []SettingOverrideEntry) map[string]SettingOverrideEntry {
	result := make(map[string]SettingOverrideEntry, len(entries))
	for _, entry := range entries {
		result[entry.SettingKey] = entry
	}
	return result
}

func envNames(entries []ConfigEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Env)
	}
	return result
}

func sortedSettingKeys(entries []SettingOverrideEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.SettingKey)
	}
	sort.Strings(result)
	return result
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func categoriesThenEnv(entries []SettingOverrideEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Category+"|"+entry.Env)
	}
	return result
}

func sortedCategoryEnvPairs(entries []SettingOverrideEntry) []string {
	copied := append([]SettingOverrideEntry(nil), entries...)
	sort.Slice(copied, func(i, j int) bool {
		if copied[i].Category == copied[j].Category {
			return copied[i].Env < copied[j].Env
		}
		return copied[i].Category < copied[j].Category
	})

	result := make([]string, 0, len(copied))
	for _, entry := range copied {
		result = append(result, entry.Category+"|"+entry.Env)
	}
	return result
}

func countDocumentedSettingOverrides() int {
	settingsType := reflect.TypeFor[models.Settings]()
	count := 0

	for field := range settingsType.Fields() {
		keyTag := field.Tag.Get("key")
		if keyTag == "" {
			continue
		}

		tagParts := splitTagListInternal(keyTag)
		if len(tagParts) == 0 {
			continue
		}

		attrs := tagParts[1:]
		if slices.Contains(attrs, "envOverride") || !slices.Contains(attrs, "internal") {
			count++
		}
	}

	return count
}

var expectedEnvConfigVars = []string{
	"ADMIN_STATIC_API_KEY",
	"AGENT_MODE",
	"AGENT_TOKEN",
	"ALLOW_DOWNGRADE",
	"ANALYTICS_DISABLED",
	"APP_URL",
	"ARCANE_BACKUP_VOLUME_NAME",
	"AUTO_LOGIN_PASSWORD",
	"AUTO_LOGIN_USERNAME",
	"DATABASE_URL",
	"DIR_PERM",
	"DOCKER_API_TIMEOUT",
	"DOCKER_HOST",
	"DOCKER_IMAGE_PULL_TIMEOUT",
	"EDGE_AGENT",
	"EDGE_RECONNECT_INTERVAL",
	"EDGE_TRANSPORT",
	"ENCRYPTION_KEY",
	"ENVIRONMENT",
	"FILE_PERM",
	"GIT_OPERATION_TIMEOUT",
	"GIT_WORK_DIR",
	"GPU_MONITORING_ENABLED",
	"GPU_TYPE",
	"HTTP_CLIENT_TIMEOUT",
	"JWT_REFRESH_EXPIRY",
	"JWT_SECRET",
	"LISTEN",
	"LOG_JSON",
	"LOG_LEVEL",
	"MANAGER_API_URL",
	"OIDC_ADMIN_CLAIM",
	"OIDC_ADMIN_VALUE",
	"OIDC_AUTO_REDIRECT_TO_PROVIDER",
	"OIDC_CLIENT_ID",
	"OIDC_CLIENT_SECRET",
	"OIDC_ENABLED",
	"OIDC_ISSUER_URL",
	"OIDC_PROVIDER_LOGO_URL",
	"OIDC_PROVIDER_NAME",
	"OIDC_SCOPES",
	"OIDC_SKIP_TLS_VERIFY",
	"PORT",
	"PROJECTS_DIRECTORY",
	"PROJECT_SCAN_MAX_DEPTH",
	"PROJECT_SCAN_SKIP_DIRS",
	"PROXY_REQUEST_TIMEOUT",
	"REGISTRY_TIMEOUT",
	"TLS_CERT_FILE",
	"TLS_ENABLED",
	"TLS_KEY_FILE",
	"TRIVY_SCAN_TIMEOUT",
	"TZ",
	"UI_CONFIGURATION_DISABLED",
	"UPDATE_CHECK_DISABLED",
}

var expectedSettingOverrideKeys = []string{
	"accentColor",
	"applicationTheme",
	"authLocalEnabled",
	"authOidcConfig",
	"authPasswordPolicy",
	"authSessionTimeout",
	"autoHealEnabled",
	"autoHealExcludedContainers",
	"autoHealInterval",
	"autoHealMaxRestarts",
	"autoHealRestartWindow",
	"autoInjectEnv",
	"autoUpdate",
	"autoUpdateExcludedContainers",
	"autoUpdateInterval",
	"baseServerUrl",
	"buildProvider",
	"buildTimeout",
	"buildsDirectory",
	"defaultDeployPullPolicy",
	"defaultShell",
	"depotProjectId",
	"depotToken",
	"diskUsagePath",
	"dockerApiTimeout",
	"dockerHost",
	"dockerImagePullTimeout",
	"dockerPruneMode",
	"enableGravatar",
	"environmentHealthInterval",
	"eventCleanupInterval",
	"followProjectSymlinks",
	"gitOperationTimeout",
	"gitSyncMaxBinarySizeMb",
	"gitSyncMaxFiles",
	"gitSyncMaxTotalSizeMb",
	"gitopsSyncInterval",
	"httpClientTimeout",
	"keyboardShortcutsEnabled",
	"maxImageUploadSize",
	"mobileNavigationMode",
	"mobileNavigationShowLabels",
	"oidcAdminClaim",
	"oidcAdminValue",
	"oidcAuthorizationEndpoint",
	"oidcAutoRedirectToProvider",
	"oidcClientId",
	"oidcClientSecret",
	"oidcDeviceAuthorizationEndpoint",
	"oidcEnabled",
	"oidcIssuerUrl",
	"oidcJwksEndpoint",
	"oidcMergeAccounts",
	"oidcProviderLogoUrl",
	"oidcProviderName",
	"oidcScopes",
	"oidcSkipTlsVerify",
	"oidcTokenEndpoint",
	"oidcUserinfoEndpoint",
	"oledMode",
	"pollingEnabled",
	"pollingInterval",
	"projectsDirectory",
	"proxyRequestTimeout",
	"registryTimeout",
	"scheduledPruneBuildCache",
	"scheduledPruneContainers",
	"scheduledPruneEnabled",
	"scheduledPruneImages",
	"scheduledPruneInterval",
	"scheduledPruneNetworks",
	"scheduledPruneVolumes",
	"sidebarHoverExpansion",
	"swarmStackSourcesDirectory",
	"trivyConcurrentScanContainers",
	"trivyConfig",
	"trivyCpuLimit",
	"trivyIgnore",
	"trivyImage",
	"trivyMemoryLimitMb",
	"trivyNetwork",
	"trivyPreserveCacheOnVolumePrune",
	"trivyPrivileged",
	"trivyResourceLimitsEnabled",
	"trivyScanTimeout",
	"trivySecurityOpts",
	"vulnerabilityScanEnabled",
	"vulnerabilityScanInterval",
}
