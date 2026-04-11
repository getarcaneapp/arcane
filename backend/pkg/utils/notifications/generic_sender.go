package notifications

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// BuildGenericURL converts GenericConfig to Shoutrrr URL format for generic webhooks
func BuildGenericURL(config models.GenericConfig) (string, error) {
	if config.WebhookURL == "" {
		return "", fmt.Errorf("webhook URL is empty")
	}

	// Parse the webhook URL
	webhookURL, err := url.Parse(config.WebhookURL)
	if err != nil {
		return "", fmt.Errorf("invalid webhook URL: %w", err)
	}

	hasScheme := strings.Contains(config.WebhookURL, "://")
	if webhookURL.Host == "" && !hasScheme {
		fallbackScheme := "https"
		if config.DisableTLS {
			fallbackScheme = "http"
		}
		normalized := strings.TrimPrefix(config.WebhookURL, "//")
		webhookURL, err = url.Parse(fmt.Sprintf("%s://%s", fallbackScheme, normalized))
		if err != nil {
			return "", fmt.Errorf("invalid webhook URL: %w", err)
		}
	}

	if webhookURL.Host == "" {
		return "", fmt.Errorf("invalid webhook URL: missing host")
	}

	// Build generic service URL
	// Format: generic://host[:port]/path?params
	// Shoutrrr's generic service uses HTTP or HTTPS based on the DisableTLS setting.

	// Start from the user's existing query parameters. Shoutrrr's generic
	// service preserves any query keys it does not recognise, so provider
	// tokens embedded in the webhook URL (e.g. PushPlus's `?token=...`) flow
	// straight through to the outbound HTTP request untouched.
	//
	// For Shoutrrr config keys (template, contenttype, method, titlekey,
	// messagekey, disabletls) we only fill in defaults / configured values
	// when the user has not already set the same key inline in the URL.
	// That way an explicit `?template=custom` or `?disabletls=yes` from the
	// user is always respected and never silently overwritten by the
	// provider settings or the URL-scheme-derived TLS flag.
	query := webhookURL.Query()

	setDefault := func(key, value string) {
		if value == "" {
			return
		}
		if query.Get(key) != "" {
			return
		}
		query.Set(key, value)
	}

	// Default to the JSON template — Shoutrrr's JSON template marshals the
	// notification params as a flat JSON object at the root level, which is
	// the format most providers (PushPlus, custom APIs, Home Assistant, etc.)
	// expect.
	setDefault("template", "json")
	setDefault("contenttype", config.ContentType)
	setDefault("method", config.Method)
	setDefault("titlekey", config.TitleKey)
	setDefault("messagekey", config.MessageKey)

	// Determine TLS setting from the webhook URL scheme (http/https) when the
	// user has not already passed `disabletls` explicitly. If the scheme is
	// missing here we treat it as a hard error because Shoutrrr needs an
	// explicit transport.
	switch strings.ToLower(webhookURL.Scheme) {
	case "http":
		setDefault("disabletls", "yes")
	case "https":
		setDefault("disabletls", "no")
	default:
		return "", fmt.Errorf("invalid webhook URL scheme: %s", webhookURL.Scheme)
	}

	// Add custom headers as query parameters with @ prefix
	if len(config.CustomHeaders) > 0 {
		for key, value := range config.CustomHeaders {
			// Shoutrrr uses @ prefix for headers
			query.Set("@"+key, value)
		}
	}

	shoutrrrURL := &url.URL{
		Scheme:   "generic",
		Host:     webhookURL.Host,
		Path:     webhookURL.Path,
		RawQuery: query.Encode(),
	}

	return shoutrrrURL.String(), nil
}

// SendGenericWithTitle sends a message with title via Shoutrrr Generic webhook
func SendGenericWithTitle(ctx context.Context, config models.GenericConfig, title, message string) error {
	if config.WebhookURL == "" {
		return fmt.Errorf("webhook URL is empty")
	}

	shoutrrrURL, err := BuildGenericURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Generic URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Generic sender: %w", err)
	}

	// Build params with title. Always use "title" as the param key — Shoutrrr's
	// generic service maps it to the configured titlekey in the JSON payload.
	params := shoutrrrTypes.Params{}
	if title != "" {
		params["title"] = title
	}

	errs := sender.Send(message, &params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Generic webhook message with title via shoutrrr: %w", err)
		}
	}
	return nil
}
