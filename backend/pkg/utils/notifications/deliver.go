package notifications

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/mail"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

// Content is the provider-independent description of one notification: the
// message text per format plus the per-event knobs that vary between events.
type Content struct {
	// Text holds the message body per format, built once per fan-out via the
	// messages.go builders.
	Text map[MessageFormat]string

	// Title is the generic-webhook title.
	Title string

	// NtfyTitle is the event-specific ntfy title used when the provider does
	// not have a custom title configured.
	NtfyTitle string

	// DefaultTitle is applied to pushover/gotify when their config has no
	// title; "" means don't default.
	DefaultTitle string

	// RenderEmail lazily renders the email subject and HTML body. Built as a
	// closure by the caller so template resources and app config stay out of
	// this package. Nil means the provider map's email deliverer errors.
	RenderEmail func() (subject, html string, err error)

	// Vars carries per-event variables (environment, environmentId, event,
	// timestamp) for the generic webhook's payload template. They are only
	// injected into the outgoing request when a payload template is configured,
	// so existing template-less generic webhooks keep their exact payload.
	Vars map[string]string

	// RequireNtfyTopic preserves the per-event ntfy topic validation.
	RequireNtfyTopic bool

	// ValidatePushoverUser preserves the per-event pushover token/user validation.
	ValidatePushoverUser bool
}

type delivererFunc func(ctx context.Context, config database.JSON, c Content) error

var providerDeliverers = map[NotificationProvider]delivererFunc{
	NotificationProviderDiscord:    deliverDiscord,
	NotificationProviderEmail:      deliverEmail,
	NotificationProviderTelegram:   deliverTelegram,
	NotificationProviderSignal:     deliverSignal,
	NotificationProviderSlack:      deliverSlack,
	NotificationProviderNtfy:       deliverNtfy,
	NotificationProviderPushover:   deliverPushover,
	NotificationProviderGotify:     deliverGotify,
	NotificationProviderMatrix:     deliverMatrix,
	NotificationProviderGoogleChat: deliverGoogleChat,
	NotificationProviderGeneric:    deliverGeneric,
}

// Deliver sends c to a single provider. handled is false for unknown providers.
func Deliver(ctx context.Context, provider NotificationProvider, config database.JSON, c Content) (handled bool, err error) {
	deliver, ok := providerDeliverers[provider]
	if !ok {
		return false, nil
	}
	return true, deliver(ctx, config, c)
}

func deliverDiscord(ctx context.Context, config database.JSON, c Content) error {
	discordConfig, err := DecodeConfig[DiscordConfig](config, "Discord")
	if err != nil {
		return err
	}
	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return errors.New("discord webhook ID or token not configured")
	}
	if decryptStringCredentialErr := DecryptStringCredential(&discordConfig.Token); decryptStringCredentialErr != nil {
		return decryptStringCredentialErr
	}
	if sendDiscordErr := SendDiscord(ctx, discordConfig, c.Text[MessageFormatMarkdown]); sendDiscordErr != nil {
		return fmt.Errorf("failed to send Discord notification: %w", sendDiscordErr)
	}
	return nil
}

func deliverEmail(ctx context.Context, config database.JSON, c Content) error {
	emailConfig, err := DecodeConfig[EmailConfig](config, "email")
	if err != nil {
		return err
	}
	if emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 {
		return errors.New("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}
	if _, parseAddressErr := mail.ParseAddress(emailConfig.FromAddress); parseAddressErr != nil {
		return fmt.Errorf("invalid from address: %w", parseAddressErr)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, parseAddressErr2 := mail.ParseAddress(addr); parseAddressErr2 != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, parseAddressErr2)
		}
	}
	if decryptStringCredentialErr := DecryptStringCredential(&emailConfig.SMTPPassword); decryptStringCredentialErr != nil {
		return decryptStringCredentialErr
	}
	if c.RenderEmail == nil {
		return errors.New("email rendering not configured for this notification")
	}
	subject, htmlBody, err := c.RenderEmail()
	if err != nil {
		return err
	}
	if sendEmailErr := SendEmail(ctx, emailConfig, subject, htmlBody); sendEmailErr != nil {
		return fmt.Errorf("failed to send email: %w", sendEmailErr)
	}
	return nil
}

