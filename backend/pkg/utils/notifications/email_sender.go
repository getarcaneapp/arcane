package notifications

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/wneessen/go-mail"
	kit "go.getarcane.app/kit/pkg"
)

const (
	defaultSMTPClientHost = "localhost"
	defaultSMTPTimeout    = 10 * time.Second
)

type smtpBuildOptions struct {
	tlsConfig *tls.Config
	timeout   time.Duration
}

func smtpBuildOptionsFromContext(ctx context.Context) smtpBuildOptions {
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

func smtpAuthTypeFromMode(mode EmailAuthMode) mail.SMTPAuthType {
	switch mode {
	case EmailAuthModeNone:
		return mail.SMTPAuthNoAuth
	case EmailAuthModeAuto:
		return mail.SMTPAuthAutoDiscover
	case EmailAuthModePlain:
		return mail.SMTPAuthPlain
	case EmailAuthModeLogin:
		return mail.SMTPAuthLogin
	case EmailAuthModeCRAMMD5:
		return mail.SMTPAuthCramMD5
	default:
		return mail.SMTPAuthAutoDiscover
	}
}

func buildMailClient(config EmailConfig, options smtpBuildOptions) (*mail.Client, error) {
	if config.SMTPHost == "" {
		return nil, errors.New("SMTP host is empty")
	}
	if config.SMTPPort < 1 || config.SMTPPort > 65535 {
		return nil, fmt.Errorf("invalid SMTP port: %d", config.SMTPPort)
	}

	opts := []mail.Option{
		mail.WithPort(config.SMTPPort),
		mail.WithTimeout(kit.Ternary(options.timeout > 0, options.timeout, defaultSMTPTimeout)),
		mail.WithHELO(defaultSMTPClientHost),
	}

	switch config.TLSMode {
	case EmailTLSModeNone:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	case EmailTLSModeStartTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case EmailTLSModeSSL:
		opts = append(opts, mail.WithSSL())
	default:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	}

	if options.tlsConfig != nil {
		opts = append(opts, mail.WithTLSConfig(options.tlsConfig))
	}

	if config.AuthMode != EmailAuthModeNone && config.SMTPUsername != "" && config.SMTPPassword != "" {
		opts = append(opts,
			mail.WithSMTPAuth(smtpAuthTypeFromMode(config.AuthMode)),
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
func SendEmail(ctx context.Context, config EmailConfig, subject, htmlBody, textBody string) error {
	return sendEmail(ctx, config, subject, htmlBody, textBody, smtpBuildOptionsFromContext(ctx))
}

func sendEmail(ctx context.Context, config EmailConfig, subject, htmlBody, textBody string, options smtpBuildOptions) error {
	if ctx == nil {
		return errors.New("email send context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("email send canceled: %w", err)
	}

	if config.FromAddress == "" {
		return errors.New("from address is required")
	}
	if len(config.ToAddresses) == 0 {
		return errors.New("at least one recipient is required")
	}

	client, err := buildMailClient(config, options)
	if err != nil {
		return fmt.Errorf("failed to build SMTP client: %w", err)
	}

	msg := mail.NewMsg()
	if fromErr := msg.From(config.FromAddress); fromErr != nil {
		return fmt.Errorf("invalid from address %q: %w", config.FromAddress, fromErr)
	}
	if toErr := msg.To(config.ToAddresses...); toErr != nil {
		return fmt.Errorf("invalid recipient address(es): %w", toErr)
	}
	msg.Subject(subject)
	if textBody != "" {
		msg.SetBodyString(mail.TypeTextPlain, textBody)
		msg.AddAlternativeString(mail.TypeTextHTML, htmlBody)
	} else {
		msg.SetBodyString(mail.TypeTextHTML, htmlBody)
	}

	if dialAndSendWithContextErr := client.DialAndSendWithContext(ctx, msg); dialAndSendWithContextErr != nil {
		return fmt.Errorf("failed to send email: %w", dialAndSendWithContextErr)
	}

	return nil
}
