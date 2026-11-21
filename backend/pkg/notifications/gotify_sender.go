package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func BuildGotifyURL(config GotifyConfig) (string, error) {
	if config.Host == "" {
		return "", errors.New("gotify host is required")
	}

	if config.Token == "" {
		return "", errors.New("gotify token is required")
	}

	host := config.Host
	if config.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, config.Port)
	}

	path := config.Path
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")

	q := url.Values{}
	// Always set priority if it's within valid range, 0 is a valid priority
	q.Set("priority", strconv.Itoa(config.Priority))

	if config.Title != "" {
		q.Set("title", config.Title)
	}
	if config.DisableTLS {
		q.Set("disabletls", "yes")
	}
	if config.InsecureSkipVerify {
		q.Set("insecureskipverify", "yes")
	}
	if config.UseHeader {
		q.Set("useheader", "yes")
	}

	return (&url.URL{
		Scheme:   "gotify",
		Host:     host,
		Path:     path + "/" + config.Token,
		RawQuery: q.Encode(),
	}).String(), nil
}

func SendGotify(ctx context.Context, config GotifyConfig, message string) error {
	shoutrrrURL, err := BuildGotifyURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Gotify URL: %w", err)
	}

	return sendShoutrrr("Gotify", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
