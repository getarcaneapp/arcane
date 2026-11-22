package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/signal"
	"github.com/samber/mo"
)

func BuildSignalURL(config SignalConfig) (string, error) {
	signalConfig := &signal.Config{
		Host:       config.Host,
		Port:       config.Port,
		User:       config.User,
		Password:   config.Password,
		Token:      config.Token,
		Source:     config.Source,
		Recipients: config.Recipients,
		DisableTLS: config.DisableTLS,
		NotifySelf: mo.PointerToOption(config.NotifySelf).OrElse(true),
	}

	return signalConfig.GetURL().String(), nil
}

func SendSignal(ctx context.Context, config SignalConfig, message string) error {
	if config.Host == "" {
		return errors.New("signal host is empty")
	}
	if config.Port == 0 {
		return errors.New("signal port is not set")
	}
	if config.Source == "" {
		return errors.New("signal source phone number is empty")
	}
	if len(config.Recipients) == 0 {
		return errors.New("no signal recipients configured")
	}

	hasBasicAuth := config.User != "" && config.Password != ""
	hasTokenAuth := config.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return errors.New("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return errors.New("signal cannot use both basic auth and token authentication simultaneously")
	}

	shoutrrrURL, err := BuildSignalURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Signal URL: %w", err)
	}

	return sendShoutrrr("Signal", shoutrrrURL, message, nil)
}
