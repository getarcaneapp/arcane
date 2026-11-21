package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func BuildMatrixURL(config MatrixConfig) (string, error) {
	if config.Host == "" {
		return "", errors.New("matrix host is required")
	}

	var user *url.Userinfo
	if config.Password != "" {
		user = url.UserPassword(config.Username, config.Password)
	}

	host := config.Host
	if config.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, config.Port)
	}

	q := url.Values{}
	if config.Rooms != "" {
		q.Set("rooms", config.Rooms)
	}
	if config.DisableTLSVerification {
		q.Set("disabletls", "yes")
	}

	return (&url.URL{
		Scheme:   "matrix",
		User:     user,
		Host:     host,
		RawQuery: strings.ReplaceAll(q.Encode(), "%21", "!"), // required by Matrix notification syntax
	}).String(), nil
}

func SendMatrix(ctx context.Context, config MatrixConfig, message string) error {
	shoutrrrURL, err := BuildMatrixURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Matrix URL: %w", err)
	}

	return sendShoutrrr("Matrix", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