func deliverTelegram(ctx context.Context, config database.JSON, c Content) error {
	telegramConfig, err := DecodeConfig[TelegramConfig](config, "Telegram")
	if err != nil {
		return err
	}
	if telegramConfig.BotToken == "" {
		return errors.New("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return errors.New("no telegram chat IDs configured")
	}
	if decryptStringCredentialErr := DecryptStringCredential(&telegramConfig.BotToken); decryptStringCredentialErr != nil {
		return decryptStringCredentialErr
	}
	telegramConfig.ParseMode = cmp.Or(telegramConfig.ParseMode, "HTML")
	if sendTelegramErr := SendTelegram(ctx, telegramConfig, c.Text[MessageFormatHTML]); sendTelegramErr != nil {
		return fmt.Errorf("failed to send Telegram notification: %w", sendTelegramErr)
	}
	return nil
}

func deliverSignal(ctx context.Context, config database.JSON, c Content) error {
	signalConfig, err := DecodeConfig[SignalConfig](config, "Signal")
	if err != nil {
		return err
	}
	if signalConfig.Host == "" || signalConfig.Port == 0 || signalConfig.Source == "" || len(signalConfig.Recipients) == 0 {
		return errors.New("signal not fully configured")
	}
	hasBasicAuth := signalConfig.User != "" && signalConfig.Password != ""
	hasTokenAuth := signalConfig.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return errors.New("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return errors.New("signal cannot use both basic auth and token authentication simultaneously")
	}
	if decryptStringCredentialErr := DecryptStringCredential(&signalConfig.Password); decryptStringCredentialErr != nil {
		return decryptStringCredentialErr
	}
	if decryptStringCredentialErr2 := DecryptStringCredential(&signalConfig.Token); decryptStringCredentialErr2 != nil {
		return decryptStringCredentialErr2
	}
	if sendSignalErr := SendSignal(ctx, signalConfig, c.Text[MessageFormatPlain]); sendSignalErr != nil {
		return fmt.Errorf("failed to send Signal notification: %w", sendSignalErr)
	}
	return nil
}

func deliverSlack(ctx context.Context, config database.JSON, c Content) error {
	slackConfig, err := PrepareSlackConfig(config, "Slack", true)
	if err != nil {
		return err
	}
	if sendSlackErr := SendSlack(ctx, slackConfig, c.Text[MessageFormatSlack]); sendSlackErr != nil {
		return fmt.Errorf("failed to send Slack notification: %w", sendSlackErr)
	}
	return nil
}

func deliverNtfy(ctx context.Context, config database.JSON, c Content) error {
	ntfyConfig, err := PrepareNtfyConfig(config, "Ntfy", c.RequireNtfyTopic)
	if err != nil {
		return err
	}
	if ntfyConfig.Title == "" {
		ntfyConfig.Title = c.NtfyTitle
	}
	if sendNtfyErr := SendNtfy(ctx, ntfyConfig, c.Text[MessageFormatNtfyMarkdown]); sendNtfyErr != nil {
		return fmt.Errorf("failed to send Ntfy notification: %w", sendNtfyErr)
	}
	return nil
}

func deliverPushover(ctx context.Context, config database.JSON, c Content) error {
	pushoverConfig, err := PreparePushoverConfig(config, "Pushover")
	if err != nil {
		return err
	}
	if c.ValidatePushoverUser {
		if pushoverConfig.Token == "" {
			return errors.New("pushover API token not configured")
		}
		if pushoverConfig.User == "" {
			return errors.New("pushover user key not configured")
		}
	}
	if pushoverConfig.Title == "" && c.DefaultTitle != "" {
		pushoverConfig.Title = c.DefaultTitle
	}
	if sendPushoverErr := SendPushover(ctx, pushoverConfig, c.Text[MessageFormatPlain]); sendPushoverErr != nil {
		return fmt.Errorf("failed to send Pushover notification: %w", sendPushoverErr)
	}
	return nil
}

func deliverGotify(ctx context.Context, config database.JSON, c Content) error {
	gotifyConfig, err := PrepareGotifyConfig(config, "Gotify")
	if err != nil {
		return err
	}
	if gotifyConfig.Title == "" && c.DefaultTitle != "" {
		gotifyConfig.Title = c.DefaultTitle
	}
	if sendGotifyErr := SendGotify(ctx, gotifyConfig, c.Text[MessageFormatPlain]); sendGotifyErr != nil {
		return fmt.Errorf("failed to send Gotify notification: %w", sendGotifyErr)
	}
	return nil
}

func deliverMatrix(ctx context.Context, config database.JSON, c Content) error {
	matrixConfig, err := PrepareMatrixConfig(config)
	if err != nil {
		return err
	}
	if sendMatrixErr := SendMatrix(ctx, matrixConfig, c.Text[MessageFormatPlain]); sendMatrixErr != nil {
		return fmt.Errorf("failed to send Matrix notification: %w", sendMatrixErr)
	}
	return nil
}

func deliverGoogleChat(ctx context.Context, config database.JSON, c Content) error {
	googleChatConfig, err := DecodeConfig[GoogleChatConfig](config, "Google Chat")
	if err != nil {
		return err
	}
	if googleChatConfig.WebhookURL == "" {
		return errors.New("google chat webhook URL not configured")
	}
	if decryptStringCredentialErr := DecryptStringCredential(&googleChatConfig.WebhookURL); decryptStringCredentialErr != nil {
		return decryptStringCredentialErr
	}
	if sendGoogleChatErr := SendGoogleChat(ctx, googleChatConfig, c.Text[MessageFormatPlain]); sendGoogleChatErr != nil {
		return fmt.Errorf("failed to send Google Chat notification: %w", sendGoogleChatErr)
	}
	return nil
}

func deliverGeneric(ctx context.Context, config database.JSON, c Content) error {
	genericConfig, err := DecodeConfig[GenericConfig](config, "Generic")
	if err != nil {
		return err
	}
	if genericConfig.WebhookURL == "" {
		return errors.New("webhook URL not configured")
	}
	if sendGenericWithTitleErr := SendGenericWithTitle(ctx, genericConfig, c.Title, c.Text[MessageFormatPlain], c.Vars); sendGenericWithTitleErr != nil {
		return fmt.Errorf("failed to send Generic webhook notification: %w", sendGenericWithTitleErr)
	}
	return nil
}

// TextByFormat builds the per-format message map for Content.Text from a
// single messages.go builder closure.
func TextByFormat(build func(MessageFormat) string) map[MessageFormat]string {
	formats := []MessageFormat{
		MessageFormatMarkdown,
		MessageFormatNtfyMarkdown,
		MessageFormatHTML,
		MessageFormatSlack,
		MessageFormatPlain,
	}
	text := make(map[MessageFormat]string, len(formats))
	for _, format := range formats {
		text[format] = build(format)
	}
	return text
}
