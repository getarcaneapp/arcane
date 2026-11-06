package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func BuildGoogleChatURL(config GoogleChatConfig) (string, error) {
	if config.WebhookURL == "" {
		return "", errors.New("google chat webhook URL is empty")
	}

	parsed, err := url.Parse(config.WebhookURL)
	if err != nil {
		return "", fmt.Errorf("invalid google chat webhook URL: %w", err)
	}

	// Accept a bare host without scheme, mirroring the generic webhook's URL
	// normalisation.
	if parsed.Host == "" && !strings.Contains(config.WebhookURL, "://") {
		normalized := strings.TrimPrefix(config.WebhookURL, "//")
		parsed, err = url.Parse("https://" + normalized)
		if err != nil {
			return "", fmt.Errorf("invalid google chat webhook URL: %w", err)
		}
	}

	if parsed.Host == "" {
		return "", errors.New("invalid google chat webhook URL: missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("invalid google chat webhook URL scheme: %s", parsed.Scheme)
	}

	shoutrrrURL := &url.URL{
		Scheme:   "googlechat",
		Host:     parsed.Host,
		Path:     parsed.Path,
		RawQuery: parsed.Query().Encode(),
	}

	return shoutrrrURL.String(), nil
}

func SendGoogleChat(ctx context.Context, config GoogleChatConfig, message string) error {
	shoutrrrURL, err := BuildGoogleChatURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Google Chat URL: %w", err)
	}

	return sendShoutrrr("Google Chat", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
