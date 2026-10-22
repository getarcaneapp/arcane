package schema

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

func TestGenerate_EnvConfigIncludesTaggedFields(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	assert.Equal(t, expectedEnvConfigVars, envNames(doc.EnvConfig))

	entries := mapEnvEntries(doc.EnvConfig)

	appURL, ok := entries["ARCANE_APP_URL"]
	require.True(t, ok)
	assert.Equal(t, "AppUrl", appURL.Field)
	assert.Equal(t, "string", appURL.Type)
	assert.Empty(t, appURL.DefaultValue)
	assert.Equal(t, "config.Config", appURL.Source)
	assert.Equal(t, sourceFileConfig, appURL.SourceFile)

	encryptionKey, ok := entries["ARCANE_ENCRYPTION_KEY"]
	require.True(t, ok)
	assert.True(t, encryptionKey.SupportsFile)
	assert.Contains(t, encryptionKey.Options, "file")

	dockerConfig, ok := entries["DOCKER_CONFIG"]
	require.True(t, ok)
	assert.Equal(t, "DockerConfig", dockerConfig.Field)
	assert.Equal(t, "string", dockerConfig.Type)
	assert.Empty(t, dockerConfig.DefaultValue)
	assert.Equal(t, "config.Config", dockerConfig.Source)

	buildableUsername, ok := entries["ARCANE_AUTO_LOGIN_USERNAME"]
	require.True(t, ok)
	assert.True(t, buildableUsername.Conditional)
	assert.Equal(t, []string{"buildables"}, buildableUsername.BuildTags)
	assert.Equal(t, sourceFileBuildablesConfig, buildableUsername.SourceFile)
	assert.Equal(t, "config.BuildablesConfig", buildableUsername.Source)
}

func TestGenerate_SettingsMatchModelMetadata(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	assert.Len(t, doc.Settings, countDocumentedSettings())
	assert.Equal(t, expectedSettingKeys, sortedSettingKeys(doc.Settings))

	entries := mapSettingEntries(doc.Settings)
	feature, ok := entries["featureVulnerabilityManagementEnabled"]
	require.True(t, ok)
	assert.Equal(t, "ARCANE_FEATURE_VULNERABILITY_MANAGEMENT_ENABLED", feature.Env)
	assert.Equal(t, "true", feature.Default)
	assert.Equal(t, "boolean", feature.Type)
	assert.True(t, feature.Public)

	dockerTimeout, ok := entries["dockerApiTimeout"]
	require.True(t, ok)
	assert.Equal(t, "ARCANE_DOCKER_API_TIMEOUT", dockerTimeout.Env)
	assert.Equal(t, "30", dockerTimeout.Default)
	assert.Equal(t, "integer", dockerTimeout.Type)
	assert.Equal(t, "timeouts", dockerTimeout.Category)
	assert.Equal(t, "settings.Settings", dockerTimeout.Source)

	avatarSize, ok := entries["avatarMaxUploadSizeMb"]
	require.True(t, ok)
	assert.Equal(t, new(1.0), avatarSize.Min)
	assert.Equal(t, new(50.0), avatarSize.Max)
	assert.Equal(t, "manager", avatarSize.Scope)

	gpuType, ok := entries["gpuType"]
	require.True(t, ok)
	assert.Equal(t, []string{"auto", "nvidia", "amd", "intel"}, gpuType.Values)
	assert.Equal(t, "environment", gpuType.Scope)
	assert.Nil(t, gpuType.Min)

	dockerHost, ok := entries["dockerHost"]
	require.True(t, ok)
	assert.Equal(t, "DOCKER_HOST", dockerHost.Env)

	oidcSecret, ok := entries["oidcClientSecret"]
	require.True(t, ok)
	assert.True(t, oidcSecret.Sensitive)
	assert.Equal(t, "ARCANE_OIDC_CLIENT_SECRET", oidcSecret.Env)
	assert.Empty(t, oidcSecret.Default)

	oidcMobileRedirectURIs, ok := entries["oidcMobileRedirectUris"]
	require.True(t, ok)
	assert.Equal(t, "ARCANE_OIDC_MOBILE_REDIRECT_URIS", oidcMobileRedirectURIs.Env)
	assert.Equal(t, "arcane-mobile://oidc-callback", oidcMobileRedirectURIs.Default)
	assert.Equal(t, "authentication", oidcMobileRedirectURIs.Category)

	_, hasRuntimeDerived := entries["uiConfigDisabled"]
	assert.False(t, hasRuntimeDerived)
}

func TestGenerate_RenamedAndRetiredEnv(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	assert.Equal(t, RenamedEnv(), doc.RenamedEnv)
	assert.Equal(t, "ARCANE_PORT", doc.RenamedEnv["PORT"])
	assert.Equal(t, []string{"ANALYTICS_DISABLED", "JWT_SECRET", "UPDATE_CHECK_DISABLED"}, doc.RetiredEnv)
}

