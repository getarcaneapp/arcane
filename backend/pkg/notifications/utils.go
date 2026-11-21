package notifications

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func SanitizeForEmail(text string) string {
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	return strings.TrimSpace(text)
}

func parseWebhookURL(rawURL, defaultScheme, label string) (*url.URL, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("%s is empty", label)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}

	if parsed.Host == "" && !strings.Contains(rawURL, "://") {
		normalized := strings.TrimPrefix(rawURL, "//")
		parsed, err = url.Parse(defaultScheme + "://" + normalized)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", label, err)
		}
	}

	if parsed.Host == "" {
		return nil, errors.New("invalid " + label + ": missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid %s scheme: %s", label, parsed.Scheme)
	}

	return parsed, nil
}

func sendShoutrrr(provider, destinationURL, message string, params *shoutrrrTypes.Params) error {
	sender, err := shoutrrr.CreateSenderWithOptions(shoutrrrTypes.SenderOptions{}, destinationURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr %s sender: %w", provider, err)
	}
	for _, err := range sender.Send(message, params) {
		if err != nil {
			return fmt.Errorf("failed to send %s message via shoutrrr: %w", provider, err)
		}
	}
	return nil
}
