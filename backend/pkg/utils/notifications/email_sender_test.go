package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSMTPURL(t *testing.T) {
	tests := []struct {
		name    string
		config  models.EmailConfig
		wantURL string
	}{
		{
			name: "Basic SMTP no auth, no TLS",
			config: models.EmailConfig{
				SMTPHost:    "smtp.example.com",
				SMTPPort:    25,
				FromAddress: "from@example.com",
				ToAddresses: []string{"to@example.com"},
				TLSMode:     models.EmailTLSModeNone,
			},
			wantURL: "smtp://smtp.example.com:25/?encryption=None&fromAddress=from%40example.com&toAddresses=to%40example.com&useHTML=yes&useStartTLS=no",
		},
		{
			name: "SMTP with auth and starttls",
			config: models.EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     587,
				SMTPUsername: "user",
				SMTPPassword: "password",
				FromAddress:  "from@example.com",
				ToAddresses:  []string{"to1@example.com", "to2@example.com"},
				TLSMode:      models.EmailTLSModeStartTLS,
			},
			wantURL: "smtp://user:password@smtp.example.com:587/?encryption=ExplicitTLS&fromAddress=from%40example.com&toAddresses=to1%40example.com%2Cto2%40example.com&useHTML=yes&useStartTLS=yes",
		},
		{
			name: "SMTP with SSL/TLS and special characters in credentials",
			config: models.EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     465,
				SMTPUsername: "user@example.com",
				SMTPPassword: "pass/word!",
				FromAddress:  "from@example.com",
				ToAddresses:  []string{"to@example.com"},
				TLSMode:      models.EmailTLSModeSSL,
			},
			wantURL: "smtp://user%40example.com:pass%2Fword%21@smtp.example.com:465/?encryption=ImplicitTLS&fromAddress=from%40example.com&toAddresses=to%40example.com&useHTML=yes&useStartTLS=no",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildSMTPURL(tt.config)
			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, gotURL)
		})
	}
}
