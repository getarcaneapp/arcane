package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/telegram"
)

// BuildTelegramURL converts TelegramConfig to Shoutrrr URL format using shoutrrr's Config
func BuildTelegramURL(config TelegramConfig) (string, error) {
	telegramConfig := &telegram.Config{
		Token:        config.BotToken,
		Chats:        config.ChatIDs,
		Preview:      config.Preview,
		Notification: config.Notification,
		Title:        config.Title,
	}

	url := telegramConfig.GetURL()

	// Add parse mode as query parameter if provided
	if config.ParseMode != "" {
		// Validate parse mode
		switch config.ParseMode {
		case "Markdown", "HTML", "MarkdownV2", "None":
			q := url.Query()
			q.Set("parsemode", config.ParseMode)
			url.RawQuery = q.Encode()
		default:
			return "", fmt.Errorf("invalid parse mode: %s (must be Markdown, HTML, MarkdownV2, or None)", config.ParseMode)
		}
	}

	return url.String(), nil
}

// SendTelegram sends a message via Shoutrrr Telegram using proper service configuration
func SendTelegram(ctx context.Context, config TelegramConfig, message string) error {
	if config.BotToken == "" {
		return errors.New("telegram bot token is empty")
	}
	if len(config.ChatIDs) == 0 {
		return errors.New("no telegram chat IDs configured")
	}

	shoutrrrURL, err := BuildTelegramURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Telegram URL: %w", err)
	}

	return sendShoutrrrInternal("Telegram", shoutrrrURL, message, nil)
}
