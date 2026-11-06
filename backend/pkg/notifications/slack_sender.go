package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"
)

func BuildSlackURL(config SlackConfig) (string, error) {
	if config.Token == "" {
		return "", errors.New("slack token is required")
	}

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

func SendSlack(ctx context.Context, config SlackConfig, message string) error {
	if config.Token == "" {
		return errors.New("slack token is empty")
	}

	shoutrrrURL, err := BuildSlackURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Slack URL: %w", err)
	}

	return sendShoutrrr("Slack", shoutrrrURL, message, nil)
}
