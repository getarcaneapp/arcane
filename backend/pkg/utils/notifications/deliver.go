package notifications

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"net/mail"

	"emperror.dev/errors"
)

// Content is the provider-independent description of one notification: the
// message text per format plus the per-event knobs that vary between events.
type Content struct {
	// Text holds the message body per format, built once per fan-out via the
	// messages.go builders.
	Text map[MessageFormat]string

	// Title is the generic-webhook title.
	Title string

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
	if err := DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
	}
	if err := SendDiscord(ctx, discordConfig, c.Text[MessageFormatMarkdown]); err != nil {
		return errors.WrapIf(err, "failed to send Discord notification")
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
	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return errors.WrapIf(err, "invalid from address")
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return errors.WrapIff(err, "invalid to address %s", addr)
		}
	}
	if err := DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
	}
	if c.RenderEmail == nil {
		return errors.New("email rendering not configured for this notification")
	}
	subject, htmlBody, err := c.RenderEmail()
	if err != nil {
		return err
	}
	if err := SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return errors.WrapIf(err, "failed to send email")
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
	if err := DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
	}
	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}
	if err := SendTelegram(ctx, telegramConfig, c.Text[MessageFormatHTML]); err != nil {
		return errors.WrapIf(err, "failed to send Telegram notification")
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
	if err := DecryptStringCredential(&signalConfig.Password); err != nil {
		return err
	}
	if err := DecryptStringCredential(&signalConfig.Token); err != nil {
		return err
	}
	if err := SendSignal(ctx, signalConfig, c.Text[MessageFormatPlain]); err != nil {
		return errors.WrapIf(err, "failed to send Signal notification")
	}
	return nil
}

func deliverSlack(ctx context.Context, config database.JSON, c Content) error {
	slackConfig, err := PrepareSlackConfig(config, "Slack", true)
	if err != nil {
		return err
	}
	if err := SendSlack(ctx, slackConfig, c.Text[MessageFormatSlack]); err != nil {
		return errors.WrapIf(err, "failed to send Slack notification")
	}
	return nil
}

func deliverNtfy(ctx context.Context, config database.JSON, c Content) error {
	ntfyConfig, err := PrepareNtfyConfig(config, "Ntfy", c.RequireNtfyTopic)
	if err != nil {
		return err
	}
	if err := SendNtfy(ctx, ntfyConfig, c.Text[MessageFormatPlain]); err != nil {
		return errors.WrapIf(err, "failed to send Ntfy notification")
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
	if err := SendPushover(ctx, pushoverConfig, c.Text[MessageFormatPlain]); err != nil {
		return errors.WrapIf(err, "failed to send Pushover notification")
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
	if err := SendGotify(ctx, gotifyConfig, c.Text[MessageFormatPlain]); err != nil {
		return errors.WrapIf(err, "failed to send Gotify notification")
	}
	return nil
}

func deliverMatrix(ctx context.Context, config database.JSON, c Content) error {
	matrixConfig, err := PrepareMatrixConfig(config)
	if err != nil {
		return err
	}
	if err := SendMatrix(ctx, matrixConfig, c.Text[MessageFormatPlain]); err != nil {
		return errors.WrapIf(err, "failed to send Matrix notification")
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
	if err := DecryptStringCredential(&googleChatConfig.WebhookURL); err != nil {
		return err
	}
	if err := SendGoogleChat(ctx, googleChatConfig, c.Text[MessageFormatPlain]); err != nil {
		return errors.WrapIf(err, "failed to send Google Chat notification")
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
	if err := SendGenericWithTitle(ctx, genericConfig, c.Title, c.Text[MessageFormatPlain], c.Vars); err != nil {
		return errors.WrapIf(err, "failed to send Generic webhook notification")
	}
	return nil
}

// TextByFormat builds the per-format message map for Content.Text from a
// single messages.go builder closure.
func TextByFormat(build func(MessageFormat) string) map[MessageFormat]string {
	formats := []MessageFormat{MessageFormatMarkdown, MessageFormatHTML, MessageFormatSlack, MessageFormatPlain}
	text := make(map[MessageFormat]string, len(formats))
	for _, format := range formats {
		text[format] = build(format)
	}
	return text
}
