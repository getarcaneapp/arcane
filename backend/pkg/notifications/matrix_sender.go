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

	u := &url.URL{
		Scheme: "matrix",
	}

	if config.Password != "" {
		u.User = url.UserPassword(config.Username, config.Password)
	}

	host := config.Host
	if config.Port > 0 {
		u.Host = fmt.Sprintf("%s:%d", host, config.Port)
	} else {
		u.Host = host
	}

	q := u.Query()

	if config.Rooms != "" {
		q.Set("rooms", config.Rooms)
	}

	if config.DisableTLSVerification {
		q.Set("disabletls", "yes")
	}

	rawQuery := q.Encode()
	rawQuery = strings.ReplaceAll(rawQuery, "%21", "!") // required by Matrix notification syntax

	u.RawQuery = rawQuery

	return u.String(), nil
}

func SendMatrix(ctx context.Context, config MatrixConfig, message string) error {
	shoutrrrURL, err := BuildMatrixURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Matrix URL: %w", err)
	}

	return sendShoutrrr("Matrix", shoutrrrURL, message, &shoutrrrTypes.Params{})
}
