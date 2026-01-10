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

// BuildNtfyURL converts NtfyConfig to Shoutrrr URL format
// URL example: ntfy://[user:password@]host[:port]/topic[?query]
func BuildNtfyURL(config models.NtfyConfig) (string, error) {
	if config.Topic == "" {
		return "", fmt.Errorf("ntfy topic is required")
	}

	// Default host to ntfy.sh if not specified
	host := config.Host
	if host == "" {
		host = "ntfy.sh"
	}

	// Build the base URL
	u := &url.URL{
		Scheme: "ntfy",
	}

	// Add authentication if provided
	if config.Username != "" || config.Password != "" {
		u.User = url.UserPassword(config.Username, config.Password)
	}

	// Set host and port
	if config.Port > 0 {
		u.Host = fmt.Sprintf("%s:%d", host, config.Port)
	} else {
		u.Host = host
	}

	// Set path (topic)
	u.Path = "/" + config.Topic

	// Add query parameters
	q := u.Query()

	if config.Priority != "" {
		q.Set("priority", config.Priority)
	}

	if len(config.Tags) > 0 {
		q.Set("tags", strings.Join(config.Tags, ","))
	}

	if config.Icon != "" {
		q.Set("icon", config.Icon)
	}

	// Always send explicit cache/firebase params to avoid ambiguity
	if config.Cache {
		q.Set("cache", "yes")
	} else {
		q.Set("cache", "no")
	}

	if config.Firebase {
		q.Set("firebase", "yes")
	} else {
		q.Set("firebase", "no")
	}

	if config.DisableTLSVerification {
		q.Set("disabletls", "yes")
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendNtfy sends a message via Shoutrrr Ntfy using proper service configuration
func SendNtfy(ctx context.Context, config models.NtfyConfig, message string) error {
	if config.Topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}

	shoutrrrURL, err := BuildNtfyURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Ntfy URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Ntfy sender: %w", err)
	}

	params := &shoutrrrTypes.Params{}

	errs := sender.Send(message, params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Ntfy message via shoutrrr: %w", err)
		}
	}
	return nil
}
