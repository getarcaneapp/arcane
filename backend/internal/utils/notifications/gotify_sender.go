package notifications

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// BuildGotifyURL converts GotifyConfig to Shoutrrr URL format
// URL example: gotify://host[:port][/path]/token[?query]
func BuildGotifyURL(config models.GotifyConfig) (string, error) {
	if config.Host == "" {
		return "", fmt.Errorf("gotify host is required")
	}

	if config.Token == "" {
		return "", fmt.Errorf("gotify token is required")
	}

	u := &url.URL{
		Scheme: "gotify",
	}

	host := config.Host
	if config.Port > 0 {
		u.Host = fmt.Sprintf("%s:%d", host, config.Port)
	} else {
		u.Host = host
	}

	path := config.Path
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	u.Path = path + "/" + config.Token

	q := u.Query()
	// Always set priority if it's within valid range, 0 is a valid priority
	q.Set("priority", fmt.Sprintf("%d", config.Priority))

	if config.Title != "" {
		q.Set("title", config.Title)
	}
	if config.DisableTLS {
		q.Set("disabletls", "yes")
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendGotify sends a message via Shoutrrr Gotify using proper service configuration
func SendGotify(ctx context.Context, config models.GotifyConfig, message string) error {
	shoutrrrURL, err := BuildGotifyURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Gotify URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Gotify sender: %w", err)
	}

	params := &shoutrrrTypes.Params{}

	errs := sender.Send(message, params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Gotify message via shoutrrr: %w", err)
		}
	}
	return nil
}
