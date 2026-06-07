package notifications

import (
	"context"
	"fmt"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"
)

// BuildSlackURL converts SlackConfig to Shoutrrr URL format using shoutrrr's Config
func BuildSlackURL(config models.SlackConfig) (string, error) {
	if config.Token == "" {
		return "", fmt.Errorf("slack token is required")
	}

	// Parse the token to get the token object
	token, err := slack.ParseToken(config.Token)
	if err != nil {
		return "", fmt.Errorf("invalid Slack token format (expected format: xoxb-... or xoxp-...): %w", err)
	}

	slackConfig := &slack.Config{
		BotName:  config.BotName,
		Icon:     config.Icon,
		Token:    *token,
		Color:    config.Color,
		Title:    config.Title,
		Channel:  config.Channel,
		ThreadTS: config.ThreadTS,
	}

	url := slackConfig.GetURL()
	return url.String(), nil
}

// SendSlack sends a message via Shoutrrr Slack using proper service configuration
func SendSlack(ctx context.Context, config models.SlackConfig, message string) error {
	if config.Token == "" {
		return fmt.Errorf("slack token is empty")
	}

	shoutrrrURL, err := BuildSlackURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Slack URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Slack sender: %w", err)
	}

	errs := sender.Send(message, nil)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Slack message via shoutrrr: %w", err)
		}
	}
	return nil
}
