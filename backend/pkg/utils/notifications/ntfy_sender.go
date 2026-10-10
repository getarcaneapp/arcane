package notifications

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// BuildNtfyURL converts NtfyConfig to Shoutrrr URL format
// URL example: ntfy://[user:password@]host[:port]/topic[?query]
func BuildNtfyURL(config NtfyConfig) (string, error) {
	if config.Topic == "" {
		return "", errors.New("ntfy topic is required")
	}

	// Default host to ntfy.sh if not specified
	host := cmp.Or(config.Host, "ntfy.sh")

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

	if config.Title != "" {
		q.Set("title", config.Title)
	}

	if config.Priority != "" {
		q.Set("priority", config.Priority)
	}

	if len(config.Tags) > 0 {
		q.Set("tags", strings.Join(config.Tags, ","))
	}

	if config.Icon != "" {
		q.Set("icon", config.Icon)
	}

	// Arcane notification bodies use Markdown for improved readability.
	q.Set("markdown", "yes")

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

	if config.DisableTLS {
		q.Set("disabletls", "yes")
	}

	if config.DisableTLSVerification {
		q.Set("disabletlsverification", "yes")
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendNtfy sends a message via Shoutrrr Ntfy using proper service configuration
func SendNtfy(ctx context.Context, config NtfyConfig, message string) error {
	if config.Topic == "" {
		return errors.New("ntfy topic is required")
	}

	shoutrrrURL, err := BuildNtfyURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Ntfy URL: %w", err)
	}

	return sendShoutrrrInternal("Ntfy", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
