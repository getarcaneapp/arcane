package notifications

type NotificationProvider string

const (
	NotificationProviderDiscord    NotificationProvider = "discord"
	NotificationProviderEmail      NotificationProvider = "email"
	NotificationProviderTelegram   NotificationProvider = "telegram"
	NotificationProviderSignal     NotificationProvider = "signal"
	NotificationProviderSlack      NotificationProvider = "slack"
	NotificationProviderNtfy       NotificationProvider = "ntfy"
	NotificationProviderPushover   NotificationProvider = "pushover"
	NotificationProviderGotify     NotificationProvider = "gotify"
	NotificationProviderMatrix     NotificationProvider = "matrix"
	NotificationProviderGoogleChat NotificationProvider = "googlechat"
	NotificationProviderGeneric    NotificationProvider = "generic"

	NotificationEventImageUpdate        NotificationEventType = "image_update"
	NotificationEventContainerUpdate    NotificationEventType = "container_update"
	NotificationEventVulnerabilityFound NotificationEventType = "vulnerability_found"
	NotificationEventPruneReport        NotificationEventType = "prune_report"
	NotificationEventAutoHeal           NotificationEventType = "auto_heal"

	EmailTLSModeNone     EmailTLSMode = "none"
	EmailTLSModeStartTLS EmailTLSMode = "starttls"
	EmailTLSModeSSL      EmailTLSMode = "ssl"

	EmailAuthModeNone    EmailAuthMode = "none"
	EmailAuthModeAuto    EmailAuthMode = "auto"
	EmailAuthModePlain   EmailAuthMode = "plain"
	EmailAuthModeLogin   EmailAuthMode = "login"
	EmailAuthModeCRAMMD5 EmailAuthMode = "crammd5"
)

var validNotificationProviders = map[NotificationProvider]struct{}{
	NotificationProviderDiscord:    {},
	NotificationProviderEmail:      {},
	NotificationProviderTelegram:   {},
	NotificationProviderSignal:     {},
	NotificationProviderSlack:      {},
	NotificationProviderNtfy:       {},
	NotificationProviderPushover:   {},
	NotificationProviderGotify:     {},
	NotificationProviderMatrix:     {},
	NotificationProviderGoogleChat: {},
	NotificationProviderGeneric:    {},
}

func IsValidNotificationProvider(provider NotificationProvider) bool {
	_, ok := validNotificationProviders[provider]
	return ok
}

type NotificationEventType string

var AllNotificationEventTypes = []NotificationEventType{
	NotificationEventImageUpdate,
	NotificationEventContainerUpdate,
	NotificationEventVulnerabilityFound,
	NotificationEventPruneReport,
	NotificationEventAutoHeal,
}

type EmailTLSMode string

type EmailAuthMode string

type DiscordConfig struct {
	Events    map[NotificationEventType]bool `json:"events,omitempty"`
	WebhookID string                         `json:"webhookId"`
	Token     string                         `json:"token"`
	Username  string                         `json:"username,omitempty"`
	AvatarURL string                         `json:"avatarUrl,omitempty"`
}

type EmailConfig struct {
	Events       map[NotificationEventType]bool `json:"events,omitempty"`
	SMTPHost     string                         `json:"smtpHost"`
	SMTPUsername string                         `json:"smtpUsername"`
	SMTPPassword string                         `json:"smtpPassword"`
	FromAddress  string                         `json:"fromAddress"`
	TLSMode      EmailTLSMode                   `json:"tlsMode"`
	AuthMode     EmailAuthMode                  `json:"authMode,omitempty"`
	ToAddresses  []string                       `json:"toAddresses"`
	SMTPPort     int                            `json:"smtpPort"`
}

type TelegramConfig struct {
	Events       map[NotificationEventType]bool `json:"events,omitempty"`
	BotToken     string                         `json:"botToken"`
	ParseMode    string                         `json:"parseMode,omitempty"`
	Title        string                         `json:"title,omitempty"`
	ChatIDs      []string                       `json:"chatIds"`
	Preview      bool                           `json:"preview"`
	Notification bool                           `json:"notification"`
}

type SignalConfig struct {
	NotifySelf *bool                          `json:"notifySelf,omitempty"`
	Events     map[NotificationEventType]bool `json:"events,omitempty"`
	Host       string                         `json:"host"`
	User       string                         `json:"user,omitempty"`
	Password   string                         `json:"password,omitempty"`
	Token      string                         `json:"token,omitempty"`
	Source     string                         `json:"source"`
	Recipients []string                       `json:"recipients"`
	Port       int                            `json:"port"`
	DisableTLS bool                           `json:"disableTls"`
}

