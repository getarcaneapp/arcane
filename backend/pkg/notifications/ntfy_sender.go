package notifications

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func BuildNtfyURL(config NtfyConfig) (string, error) {
	if config.Topic == "" {
		return "", errors.New("ntfy topic is required")
	}

	host := cmp.Or(config.Host, "ntfy.sh")

	u := &url.URL{
		Scheme: "ntfy",
	}

	if config.Username != "" || config.Password != "" {
		u.User = url.UserPassword(config.Username, config.Password)
	}

	if config.Port > 0 {
		u.Host = fmt.Sprintf("%s:%d", host, config.Port)
	} else {
		u.Host = host
	}

	u.Path = "/" + config.Topic

	q := u.Query()

	if config.Title != "" {
		q.Set("title", config.Title)
	}

	if config.Priority != "" {
		q.Set("priority", config.Priority)
	}

	if len(config.Tags) > 0 {
		q.Set("tags", strings.Join(config.Tags, ","))
	}

	if config.Icon != "" {
		q.Set("icon", config.Icon)
	}

	// Arcane notification bodies use Markdown for improved readability.
	q.Set("markdown", "yes")

	// Always send explicit cache/firebase params to avoid ambiguity
	if config.Cache {
		q.Set("cache", "yes")
	} else {
		q.Set("cache", "no")
	}

	if config.Firebase {
		q.Set("firebase", "yes")
	} else {
		q.Set("firebase", "no")
	}

	if config.DisableTLS {
		q.Set("disabletls", "yes")
	}

	if config.DisableTLSVerification {
		q.Set("disabletlsverification", "yes")
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func SendNtfy(ctx context.Context, config NtfyConfig, message string) error {
	if config.Topic == "" {
		return errors.New("ntfy topic is required")
	}

	shoutrrrURL, err := BuildNtfyURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Ntfy URL: %w", err)
	}

	return sendShoutrrr("Ntfy", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
