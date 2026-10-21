package types

// NotificationProvider is the type for notification provider identifiers.
type NotificationProvider string

const (
	// NotificationProviderDiscord is the builtin Discord notification provider.
	NotificationProviderDiscord NotificationProvider = "discord"

	// NotificationProviderEmail is the builtin Email notification provider.
	NotificationProviderEmail NotificationProvider = "email"

	// NotificationProviderTelegram is the builtin Telegram notification provider.
	NotificationProviderTelegram NotificationProvider = "telegram"

	// NotificationProviderSignal is the builtin Signal notification provider.
	NotificationProviderSignal NotificationProvider = "signal"

	// NotificationProviderSlack is the builtin Slack notification provider.
	NotificationProviderSlack NotificationProvider = "slack"

	// NotificationProviderNtfy is the builtin Ntfy notification provider.
	NotificationProviderNtfy NotificationProvider = "ntfy"

	// NotificationProviderPushover is the builtin Pushover notification provider.
	NotificationProviderPushover NotificationProvider = "pushover"

	// NotificationProviderGotify is the builtin Gotify notification provider.
	NotificationProviderGotify NotificationProvider = "gotify"

	// NotificationProviderMatrix is the builtin Matrix webhook notification provider.
	NotificationProviderMatrix NotificationProvider = "matrix"

	// NotificationProviderGoogleChat is the builtin Google Chat notification provider.
	NotificationProviderGoogleChat NotificationProvider = "googlechat"

	// NotificationProviderGeneric is the builtin Generic webhook notification provider.
	NotificationProviderGeneric NotificationProvider = "generic"

	NotificationDispatchKindImageUpdate          NotificationDispatchKind = "image_update"
	NotificationDispatchKindBatchImageUpdate     NotificationDispatchKind = "batch_image_update"
	NotificationDispatchKindContainerUpdate      NotificationDispatchKind = "container_update"
	NotificationDispatchKindBatchContainerUpdate NotificationDispatchKind = "batch_container_update"
	NotificationDispatchKindVulnerabilityFound   NotificationDispatchKind = "vulnerability_found"
	NotificationDispatchKindPruneReport          NotificationDispatchKind = "prune_report"
	NotificationDispatchKindAutoHeal             NotificationDispatchKind = "auto_heal"
)

type NotificationSettings struct {
	// ID is the unique identifier of the notification settings.
	//
	// Required: true
	ID uint `json:"id" readOnly:"true"`

	// Provider is the notification provider type.
	//
	// Required: true
	Provider NotificationProvider `json:"provider"`

	// Enabled indicates if the notification provider is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// Config contains the provider-specific configuration.
	//
	// Required: true
	Config JsonObject `json:"config"`
}

// NotificationTestResponse is the result of a test notification send.
type NotificationTestResponse struct {
	// Message describes the test outcome.
	Message string `json:"message"`

	// Warning explains why a real notification of the tested kind would not
	// send (provider disabled or event unsubscribed). Empty when none applies.
	Warning string `json:"warning,omitempty"`
}

type NotificationDispatchKind string

type NotificationDispatchImageUpdate struct {
	ImageRef   string              `json:"imageRef"`
	UpdateInfo ImageUpdateResponse `json:"updateInfo"`
}

type NotificationDispatchBatchImageUpdate struct {
	Updates map[string]*ImageUpdateResponse `json:"updates"`
}

type NotificationDispatchContainerUpdate struct {
	ContainerName string `json:"containerName"`
	ImageRef      string `json:"imageRef"`
	OldDigest     string `json:"oldDigest,omitempty"`
	NewDigest     string `json:"newDigest,omitempty"`
}

type NotificationDispatchBatchContainerUpdate struct {
	Updates []NotificationDispatchContainerUpdate `json:"updates"`
}

type NotificationDispatchVulnerabilityFound struct {
	CVEID            string `json:"cveId"`
	CVELink          string `json:"cveLink"`
	Severity         string `json:"severity"`
	ImageName        string `json:"imageName"`
	FixedVersion     string `json:"fixedVersion,omitempty"`
	PkgName          string `json:"pkgName,omitempty"`
	InstalledVersion string `json:"installedVersion,omitempty"`
}

type NotificationDispatchPruneReport struct {
	Result PruneAllResult `json:"result"`
}

type NotificationDispatchAutoHeal struct {
	ContainerName string `json:"containerName"`
	ContainerID   string `json:"containerId"`
}

type NotificationDispatchRequest struct {
	Kind                 NotificationDispatchKind                  `json:"kind"`
	ImageUpdate          *NotificationDispatchImageUpdate          `json:"imageUpdate,omitempty"`
	BatchImageUpdate     *NotificationDispatchBatchImageUpdate     `json:"batchImageUpdate,omitempty"`
	ContainerUpdate      *NotificationDispatchContainerUpdate      `json:"containerUpdate,omitempty"`
	BatchContainerUpdate *NotificationDispatchBatchContainerUpdate `json:"batchContainerUpdate,omitempty"`
	VulnerabilityFound   *NotificationDispatchVulnerabilityFound   `json:"vulnerabilityFound,omitempty"`
	PruneReport          *NotificationDispatchPruneReport          `json:"pruneReport,omitempty"`
	AutoHeal             *NotificationDispatchAutoHeal             `json:"autoHeal,omitempty"`
}

type NotificationDispatchResponse struct {
	Message   string `json:"message"`
	Delivered int    `json:"delivered"`
}
