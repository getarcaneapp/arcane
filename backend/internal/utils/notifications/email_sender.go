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

// BuildSMTPURL converts EmailConfig to Shoutrrr URL format
// URL example: smtp://user:pass@host:port/?fromAddress=...&toAddresses=...&useHTML=yes
func BuildSMTPURL(config models.EmailConfig) (string, error) {
	u := &url.URL{
		Scheme: "smtp",
		Host:   fmt.Sprintf("%s:%d", config.SMTPHost, config.SMTPPort),
		Path:   "/",
	}

	if config.SMTPUsername != "" || config.SMTPPassword != "" {
		u.User = url.UserPassword(config.SMTPUsername, config.SMTPPassword)
	}

	q := u.Query()
	q.Set("fromAddress", config.FromAddress)
	q.Set("toAddresses", strings.Join(config.ToAddresses, ","))
	q.Set("useHTML", "yes")

	// TLS Mode Mapping
	// none -> encryption=None, useStartTLS=no
	// starttls -> encryption=ExplicitTLS, useStartTLS=yes
	// ssl -> encryption=ImplicitTLS, useStartTLS=no
	switch config.TLSMode {
	case models.EmailTLSModeNone:
		q.Set("encryption", "None")
		q.Set("useStartTLS", "no")
	case models.EmailTLSModeStartTLS:
		q.Set("encryption", "ExplicitTLS")
		q.Set("useStartTLS", "yes")
	case models.EmailTLSModeSSL:
		q.Set("encryption", "ImplicitTLS")
		q.Set("useStartTLS", "no")
	default:
		q.Set("encryption", "None")
		q.Set("useStartTLS", "no")
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendEmail sends pre-rendered HTML via Shoutrrr
func SendEmail(ctx context.Context, config models.EmailConfig, subject, htmlBody string) error {
	shoutrrrURL, err := BuildSMTPURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr sender: %w", err)
	}

	params := shoutrrrTypes.Params{
		"subject": subject,
	}

	errs := sender.Send(htmlBody, &params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send email via shoutrrr: %w", err)
		}
	}
	return nil
}
