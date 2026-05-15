package notifications

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/wneessen/go-mail"
)

const (
	defaultSMTPClientHost = "localhost"
	defaultSMTPTimeout    = 10 * time.Second
)

type smtpBuildOptions struct {
	tlsConfig *tls.Config
	timeout   time.Duration
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

func smtpAuthTypeFromModeInternal(mode models.EmailAuthMode) mail.SMTPAuthType {
	switch mode {
	case models.EmailAuthModeAuto:
		return mail.SMTPAuthAutoDiscover
	case models.EmailAuthModePlain:
		return mail.SMTPAuthPlain
	case models.EmailAuthModeLogin:
		return mail.SMTPAuthLogin
	case models.EmailAuthModeCRAMMD5:
		return mail.SMTPAuthCramMD5
	default:
		return mail.SMTPAuthAutoDiscover
	}
}

func buildMailClientInternal(config models.EmailConfig, options smtpBuildOptions) (*mail.Client, error) {
	if config.SMTPHost == "" {
		return nil, fmt.Errorf("SMTP host is empty")
	}
	if config.SMTPPort < 1 || config.SMTPPort > 65535 {
		return nil, fmt.Errorf("invalid SMTP port: %d", config.SMTPPort)
	}

	opts := []mail.Option{
		mail.WithPort(config.SMTPPort),
		mail.WithTimeout(smtpTimeoutFromOptionsInternal(options)),
		mail.WithHELO(defaultSMTPClientHost),
	}

	switch config.TLSMode {
	case models.EmailTLSModeNone:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	case models.EmailTLSModeStartTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case models.EmailTLSModeSSL:
		opts = append(opts, mail.WithSSL())
	default:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	}

	if options.tlsConfig != nil {
		opts = append(opts, mail.WithTLSConfig(options.tlsConfig))
	}

	if config.SMTPUsername != "" || config.SMTPPassword != "" {
		opts = append(opts,
			mail.WithSMTPAuth(smtpAuthTypeFromModeInternal(config.AuthMode)),
			mail.WithUsername(config.SMTPUsername),
			mail.WithPassword(config.SMTPPassword),
		)
	} else {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthNoAuth))
	}

	client, err := mail.NewClient(config.SMTPHost, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to construct SMTP client: %w", err)
	}

	return client, nil
}

// SendEmail sends pre-rendered HTML via go-mail.
func SendEmail(ctx context.Context, config models.EmailConfig, subject, htmlBody string) error {
	return sendEmailInternal(ctx, config, subject, htmlBody, smtpBuildOptionsFromContextInternal(ctx))
}

func sendEmailInternal(ctx context.Context, config models.EmailConfig, subject, htmlBody string, options smtpBuildOptions) error {
	if ctx == nil {
		return fmt.Errorf("email send context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("email send canceled: %w", err)
	}

	if config.FromAddress == "" {
		return fmt.Errorf("from address is required")
	}
	if len(config.ToAddresses) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	client, err := buildMailClientInternal(config, options)
	if err != nil {
		return fmt.Errorf("failed to build SMTP client: %w", err)
	}

	msg := mail.NewMsg()
	if err := msg.From(config.FromAddress); err != nil {
		return fmt.Errorf("invalid from address %q: %w", config.FromAddress, err)
	}
	if err := msg.To(config.ToAddresses...); err != nil {
		return fmt.Errorf("invalid recipient address(es): %w", err)
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextHTML, htmlBody)

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}
