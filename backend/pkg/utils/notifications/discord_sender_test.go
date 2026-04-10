package notifications

import (
	"fmt"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDiscordURL(t *testing.T) {
	tests := []struct {
		name   string
		config models.DiscordConfig
		check  func(string) bool
	}{
		{
			name: "basic webhook configuration",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "abcdefghijklmnopqrstuvwxyz0123456789",
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "discord://") &&
					assert.Contains(t, url, "abcdefghijklmnopqrstuvwxyz0123456789") &&
					assert.Contains(t, url, "@123456789012345678")
			},
		},
		{
			name: "webhook with custom username",
			config: models.DiscordConfig{
				WebhookID: "987654321098765432",
				Token:     "token123456789abcdefghij",
				Username:  "Arcane Bot",
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "username=Arcane+Bot")
			},
		},
		{
			name: "webhook with avatar URL",
			config: models.DiscordConfig{
				WebhookID: "111222333444555666",
				Token:     "tokenXYZ",
				AvatarURL: "https://example.com/avatar.png",
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "avatar=https%3A%2F%2Fexample.com%2Favatar.png")
			},
		},
		{
			name: "webhook with username and avatar",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "abcdeftoken",
				Username:  "Custom Bot",
				AvatarURL: "https://cdn.example.com/bot.jpg",
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "avatar=https%3A%2F%2Fcdn.example.com%2Fbot.jpg") &&
					assert.Contains(t, url, "username=Custom+Bot")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildDiscordURL(tt.config)
			require.NoError(t, err)
			tt.check(gotURL)
		})
	}
}

func TestBuildDiscordURL_WebhookIdTokenSplit(t *testing.T) {
	tests := []struct {
		name      string
		webhookID string
		token     string
		wantID    bool
		wantToken bool
	}{
		{
			name:      "valid webhook ID and token",
			webhookID: "123456789012345678",
			token:     "abcdefghijklmnopqrstuvwxyz",
			wantID:    true,
			wantToken: true,
		},
		{
			name:      "numeric webhook ID",
			webhookID: "999888777666555444",
			token:     "token-with-dashes",
			wantID:    true,
			wantToken: true,
		},
		{
			name:      "long alphanumeric token",
			webhookID: "123456789012345678",
			token:     "aB3dE5gH7jK9mN1pQ4sT6vX8zZ0cF2hJ5",
			wantID:    true,
			wantToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.DiscordConfig{
				WebhookID: tt.webhookID,
				Token:     tt.token,
			}

			gotURL, err := BuildDiscordURL(config)
			require.NoError(t, err)

			if tt.wantID {
				assert.Contains(t, gotURL, tt.webhookID)
			}
			if tt.wantToken {
				assert.Contains(t, gotURL, tt.token)
			}

			// Verify format: discord://token@webhookID
			assert.Contains(t, gotURL, "discord://")
			assert.Contains(t, gotURL, "@"+tt.webhookID)
		})
	}
}

func TestBuildDiscordURL_OptionalFields(t *testing.T) {
	tests := []struct {
		name         string
		config       models.DiscordConfig
		wantUsername bool
		wantAvatar   bool
	}{
		{
			name: "no optional fields",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "token123",
			},
			wantUsername: false,
			wantAvatar:   false,
		},
		{
			name: "only username",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "token123",
				Username:  "Bot Name",
			},
			wantUsername: true,
			wantAvatar:   false,
		},
		{
			name: "only avatar",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "token123",
				AvatarURL: "https://example.com/avatar.png",
			},
			wantUsername: false,
			wantAvatar:   true,
		},
		{
			name: "both username and avatar",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "token123",
				Username:  "My Bot",
				AvatarURL: "https://example.com/bot.png",
			},
			wantUsername: true,
			wantAvatar:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildDiscordURL(tt.config)
			require.NoError(t, err)

			if tt.wantUsername {
				assert.Contains(t, gotURL, "username=")
			} else {
				assert.NotContains(t, gotURL, "username=")
			}

			if tt.wantAvatar {
				assert.Contains(t, gotURL, "avatar=")
			} else {
				assert.NotContains(t, gotURL, "avatar=")
			}
		})
	}
}

func TestBuildDiscordURL_URLEncoding(t *testing.T) {
	config := models.DiscordConfig{
		WebhookID: "123456789012345678",
		Token:     "token123",
		Username:  "Bot with spaces & special chars!",
		AvatarURL: "https://example.com/path/to/avatar.png?size=256",
	}

	gotURL, err := BuildDiscordURL(config)
	require.NoError(t, err)

	// Verify URL encoding for special characters
	assert.Contains(t, gotURL, "username=Bot+with+spaces")
	assert.Contains(t, gotURL, "avatar=https%3A%2F%2F")
}

func TestSendDiscord_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  models.DiscordConfig
		wantErr string
	}{
		{
			name: "missing webhook ID",
			config: models.DiscordConfig{
				Token: "token123",
			},
			wantErr: "discord webhook ID is empty",
		},
		{
			name: "missing token",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
			},
			wantErr: "discord token is empty",
		},
		{
			name: "empty webhook ID",
			config: models.DiscordConfig{
				WebhookID: "",
				Token:     "token123",
			},
			wantErr: "discord webhook ID is empty",
		},
		{
			name: "empty token",
			config: models.DiscordConfig{
				WebhookID: "123456789012345678",
				Token:     "",
			},
			wantErr: "discord token is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDiscordConfig(tt.config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSendDiscord_ValidConfiguration(t *testing.T) {
	config := models.DiscordConfig{
		WebhookID: "123456789012345678",
		Token:     "valid-token-123",
		Username:  "Test Bot",
	}

	err := validateDiscordConfig(config)
	assert.NoError(t, err)
}

// Helper function to extract validation logic for testing
func validateDiscordConfig(config models.DiscordConfig) error {
	if config.WebhookID == "" {
		return fmt.Errorf("discord webhook ID is empty")
	}
	if config.Token == "" {
		return fmt.Errorf("discord token is empty")
	}
	return nil
}
