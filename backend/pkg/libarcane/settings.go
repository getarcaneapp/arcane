package libarcane

import (
	"slices"
	"strings"

	"github.com/robfig/cron/v3"
)

const DepotTokenSettingKey = "depotToken"

const (
	GHCRRegistryHost      = "ghcr.io"
	DockerHubRegistryHost = "docker.io"
)

// ArcaneRegistryHost normalizes a registry setting to a host that mirrors the getarcaneapp images, defaulting to GHCR.
func ArcaneRegistryHost(registry string) string {
	if strings.TrimSpace(registry) == DockerHubRegistryHost {
		return DockerHubRegistryHost
	}
	return GHCRRegistryHost
}

type SettingUpdate struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var timeoutSettingKeys = []string{
	"dockerApiTimeout",
	"dockerImagePullTimeout",
	"trivyScanTimeout",
	"gitOperationTimeout",
	"httpClientTimeout",
	"registryTimeout",
	"proxyRequestTimeout",
	"deployWaitTimeout",
	"trivyResourceLimitsEnabled",
	"trivyCpuLimit",
	"trivyMemoryLimitMb",
	"trivyConcurrentScanContainers",
	"buildProvider",
	"buildsDirectory",
	"buildTimeout",
	"depotProjectId",
	DepotTokenSettingKey,
}

var cronSettingKeys = []string{
	"scheduledPruneInterval",
	"autoUpdateInterval",
	"pollingInterval",
	"environmentHealthInterval",
	"eventCleanupInterval",
	"expiredSessionsCleanupInterval",
	"vulnerabilityScanInterval",
}

var cronParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// TimeoutSettingKeys returns the settings propagated to remote environments.
func TimeoutSettingKeys() []string {
	return slices.Clone(timeoutSettingKeys)
}

func IsCronSettingKey(key string) bool {
	return slices.Contains(cronSettingKeys, key)
}

func ValidateCronSetting(key, value string) error {
	if value == "" || !IsCronSettingKey(key) {
		return nil
	}
	_, err := cronParser.Parse(value)
	return err
}
