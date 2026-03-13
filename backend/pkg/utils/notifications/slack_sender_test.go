package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSlackURL(t *testing.T) {
	tests := []struct {
		name    string
		config  models.SlackConfig
		wantURL string
		wantErr string
	}{
		{
			name: "basic config with bot token and channel",
			config: models.SlackConfig{
				Token:   "xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx",
				Channel: "general",
			},
			wantURL: "slack://xoxb:123456789012-123456789012-abcdefghijklmnopqrstuvwx@general",
		},
		{
			name: "config with user token",
			config: models.SlackConfig{
				Token:   "xoxp-123456789012-123456789012-abcdefghijklmnopqrstuvwx",
				Channel: "alerts",
			},
			wantURL: "slack://xoxp:123456789012-123456789012-abcdefghijklmnopqrstuvwx@alerts",
		},
		{
			name: "config with all optional fields",
			config: models.SlackConfig{
				Token:    "xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx",
				Channel:  "notifications",
				BotName:  "Arcane Bot",
				Icon:     ":robot:",
				Color:    "#FF0000",
				Title:    "Container Update",
				ThreadTS: "1234567890.123456",
			},
			wantURL: "slack://xoxb:123456789012-123456789012-abcdefghijklmnopqrstuvwx@notifications?botname=Arcane+Bot&color=%23FF0000&icon=%3Arobot%3A&thread_ts=1234567890.123456&title=Container+Update",
		},
		{
			name: "empty token",
			config: models.SlackConfig{
				Channel: "general",
			},
			wantErr: "slack token is required",
		},
		{
			name: "invalid token format",
			config: models.SlackConfig{
				Token:   "invalid-token",
				Channel: "general",
			},
			wantErr: "invalid Slack token format (expected format: xoxb-... or xoxp-...)",
		},
		{
			name: "token without prefix",
			config: models.SlackConfig{
				Token:   "1234-5678-abcd",
				Channel: "general",
			},
			wantErr: "invalid Slack token format (expected format: xoxb-... or xoxp-...)",
		},
		{
			name: "short invalid token",
			config: models.SlackConfig{
				Token:   "xoxb-1234-5678-abcd",
				Channel: "general",
			},
			wantErr: "invalid Slack token format (expected format: xoxb-... or xoxp-...)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildSlackURL(tt.config)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, gotURL)
		})
	}
}

func TestBuildSlackURL_TokenParsing(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid bot token",
			token:   "xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx",
			wantErr: false,
		},
		{
			name:    "valid user token",
			token:   "xoxp-123456789012-123456789012-abcdefghijklmnopqrstuvwx",
			wantErr: false,
		},
		{
			name:    "token with xoxa prefix is also accepted",
			token:   "xoxa-123456789012-123456789012-abcdefghijklmnopqrstuvwx",
			wantErr: false,
		},
		{
			name:    "token without dashes",
			token:   "xoxb1234567890",
			wantErr: true,
		},
		{
			name:    "short token",
			token:   "xoxb-1234-5678-abcd",
			wantErr: true,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.SlackConfig{
				Token:   tt.token,
				Channel: "test",
			}

			_, err := BuildSlackURL(config)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
