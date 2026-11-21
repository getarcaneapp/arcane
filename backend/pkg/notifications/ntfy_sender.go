package notifications

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/types"

	"kit.getarcane.app"
)

func BuildNtfyURL(config NtfyConfig) (string, error) {
	if config.Topic == "" {
		return "", errors.New("ntfy topic is required")
	}

	host := cmp.Or(config.Host, "ntfy.sh")
	if config.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, config.Port)
	}
	var user *url.Userinfo
	if config.Username != "" || config.Password != "" {
		user = url.UserPassword(config.Username, config.Password)
	}

	q := url.Values{
		// Arcane notification bodies use Markdown for improved readability.
		"markdown": {"yes"},
		// Always send explicit cache/firebase params to avoid ambiguity
		"cache":    {kit.Ternary(config.Cache, "yes", "no")},
		"firebase": {kit.Ternary(config.Firebase, "yes", "no")},
	}
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
	if config.DisableTLS {
		q.Set("disabletls", "yes")
	}
	if config.DisableTLSVerification {
		q.Set("disabletlsverification", "yes")
	}

	return (&url.URL{
		Scheme:   "ntfy",
		User:     user,
		Host:     host,
		Path:     "/" + config.Topic,
		RawQuery: q.Encode(),
	}).String(), nil
}

func SendNtfy(ctx context.Context, config NtfyConfig, message string) error {
	if config.Topic == "" {
		return errors.New("ntfy topic is required")
	}

	shoutrrrURL, err := BuildNtfyURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Ntfy URL: %w", err)
	}

	return sendShoutrrr("Ntfy", shoutrrrURL, message, &types.Params{})
}
