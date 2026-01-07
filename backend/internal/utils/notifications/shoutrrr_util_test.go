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
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}
