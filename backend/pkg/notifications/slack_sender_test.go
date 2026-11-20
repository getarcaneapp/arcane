package notifications

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendSlack_RejectsInvalidToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr string
	}{
		{
			name:    "empty token",
			wantErr: "slack token is empty",
		},
		{
			name:    "invalid token format",
			token:   "invalid-token",
			wantErr: "invalid Slack token format",
		},
		{
			name:    "token without prefix",
			token:   "1234-5678-abcd",
			wantErr: "invalid Slack token format",
		},
		{
			name:    "short invalid token",
			token:   "xoxb-1234-5678-abcd",
			wantErr: "invalid Slack token format",
		},
		{
			name:    "token without dashes",
			token:   "xoxb1234567890",
			wantErr: "invalid Slack token format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SendSlack(t.Context(), SlackConfig{
				Token:   tt.token,
				Channel: "#general",
			}, "message")

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
