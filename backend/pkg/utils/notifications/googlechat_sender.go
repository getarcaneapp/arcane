package notifications

import (
	"context"
	"net/url"
	"strings"

	"emperror.dev/errors"

	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// BuildGoogleChatURL converts GoogleChatConfig to Shoutrrr URL format by
// swapping the incoming webhook URL's scheme to googlechat while preserving
// the host, path and the key/token query parameters.
func BuildGoogleChatURL(config GoogleChatConfig) (string, error) {
	if config.WebhookURL == "" {
		return "", errors.New("google chat webhook URL is empty")
	}

	parsed, err := url.Parse(config.WebhookURL)
	if err != nil {
		return "", errors.WrapIf(err, "invalid google chat webhook URL")
	}

	// Accept a bare host without scheme, mirroring the generic webhook's URL
	// normalisation.
	if parsed.Host == "" && !strings.Contains(config.WebhookURL, "://") {
		normalized := strings.TrimPrefix(config.WebhookURL, "//")
		parsed, err = url.Parse("https://" + normalized)
		if err != nil {
			return "", errors.WrapIf(err, "invalid google chat webhook URL")
		}
	}

	if parsed.Host == "" {
		return "", errors.New("invalid google chat webhook URL: missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", errors.Errorf("invalid google chat webhook URL scheme: %s", parsed.Scheme)
	}

	shoutrrrURL := &url.URL{
		Scheme:   "googlechat",
		Host:     parsed.Host,
		Path:     parsed.Path,
		RawQuery: parsed.Query().Encode(),
	}

	return shoutrrrURL.String(), nil
}

// SendGoogleChat sends a message via Shoutrrr's Google Chat service. The
// service posts plain text only ({"text": ...}) and ignores params, so the
// title is expected to already be part of the message body.
func SendGoogleChat(ctx context.Context, config GoogleChatConfig, message string) error {
	shoutrrrURL, err := BuildGoogleChatURL(config)
	if err != nil {
		return errors.WrapIf(err, "failed to build shoutrrr Google Chat URL")
	}

	sender, err := shoutrrr.CreateSenderWithOptions(shoutrrrTypes.SenderOptions{}, shoutrrrURL)
	if err != nil {
		return errors.WrapIf(err, "failed to create shoutrrr Google Chat sender")
	}

	errs := sender.Send(message, &shoutrrrTypes.Params{})
	for _, err := range errs {
		if err != nil {
			return errors.WrapIf(err, "failed to send Google Chat message via shoutrrr")
		}
	}
	return nil
}
