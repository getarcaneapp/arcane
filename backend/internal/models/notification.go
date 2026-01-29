package models

import (
	"time"
)

type NotificationProvider string

const (
	NotificationProviderDiscord  NotificationProvider = "discord"
	NotificationProviderEmail    NotificationProvider = "email"
	NotificationProviderTelegram NotificationProvider = "telegram"
	NotificationProviderSignal   NotificationProvider = "signal"
	NotificationProviderSlack    NotificationProvider = "slack"
	NotificationProviderNtfy     NotificationProvider = "ntfy"
	NotificationProviderPushover NotificationProvider = "pushover"
	NotificationProviderGotify   NotificationProvider = "gotify"
	NotificationProviderGeneric  NotificationProvider = "generic"
)

var validNotificationProviders = map[NotificationProvider]struct{}{
	NotificationProviderDiscord:  {},
	NotificationProviderEmail:    {},
	NotificationProviderTelegram: {},
	NotificationProviderSignal:   {},
	NotificationProviderSlack:    {},
	NotificationProviderNtfy:     {},
	NotificationProviderPushover: {},
	NotificationProviderGotify:   {},
	NotificationProviderGeneric:  {},
}

func IsValidNotificationProvider(provider NotificationProvider) bool {
	_, ok := validNotificationProviders[provider]
	return ok
}

type NotificationEventType string

const (
	NotificationEventImageUpdate     NotificationEventType = "image_update"
	NotificationEventContainerUpdate NotificationEventType = "container_update"
)

type EmailTLSMode string

const (
	EmailTLSModeNone     EmailTLSMode = "none"
	EmailTLSModeStartTLS EmailTLSMode = "starttls"
	EmailTLSModeSSL      EmailTLSMode = "ssl"
)

type NotificationSettings struct {
	ID        uint                 `json:"id" gorm:"primaryKey"`
	Provider  NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
	Enabled   bool                 `json:"enabled" gorm:"default:false"`
	Config    JSON                 `json:"config" gorm:"type:jsonb"`
	CreatedAt time.Time            `json:"createdAt"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

func (NotificationSettings) TableName() string {
	return "notification_settings"
}

type NotificationLog struct {
	ID        uint                 `json:"id" gorm:"primaryKey"`
	Provider  NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
	ImageRef  string               `json:"imageRef" gorm:"not null"`
	Status    string               `json:"status" gorm:"not null"`
	Error     *string              `json:"error,omitempty"`
	Metadata  JSON                 `json:"metadata" gorm:"type:jsonb"`
	SentAt    time.Time            `json:"sentAt" gorm:"not null;index"`
	CreatedAt time.Time            `json:"createdAt"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

func (NotificationLog) TableName() string {
	return "notification_logs"
}

type DiscordConfig struct {
	WebhookID string                         `json:"webhookId"`
	Token     string                         `json:"token"`
	Username  string                         `json:"username,omitempty"`
	AvatarURL string                         `json:"avatarUrl,omitempty"`
	Events    map[NotificationEventType]bool `json:"events,omitempty"`
}

type EmailConfig struct {
	SMTPHost     string                         `json:"smtpHost"`
	SMTPPort     int                            `json:"smtpPort"`
	SMTPUsername string                         `json:"smtpUsername"`
	SMTPPassword string                         `json:"smtpPassword"`
	FromAddress  string                         `json:"fromAddress"`
	ToAddresses  []string                       `json:"toAddresses"`
	TLSMode      EmailTLSMode                   `json:"tlsMode"`
	Events       map[NotificationEventType]bool `json:"events,omitempty"`
}

type TelegramConfig struct {
	BotToken     string                         `json:"botToken"`
	ChatIDs      []string                       `json:"chatIds"`
	Preview      bool                           `json:"preview"`
	Notification bool                           `json:"notification"`
	ParseMode    string                         `json:"parseMode,omitempty"`
	Title        string                         `json:"title,omitempty"`
	Events       map[NotificationEventType]bool `json:"events,omitempty"`
}

type SignalConfig struct {
	Host       string                         `json:"host"`
	Port       int                            `json:"port"`
	User       string                         `json:"user,omitempty"`
	Password   string                         `json:"password,omitempty"`
	Token      string                         `json:"token,omitempty"`
	Source     string                         `json:"source"`
	Recipients []string                       `json:"recipients"`
	DisableTLS bool                           `json:"disableTls"`
	Events     map[NotificationEventType]bool `json:"events,omitempty"`
}

type SlackConfig struct {
	Token    string                         `json:"token"`
	BotName  string                         `json:"botName,omitempty"`
	Icon     string                         `json:"icon,omitempty"`
	Color    string                         `json:"color,omitempty"`
	Title    string                         `json:"title,omitempty"`
	Channel  string                         `json:"channel,omitempty"`
	ThreadTS string                         `json:"threadTs,omitempty"`
	Events   map[NotificationEventType]bool `json:"events,omitempty"`
}

type NtfyConfig struct {
	Host                   string                         `json:"host"`
	Port                   int                            `json:"port"`
	Topic                  string                         `json:"topic"`
	Username               string                         `json:"username,omitempty"`
	Password               string                         `json:"password,omitempty"`
	Priority               string                         `json:"priority,omitempty"`
	Tags                   []string                       `json:"tags,omitempty"`
	Icon                   string                         `json:"icon,omitempty"`
	Cache                  bool                           `json:"cache"`
	Firebase               bool                           `json:"firebase"`
	DisableTLSVerification bool                           `json:"disableTlsVerification"`
	Events                 map[NotificationEventType]bool `json:"events,omitempty"`
}

type PushoverConfig struct {
	Token    string                         `json:"token"`
	User     string                         `json:"user"`
	Devices  []string                       `json:"devices,omitempty"`
	Priority int8                           `json:"priority"`
	Title    string                         `json:"title,omitempty"`
	Events   map[NotificationEventType]bool `json:"events,omitempty"`
}

type GotifyConfig struct {
	Host       string                         `json:"host"`
	Port       int                            `json:"port,omitempty"`
	Token      string                         `json:"token"`
	Path       string                         `json:"path,omitempty"`
	Priority   int                            `json:"priority,omitempty"`
	Title      string                         `json:"title,omitempty"`
	DisableTLS bool                           `json:"disableTls"`
	Events     map[NotificationEventType]bool `json:"events,omitempty"`
}

type GenericConfig struct {
	WebhookURL    string                         `json:"webhookUrl"`
	Method        string                         `json:"method,omitempty"`
	ContentType   string                         `json:"contentType,omitempty"`
	TitleKey      string                         `json:"titleKey,omitempty"`
	MessageKey    string                         `json:"messageKey,omitempty"`
	CustomHeaders map[string]string              `json:"customHeaders,omitempty"`
	DisableTLS    bool                           `json:"disableTls"`
	Events        map[NotificationEventType]bool `json:"events,omitempty"`
}

type AppriseSettings struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	APIURL             string    `json:"apiUrl" gorm:"not null"`
	Enabled            bool      `json:"enabled" gorm:"default:false"`
	ImageUpdateTag     string    `json:"imageUpdateTag" gorm:"type:varchar(255)"`
	ContainerUpdateTag string    `json:"containerUpdateTag" gorm:"type:varchar(255)"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func (AppriseSettings) TableName() string {
	return "apprise_settings"
}