func TestGenerate_EnvNamesAreUnique(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	seen := map[string]string{}
	for _, entry := range doc.EnvConfig {
		if previous, exists := seen[entry.Env]; exists && previous != entry.SourceSymbol {
			t.Errorf("%s is declared by %s and %s", entry.Env, previous, entry.SourceSymbol)
		}
		seen[entry.Env] = entry.SourceSymbol
	}
	for _, entry := range doc.Settings {
		if previous, exists := seen[entry.Env]; exists {
			t.Errorf("%s is declared by %s and %s", entry.Env, previous, entry.SourceSymbol)
		}
		seen[entry.Env] = entry.SourceSymbol
	}
}

func TestGenerate_OutputOrderingIsStable(t *testing.T) {
	doc, err := GenerateWithSourceRoot("")
	require.NoError(t, err)

	assert.Equal(t, envNames(doc.EnvConfig), sortedStrings(envNames(doc.EnvConfig)))
	assert.Equal(t, categoriesThenEnv(doc.Settings), sortedCategoryEnvPairs(doc.Settings))
}

func mapEnvEntries(entries []ConfigEntry) map[string]ConfigEntry {
	result := make(map[string]ConfigEntry, len(entries))
	for _, entry := range entries {
		result[entry.Env] = entry
	}
	return result
}

func mapSettingEntries(entries []SettingEntry) map[string]SettingEntry {
	result := make(map[string]SettingEntry, len(entries))
	for _, entry := range entries {
		result[entry.Key] = entry
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

func sortedSettingKeys(entries []SettingEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Key)
	}
	sort.Strings(result)
	return result
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func categoriesThenEnv(entries []SettingEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Category+"|"+entry.Env)
	}
	return result
}

