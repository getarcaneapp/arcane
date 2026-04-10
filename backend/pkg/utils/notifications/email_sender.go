package notifications

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrSMTP "github.com/nicholas-fedor/shoutrrr/pkg/services/email/smtp"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

const (
	defaultSMTPClientHost = "localhost"
	defaultSMTPTimeout    = 10 * time.Second
)

type smtpBuildOptions struct {
	skipTLSVerify bool
	timeout       time.Duration
}

func buildSMTPConfigInternal(config models.EmailConfig, options smtpBuildOptions) (*shoutrrrSMTP.Config, error) {
	port, err := smtpPortFromConfigInternal(config.SMTPPort)
	if err != nil {
		return nil, err
	}

	smtpConfig := &shoutrrrSMTP.Config{
		Host:          config.SMTPHost,
		Port:          port,
		Username:      config.SMTPUsername,
		Password:      config.SMTPPassword,
		FromAddress:   config.FromAddress,
		ToAddresses:   config.ToAddresses,
		Auth:          shoutrrrSMTP.AuthTypes.None,
		Encryption:    shoutrrrSMTP.EncMethods.None,
		UseStartTLS:   false,
		UseHTML:       true,
		ClientHost:    defaultSMTPClientHost,
		Timeout:       smtpTimeoutFromOptionsInternal(options),
		SkipTLSVerify: options.skipTLSVerify,
	}

	if config.SMTPUsername != "" || config.SMTPPassword != "" {
		smtpConfig.Auth = shoutrrrSMTP.AuthTypes.Plain
	}

	switch config.TLSMode {
	case models.EmailTLSModeNone:
		smtpConfig.Encryption = shoutrrrSMTP.EncMethods.None
		smtpConfig.UseStartTLS = false
	case models.EmailTLSModeStartTLS:
		smtpConfig.Encryption = shoutrrrSMTP.EncMethods.Auto
		smtpConfig.UseStartTLS = true
		smtpConfig.RequireStartTLS = true
	case models.EmailTLSModeSSL:
		smtpConfig.Encryption = shoutrrrSMTP.EncMethods.ImplicitTLS
	default:
		smtpConfig.Encryption = shoutrrrSMTP.EncMethods.None
		smtpConfig.UseStartTLS = false
	}

	return smtpConfig, nil
}

func smtpPortFromConfigInternal(port int) (uint16, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid SMTP port: %d", port)
	}

	return uint16(port), nil
}

func smtpTimeoutFromOptionsInternal(options smtpBuildOptions) time.Duration {
	if options.timeout > 0 {
		return options.timeout
	}

	return defaultSMTPTimeout
}

func smtpBuildOptionsFromContextInternal(ctx context.Context) smtpBuildOptions {
	options := smtpBuildOptions{}
	if ctx == nil {
		return options
	}

	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout > 0 && timeout < defaultSMTPTimeout {
			options.timeout = timeout
		}
	}

	return options
}

func buildSMTPURLInternal(config models.EmailConfig, options smtpBuildOptions) (string, error) {
	smtpConfig, err := buildSMTPConfigInternal(config, options)
	if err != nil {
		return "", fmt.Errorf("failed to build SMTP config: %w", err)
	}

	u := smtpConfig.GetURL()
	if u == nil {
		return "", fmt.Errorf("failed to build SMTP config URL")
	}

	parsedURL, err := url.Parse(u.String())
	if err != nil {
		return "", fmt.Errorf("failed to parse SMTP config URL: %w", err)
	}

	q := parsedURL.Query()
	if q.Get("fromname") == "" {
		q.Del("fromname")
	}
	if q.Get("subject") == "" {
		q.Del("subject")
	}
	parsedURL.RawQuery = q.Encode()

	return parsedURL.String(), nil
}

// SendEmail sends pre-rendered HTML via Shoutrrr
func SendEmail(ctx context.Context, config models.EmailConfig, subject, htmlBody string) error {
	return sendEmailInternal(ctx, config, subject, htmlBody, smtpBuildOptionsFromContextInternal(ctx))
}

func sendEmailInternal(ctx context.Context, config models.EmailConfig, subject, htmlBody string, options smtpBuildOptions) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("email send canceled: %w", err)
		}
	}

	shoutrrrURL, err := buildSMTPURLInternal(config, options)
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
