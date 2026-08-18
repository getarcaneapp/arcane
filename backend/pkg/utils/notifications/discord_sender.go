package notifications

import (
	"context"

	"emperror.dev/errors"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// BuildDiscordURL converts DiscordConfig to Shoutrrr URL format using shoutrrr's Config
func BuildDiscordURL(config DiscordConfig) (string, error) {
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
func SendDiscord(ctx context.Context, config DiscordConfig, message string) error {
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

	sender, err := shoutrrr.CreateSenderWithOptions(shoutrrrTypes.SenderOptions{}, shoutrrrURL)
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