func sortedCategoryEnvPairs(entries []SettingEntry) []string {
	copied := append([]SettingEntry(nil), entries...)
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

func countDocumentedSettings() int {
	settingsType := reflect.TypeFor[settings.Settings]()
	count := 0

	for field := range settingsType.Fields() {
		keyTag := field.Tag.Get("key")
		if keyTag == "" {
			continue
		}

		tagParts := kit.TrimNonEmpty(strings.Split(keyTag, ","))
		if len(tagParts) == 0 {
			continue
		}

		attrs := tagParts[1:]
		if !slices.Contains(attrs, "internal") {
			count++
		}
	}

	return count
}

var expectedEnvConfigVars = []string{
	"ARCANE_ACTOR_PORT",
	"ARCANE_ADMIN_STATIC_API_KEY",
	"ARCANE_AGENT_MODE",
	"ARCANE_AGENT_TOKEN",
	"ARCANE_ALLOW_CLI_MFA_RESET",
	"ARCANE_ALLOW_CLI_PASSWORD_RESET",
	"ARCANE_ALLOW_DOWNGRADE",
	"ARCANE_APNS_KEY_ALGORITHM",
	"ARCANE_APNS_RELAY_URL",
	"ARCANE_APP_URL",
	"ARCANE_AUTO_LOGIN_PASSWORD",
	"ARCANE_AUTO_LOGIN_USERNAME",
	"ARCANE_BACKUP_VOLUME_NAME",
	"ARCANE_DATABASE_URL",
	"ARCANE_DIR_PERM",
	"ARCANE_EDGE_AGENT",
	"ARCANE_EDGE_MTLS_ASSETS_DIR",
	"ARCANE_EDGE_MTLS_CA_FILE",
	"ARCANE_EDGE_MTLS_CERT_FILE",
	"ARCANE_EDGE_MTLS_KEY_FILE",
	"ARCANE_EDGE_MTLS_MODE",
	"ARCANE_EDGE_MTLS_SERVER_NAME",
	"ARCANE_EDGE_RECONNECT_INTERVAL",
	"ARCANE_EDGE_TRANSPORT",
	"ARCANE_ENCRYPTION_KEY",
	"ARCANE_ENVIRONMENT",
	"ARCANE_FILE_PERM",
	"ARCANE_GIT_WORK_DIR",
	"ARCANE_IN_CONTAINER",
	"ARCANE_JWT_REFRESH_EXPIRY",
	"ARCANE_LISTEN",
	"ARCANE_LOG_JSON",
	"ARCANE_LOG_LEVEL",
	"ARCANE_MANAGER_API_URL",
	"ARCANE_OIDC_ROLE_MAPPINGS",
	"ARCANE_PORT",
	"ARCANE_TLS_CERT_FILE",
	"ARCANE_TLS_ENABLED",
	"ARCANE_TLS_KEY_FILE",
	"ARCANE_TRUSTED_PROXIES",
	"ARCANE_UI_CONFIGURATION_DISABLED",
	"ARCANE_UPGRADE_KEEP_CONTAINER",
	"DOCKER_CONFIG",
	"HTTPS_PROXY",
	"HTTP_PROXY",
	"NO_PROXY",
	"PGID",
	"PUID",
	"TZ",
}

var expectedSettingKeys = []string{
	"activityHistoryMaxEntries",
	"activityHistoryRetentionDays",
	"allEnvironmentsLimit",
	"analyticsEnabled",
	"apnsEnabled",
	"authLocalEnabled",
	"authPasswordPolicy",
	"authSessionTimeout",
	"autoHealEnabled",
	"autoHealExcludedContainers",
	"autoHealInterval",
	"autoHealMaxRestarts",
	"autoHealRestartWindow",
	"autoInjectEnv",
	"autoUpdate",
	"autoUpdateDelay",
	"autoUpdateExcludedContainers",
	"autoUpdateInterval",
	"avatarMaxUploadSizeMb",
	"baseServerUrl",
	"buildHistoryRetentionDays",
	"buildProvider",
	"buildTimeout",
	"buildsDirectory",
	"defaultDeployPullPolicy",
	"defaultShell",
	"deployWaitTimeout",
	"depotProjectId",
	"depotToken",
	"developmentBrandingEnabled",
	"diskUsagePath",
	"dockerApiTimeout",
	"dockerClientRefreshInterval",
	"dockerHost",
	"dockerImagePullTimeout",
	"enableGravatar",
	"environmentHealthInterval",
	"eventCleanupInterval",
	"experimentalFeaturesEnabled",
	"expiredSessionsCleanupInterval",
	"featureSwarmEnabled",
	"featureVulnerabilityManagementEnabled",
	"followProjectSymlinks",
	"gitOperationTimeout",
	"gitSyncMaxBinarySizeMb",
	"gitSyncMaxFiles",
	"gitSyncMaxTotalSizeMb",
	"gpuMonitoringEnabled",
	"gpuType",
	"httpClientTimeout",
	"imageAutoPatchEnabled",
	"imageAutoPatchInterval",
	"imageEventWatcherEnabled",
	"imagePatchAllPlatforms",
	"imagePatchSuffix",
	"imagePatchTimeoutSec",
	"lifecycleDefaultRunnerImage",
	"lifecycleEnabled",
	"lifecycleMaxTimeoutSec",
	"maxConcurrentActivities",
	"maxImageUploadSize",
	"oidcAuthorizationEndpoint",
	"oidcAutoRedirectToProvider",
	"oidcClientId",
	"oidcClientSecret",
	"oidcDeviceAuthorizationEndpoint",
	"oidcEnabled",
	"oidcGroupsClaim",
	"oidcIssuerUrl",
	"oidcJwksEndpoint",
	"oidcMergeAccounts",
	"oidcMobileRedirectUris",
	"oidcProviderLogoUrl",
	"oidcProviderName",
	"oidcScopes",
	"oidcSkipTlsVerify",
	"oidcTokenEndpoint",
	"oidcUserinfoEndpoint",
	"pollingEnabled",
	"pollingInterval",
	"projectScanMaxDepth",
	"projectScanSkipDirs",
	"projectWorkspaceMaxDepth",
	"projectWorkspaceMaxEntries",
	"projectWorkspaceMaxFileSizeMb",
	"projectsDirectory",
	"proxyRequestTimeout",
	"pruneBuildCacheMode",
	"pruneBuildCacheUntil",
	"pruneContainerMode",
	"pruneContainerUntil",
	"pruneImageMode",
	"pruneImageUntil",
	"pruneNetworkMode",
	"pruneNetworkUntil",
	"pruneVolumeMode",
	"registryTagTimeout",
	"registryTimeout",
	"scheduledPruneEnabled",
	"scheduledPruneInterval",
	"swarmStackSourcesDirectory",
	"templatesDirectory",
	"toolsImageRegistry",
	"trivyConcurrentScanContainers",
	"trivyConfig",
	"trivyCpuLimit",
	"trivyDbRegistry",
	"trivyIgnore",
	"trivyIgnoreUnfixed",
	"trivyMemoryLimitMb",
	"trivyNetwork",
	"trivyPrivileged",
	"trivyResourceLimitsEnabled",
	"trivyScanTimeout",
	"trivySecurityOpts",
	"trivyServerEnabled",
	"trivyServerToken",
	"trivyServerUrl",
	"updateCheckEnabled",
	"updateCheckRegistry",
	"upgradeLogRetentionDays",
	"volumeHelperIdleTimeout",
	"volumeWorkspaceMaxDepth",
	"volumeWorkspaceMaxEntries",
	"volumeWorkspaceMaxFileSizeMb",
	"vulnerabilityScanEnabled",
	"vulnerabilityScanInterval",
	"vulnerabilityThreatIntelEnabled",
}
