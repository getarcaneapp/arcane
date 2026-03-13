package notifications

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
)

// BuildPushoverURL converts PushoverConfig to Shoutrrr URL format.
// URL example: pushover://:token@user?devices=device1,device2&priority=1&title=Container+Update
func BuildPushoverURL(config models.PushoverConfig) (string, error) {
	user := strings.TrimSpace(config.User)
	token := strings.TrimSpace(config.Token)

	if user == "" {
		return "", fmt.Errorf("pushover user key is required")
	}
	if token == "" {
		return "", fmt.Errorf("pushover token is required")
	}
	if config.Priority < -2 || config.Priority > 2 {
		return "", fmt.Errorf("pushover priority must be between -2 and 2")
	}

	u := &url.URL{
		Scheme: "pushover",
		Host:   user,
		User:   url.UserPassword("", token),
	}

	q := u.Query()
	devices := make([]string, 0, len(config.Devices))
	for _, device := range config.Devices {
		trimmed := strings.TrimSpace(device)
		if trimmed != "" {
			devices = append(devices, trimmed)
		}
	}
	if len(devices) > 0 {
		q.Set("devices", strings.Join(devices, ","))
	}
	if config.Priority != 0 {
		q.Set("priority", strconv.FormatInt(int64(config.Priority), 10))
	}
	if title := strings.TrimSpace(config.Title); title != "" {
		q.Set("title", title)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendPushover sends a message via Shoutrrr Pushover using proper service configuration.
func SendPushover(ctx context.Context, config models.PushoverConfig, message string) error {
	if strings.TrimSpace(config.Token) == "" {
		return fmt.Errorf("pushover token is empty")
	}
	if strings.TrimSpace(config.User) == "" {
		return fmt.Errorf("pushover user key is empty")
	}
	if config.Priority < -2 || config.Priority > 2 {
		return fmt.Errorf("pushover priority must be between -2 and 2")
	}

	shoutrrrURL, err := BuildPushoverURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Pushover URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Pushover sender: %w", err)
	}

	errs := sender.Send(message, nil)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Pushover message via shoutrrr: %w", err)
		}
	}
	return nil
}