type SlackConfig struct {
	Events   map[NotificationEventType]bool `json:"events,omitempty"`
	Token    string                         `json:"token"`
	BotName  string                         `json:"botName,omitempty"`
	Icon     string                         `json:"icon,omitempty"`
	Color    string                         `json:"color,omitempty"`
	Title    string                         `json:"title,omitempty"`
	Channel  string                         `json:"channel,omitempty"`
	ThreadTS string                         `json:"threadTs,omitempty"`
}

type NtfyConfig struct {
	Events                 map[NotificationEventType]bool `json:"events,omitempty"`
	Host                   string                         `json:"host"`
	Topic                  string                         `json:"topic"`
	Username               string                         `json:"username,omitempty"`
	Password               string                         `json:"password,omitempty"`
	Title                  string                         `json:"title,omitempty"`
	Priority               string                         `json:"priority,omitempty"`
	Icon                   string                         `json:"icon,omitempty"`
	Tags                   []string                       `json:"tags,omitempty"`
	Port                   int                            `json:"port"`
	Cache                  bool                           `json:"cache"`
	Firebase               bool                           `json:"firebase"`
	DisableTLS             bool                           `json:"disableTls"`
	DisableTLSVerification bool                           `json:"disableTlsVerification"`
}

type PushoverConfig struct {
	Events   map[NotificationEventType]bool `json:"events,omitempty"`
	Token    string                         `json:"token"`
	User     string                         `json:"user"`
	Title    string                         `json:"title,omitempty"`
	Devices  []string                       `json:"devices,omitempty"`
	Priority int8                           `json:"priority"`
}

type GotifyConfig struct {
	Events             map[NotificationEventType]bool `json:"events,omitempty"`
	Host               string                         `json:"host"`
	Token              string                         `json:"token"`
	Path               string                         `json:"path,omitempty"`
	Title              string                         `json:"title,omitempty"`
	Port               int                            `json:"port,omitempty"`
	Priority           int                            `json:"priority,omitempty"`
	DisableTLS         bool                           `json:"disableTls"`
	InsecureSkipVerify bool                           `json:"insecureSkipVerify"`
	UseHeader          bool                           `json:"useHeader"`
}

type GoogleChatConfig struct {
	Events map[NotificationEventType]bool `json:"events,omitempty"`
	// WebhookURL is the full incoming webhook URL copied from Google Chat,
	// e.g. https://chat.googleapis.com/v1/spaces/SPACE/messages?key=KEY&token=TOKEN.
	WebhookURL string `json:"webhookUrl"`
}

type MatrixConfig struct {
	Events                 map[NotificationEventType]bool `json:"events,omitempty"`
	Host                   string                         `json:"host"`
	Rooms                  string                         `json:"rooms"`
	Username               string                         `json:"username,omitempty"`
	Password               string                         `json:"password,omitempty"`
	Port                   int                            `json:"port,omitempty"`
	DisableTLSVerification bool                           `json:"disableTlsVerification"`
}

type GenericConfig struct {
	CustomHeaders map[string]string              `json:"customHeaders,omitempty"`
	Events        map[NotificationEventType]bool `json:"events,omitempty"`
	WebhookURL    string                         `json:"webhookUrl"`
	Method        string                         `json:"method,omitempty"`
	ContentType   string                         `json:"contentType,omitempty"`
	TitleKey      string                         `json:"titleKey,omitempty"`
	MessageKey    string                         `json:"messageKey,omitempty"`
	// SuccessBodyContains is an optional substring that must appear in the
	// response body for the send to be considered successful. Useful for
	// providers that always return HTTP 200 but embed a status indicator in
	// the JSON body (e.g. PushPlus returns {"code":200,...} on success and
	// {"code":900,...} on failure). When empty, only the HTTP status code is
	// checked (existing behaviour).
	SuccessBodyContains string `json:"successBodyContains,omitempty"`
	// PayloadTemplate is an optional Go text/template rendered into the request
	// body, letting users target endpoints that require a nested or
	// provider-specific payload shape rather than the flat
	// {"title":...,"message":...} object. The template receives the title and
	// message (under the configured TitleKey/MessageKey) plus event variables
	// (environment, environmentId, event, timestamp), all pre-escaped for JSON
	// string contexts. When empty, the flat JSON payload is used (existing
	// behaviour).
	PayloadTemplate string `json:"payloadTemplate,omitempty"`
	DisableTLS      bool   `json:"disableTls"`
}
