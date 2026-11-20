package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"
)

func SendSlack(ctx context.Context, config SlackConfig, message string) error {
	if config.Token == "" {
		return errors.New("slack token is empty")
	}

	token, err := slack.ParseToken(config.Token)
	if err != nil {
		return fmt.Errorf("invalid Slack token format (expected format: xoxb-..., xoxp-... or a webhook URL): %w", err)
	}

	service := &slack.Service{
		Config: &slack.Config{
			BotName:  config.BotName,
			Icon:     config.Icon,
			Token:    *token,
			Color:    config.Color,
			Title:    config.Title,
			Channel:  config.Channel,
			ThreadTS: config.ThreadTS,
		},
	}
	if sendErr := service.SendContext(ctx, message, nil); sendErr != nil {
		return fmt.Errorf("failed to send Slack message via shoutrrr: %w", sendErr)
	}
	return nil
}
