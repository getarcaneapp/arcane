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

	// Build generic service URL
	// Format: generic://host[:port]/path?params
	// Shoutrrr's generic service uses HTTP or HTTPS based on the DisableTLS setting
	scheme := "generic"

	// Start with the base URL
	shoutrrrURL := &url.URL{
		Scheme: scheme,
		Host:   webhookURL.Host,
		Path:   webhookURL.Path,
	}

	// Build query parameters
	query := url.Values{}

	// Set template to JSON (default for generic webhooks)
	query.Set("template", "json")

	// Set content type if provided
	if config.ContentType != "" {
		query.Set("contenttype", config.ContentType)
	}

	// Set HTTP method if provided
	if config.Method != "" {
		query.Set("method", config.Method)
	}

	// Set title and message keys if provided
	if config.TitleKey != "" {
		query.Set("titlekey", config.TitleKey)
	}
	if config.MessageKey != "" {
		query.Set("messagekey", config.MessageKey)
	}

	// Determine TLS setting: respect both the original URL scheme and explicit config
	// If original URL was http:// OR DisableTLS is true → use HTTP (disabletls=yes)
	// Otherwise → use HTTPS (disabletls=no)
	disableTLS := config.DisableTLS || strings.ToLower(webhookURL.Scheme) == "http"
	if disableTLS {
		query.Set("disabletls", "yes")
	} else {
		query.Set("disabletls", "no")
	}

	// Add custom headers as query parameters with @ prefix
	if len(config.CustomHeaders) > 0 {
		for key, value := range config.CustomHeaders {
			// Shoutrrr uses @ prefix for headers
			query.Set("@"+key, value)
		}
	}

	shoutrrrURL.RawQuery = query.Encode()

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

	// Build params with title
	params := shoutrrrTypes.Params{}
	if title != "" {
		titleKey := config.TitleKey
		if titleKey == "" {
			titleKey = "title"
		}
		params[titleKey] = title
	}

	errs := sender.Send(message, &params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Generic webhook message with title via shoutrrr: %w", err)
		}
	}
	return nil
}
