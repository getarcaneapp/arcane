package notifications

import (
	"context"
	"fmt"

	"github.com/getarcaneapp/arcane/backend/internal/models"
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
		return fmt.Errorf("discord webhook ID is empty")
	}
	if config.Token == "" {
		return fmt.Errorf("discord token is empty")
	}

	shoutrrrURL, err := BuildDiscordURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Discord URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Discord sender: %w", err)
	}

	errs := sender.Send(message, nil)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Discord message via shoutrrr: %w", err)
		}
	}
	return nil
}
