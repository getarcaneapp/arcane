package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsValidUserEmail_AllowsReportedFormats(t *testing.T) {
	t.Parallel()

	validEmails := []string{
		"user@198.51.100.32",
		"user@[IPv6:3ffe::32]",
		"user@localdomain",
		"user@website.日本",
	}

	for _, email := range validEmails {
		t.Run(email, func(t *testing.T) {
			t.Parallel()

			require.True(t, IsValidUserEmail(email),
				"expected %q to be valid", email)

		})
	}
}

func TestIsValidUserEmail_RejectsMalformedAddresses(t *testing.T) {
	t.Parallel()

	invalidEmails := []string{
		"",
		"user",
		"user@",
		"@example.com",
		"user@@example.com",
		"user..dots@example.com",
		"user@-example.com",
		"user@example..com",
		"user@[::1]",
		"user@[::ffff:1.2.3.4]",
		"user@[IPv6:not-an-ip]",
		"user@256.256.256.256",
	}

	for _, email := range invalidEmails {
		t.Run(email, func(t *testing.T) {
			t.Parallel()

			require.False(t, IsValidUserEmail(email),
				"expected %q to be invalid", email)

		})
	}
}
