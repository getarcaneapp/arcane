package notifications

import (
	"context"
	"fmt"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/signal"
)

// BuildSignalURL converts SignalConfig to Shoutrrr URL format using shoutrrr's Config
func BuildSignalURL(config models.SignalConfig) (string, error) {
	signalConfig := &signal.Config{
		Host:       config.Host,
		Port:       config.Port,
		User:       config.User,
		Password:   config.Password,
		Token:      config.Token,
		Source:     config.Source,
		Recipients: config.Recipients,
		DisableTLS: config.DisableTLS,
	}

	url := signalConfig.GetURL()
	return url.String(), nil
}

// SendSignal sends a message via Shoutrrr Signal using proper service configuration
func SendSignal(ctx context.Context, config models.SignalConfig, message string) error {
	if config.Host == "" {
		return fmt.Errorf("signal host is empty")
	}
	if config.Port == 0 {
		return fmt.Errorf("signal port is not set")
	}
	if config.Source == "" {
		return fmt.Errorf("signal source phone number is empty")
	}
	if len(config.Recipients) == 0 {
		return fmt.Errorf("no signal recipients configured")
	}

	// Validate authentication
	hasBasicAuth := config.User != "" && config.Password != ""
	hasTokenAuth := config.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return fmt.Errorf("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return fmt.Errorf("signal cannot use both basic auth and token authentication simultaneously")
	}

	shoutrrrURL, err := BuildSignalURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Signal URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Signal sender: %w", err)
	}

	errs := sender.Send(message, nil)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Signal message via shoutrrr: %w", err)
		}
	}
	return nil
}
