package notifications

import (
	"context"
	"fmt"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/telegram"
)

// BuildTelegramURL converts TelegramConfig to Shoutrrr URL format using shoutrrr's Config
func BuildTelegramURL(config models.TelegramConfig) (string, error) {
	telegramConfig := &telegram.Config{
		Token:        config.BotToken,
		Chats:        config.ChatIDs,
		Preview:      config.Preview,
		Notification: config.Notification,
		Title:        config.Title,
	}

	url := telegramConfig.GetURL()
	return url.String(), nil
}

// SendTelegram sends a message via Shoutrrr Telegram using proper service configuration
func SendTelegram(ctx context.Context, config models.TelegramConfig, message string) error {
	if config.BotToken == "" {
		return fmt.Errorf("telegram bot token is empty")
	}
	if len(config.ChatIDs) == 0 {
		return fmt.Errorf("no telegram chat IDs configured")
	}

	shoutrrrURL, err := BuildTelegramURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Telegram URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Telegram sender: %w", err)
	}

	errs := sender.Send(message, nil)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Telegram message via shoutrrr: %w", err)
		}
	}
	return nil
}
