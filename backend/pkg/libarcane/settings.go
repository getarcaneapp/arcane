package libarcane

import (
	"slices"

	"github.com/robfig/cron/v3"
)

const DepotTokenSettingKey = "depotToken"

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
	"analyticsHeartbeatInterval",
	"vulnerabilityScanInterval",
}

var cronParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func IsTimeoutSettingKey(key string) bool {
	return slices.Contains(timeoutSettingKeys, key)
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
