package notifications

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildShoutrrrURL_Email(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		want    string
		wantErr bool
		wantErrContains string
	}{
		{
			name: "valid single recipient",
			config: map[string]interface{}{
				"smtpHost":    "smtp.example.com",
				"smtpPort":    587.0,
				"toAddresses": "test@example.com",
				"fromAddress": "from@example.com",
			},
			want: "smtp://smtp.example.com:587/?from=from%40example.com&toaddresses=test%40example.com",
		},
		{
			name: "valid multiple recipients with spaces",
			config: map[string]interface{}{
				"smtpHost":    "smtp.example.com",
				"smtpPort":    587.0,
				"toAddresses": "test1@example.com, test2@example.com ",
				"fromAddress": "from@example.com",
			},
			want: "smtp://smtp.example.com:587/?from=from%40example.com&toaddresses=test1%40example.com%2Ctest2%40example.com",
		},
		{
			name: "invalid to address",
			config: map[string]interface{}{
				"smtpHost":    "smtp.example.com",
				"smtpPort":    587.0,
				"toAddresses": "invalid-email",
				"fromAddress": "from@example.com",
			},
			wantErr: true,
		},
		{
			name: "invalid to address - missing commas",
			config: map[string]interface{}{
				"smtpHost":    "smtp.example.com",
				"smtpPort":    587.0,
				"toAddresses": "test1@example.com test2@example.com",
				"fromAddress": "from@example.com",
			},
			wantErr:         true,
			wantErrContains: "separate multiple email addresses with commas",
		},
		{
			name: "invalid from address",
			config: map[string]interface{}{
				"smtpHost":    "smtp.example.com",
				"smtpPort":    587.0,
				"toAddresses": "test@example.com",
				"fromAddress": "invalid-from",
			},
			wantErr: true,
		},
		{
			name: "empty to address after split",
			config: map[string]interface{}{
				"smtpHost":    "smtp.example.com",
				"smtpPort":    587.0,
				"toAddresses": " , ",
				"fromAddress": "from@example.com",
			},
			wantErr: true,
		},
		{
			name: "with skipTLSVerify enabled",
			config: map[string]interface{}{
				"smtpHost":      "smtp.example.com",
				"smtpPort":      587.0,
				"toAddresses":   "test@example.com",
				"fromAddress":   "from@example.com",
				"skipTLSVerify": true,
			},
			want: "smtp://smtp.example.com:587/?from=from%40example.com&skiptlsverify=yes&toaddresses=test%40example.com",
		},
		{
			name: "with skipTLSVerify disabled",
			config: map[string]interface{}{
				"smtpHost":      "smtp.example.com",
				"smtpPort":      587.0,
				"toAddresses":   "test@example.com",
				"fromAddress":   "from@example.com",
				"skipTLSVerify": false,
			},
			want: "smtp://smtp.example.com:587/?from=from%40example.com&toaddresses=test%40example.com",
		},
		{
			name: "with TLS mode and skipTLSVerify",
			config: map[string]interface{}{
				"smtpHost":      "smtp.example.com",
				"smtpPort":      587.0,
				"toAddresses":   "test@example.com",
				"fromAddress":   "from@example.com",
				"tlsMode":       "starttls",
				"skipTLSVerify": true,
			},
			want: "smtp://smtp.example.com:587/?from=from%40example.com&skiptlsverify=yes&toaddresses=test%40example.com&usestarttls=yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildShoutrrrURL("email", tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					require.Contains(t, err.Error(), tt.wantErrContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestBuildShoutrrrURL_Slack(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		want    string
		wantErr bool
	}{
		{
			name: "valid slack webhook",
			config: map[string]interface{}{
				"webhookUrl": "https://hooks.slack.com/services/T123/B456/X789",
			},
			want: "slack://T123/B456/X789",
		},
		{
			name: "valid slack webhook with trailing slash",
			config: map[string]interface{}{
				"webhookUrl": "https://hooks.slack.com/services/T123/B456/X789/",
			},
			want: "slack://T123/B456/X789",
		},
		{
			name: "valid slack webhook with query params",
			config: map[string]interface{}{
				"webhookUrl": "https://hooks.slack.com/services/T123/B456/X789?foo=bar",
			},
			want: "slack://T123/B456/X789",
		},
		{
			name: "invalid slack webhook - missing parts",
			config: map[string]interface{}{
				"webhookUrl": "https://hooks.slack.com/services/T123/B456",
			},
			wantErr: true,
		},
		{
			name: "invalid slack webhook - not services",
			config: map[string]interface{}{
				"webhookUrl": "https://hooks.slack.com/api/T123/B456/X789",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildShoutrrrURL("slack", tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestBuildShoutrrrURL_Discord(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		want    string
		wantErr bool
	}{
		{
			name: "valid discord webhook",
			config: map[string]interface{}{
				"webhookUrl": "https://discord.com/api/webhooks/123/abc",
			},
			want: "discord://abc@123",
		},
		{
			name: "valid discord webhook with query params",
			config: map[string]interface{}{
				"webhookUrl": "https://discord.com/api/webhooks/123/abc?token=1",
			},
			want: "discord://abc@123", // If this fails, it means it's greedy
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildShoutrrrURL("discord", tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}
