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

// BuildMatrixURL converts MatrixConfig to Shoutrrr URL format
// URL example: matrix://user:password@host[:port]/[?rooms=!roomID1[,roomAlias2]][&disableTLS=yes]
func BuildMatrixURL(config models.MatrixConfig) (string, error) {
	if config.Host == "" {
		return "", fmt.Errorf("matrix host is required")
	}

	// Build the base URL
	u := &url.URL{
		Scheme: "matrix",
	}

	// Add authentication if provided (username can be empty)
	if config.Password != "" {
		u.User = url.UserPassword(config.Username, config.Password)
	}

	host := config.Host
	// Set host and port
	if config.Port > 0 {
		u.Host = fmt.Sprintf("%s:%d", host, config.Port)
	} else {
		u.Host = host
	}

	// Add query parameters
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

// SendMatrix sends a message via Shoutrrr Matrix using proper service configuration
func SendMatrix(ctx context.Context, config models.MatrixConfig, message string) error {
	shoutrrrURL, err := BuildMatrixURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Matrix URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Matrix sender: %w", err)
	}

	params := &shoutrrrTypes.Params{}

	errs := sender.Send(message, params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Matrix message via shoutrrr: %w", err)
		}
	}
	return nil
}
