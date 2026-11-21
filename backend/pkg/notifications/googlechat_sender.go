package notifications

import (
	"context"
	"fmt"
	"net/url"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func BuildGoogleChatURL(config GoogleChatConfig) (string, error) {
	parsed, err := parseWebhookURL(config.WebhookURL, "https", "google chat webhook URL")
	if err != nil {
		return "", err
	}

	return (&url.URL{
		Scheme:   "googlechat",
		Host:     parsed.Host,
		Path:     parsed.Path,
		RawQuery: parsed.Query().Encode(),
	}).String(), nil
}

func SendGoogleChat(ctx context.Context, config GoogleChatConfig, message string) error {
	shoutrrrURL, err := BuildGoogleChatURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Google Chat URL: %w", err)
	}

	return sendShoutrrr("Google Chat", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
