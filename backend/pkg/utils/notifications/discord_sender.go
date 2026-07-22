package notifications

import (
	"context"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
)

// BuildDiscordURL converts DiscordConfig to Shoutrrr URL format using shoutrrr's Config
func BuildDiscordURL(config models.DiscordConfig) (string, error) {
	discordConfig := &discord.Config{
		WebhookID: config.WebhookID,
		Token:     config.Token,
		Username:  config.Username,
		Avatar:    config.AvatarURL,
	}

	url := discordConfig.GetURL()
	return url.String(), nil
}

// SendDiscord sends a message via Shoutrrr Discord using proper service configuration
func SendDiscord(ctx context.Context, config models.DiscordConfig, message string) error {
	if config.WebhookID == "" {
		return errors.New("discord webhook ID is empty")
	}
	if config.Token == "" {
		return errors.New("discord token is empty")
	}

	shoutrrrURL, err := BuildDiscordURL(config)
	if err != nil {
		return errors.WrapIf(err, "failed to build shoutrrr Discord URL")
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return errors.WrapIf(err, "failed to create shoutrrr Discord sender")
	}

	errs := sender.Send(message, nil)
	for _, err := range errs {
		if err != nil {
			return errors.WrapIf(err, "failed to send Discord message via shoutrrr")
		}
	}
	return nil
}
