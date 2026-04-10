package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTelegramURL(t *testing.T) {
	tests := []struct {
		name    string
		config  models.TelegramConfig
		wantURL string
	}{
		{
			name: "basic config with single chat",
			config: models.TelegramConfig{
				BotToken:     "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				ChatIDs:      []string{"@channel"},
				Preview:      true,
				Notification: true,
			},
			wantURL: "telegram://123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11@telegram?chats=%40channel",
		},
		{
			name: "config with multiple chats",
			config: models.TelegramConfig{
				BotToken:     "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				ChatIDs:      []string{"@channel1", "123456789"},
				Preview:      false,
				Notification: false,
				Title:        "Arcane",
			},
			wantURL: "telegram://123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11@telegram?chats=%40channel1%2C123456789&notification=No&preview=No&title=Arcane",
		},
		{
			name: "config with title",
			config: models.TelegramConfig{
				BotToken:     "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				ChatIDs:      []string{"@mybot"},
				Preview:      true,
				Notification: true,
				Title:        "Container Updates",
			},
			wantURL: "telegram://123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11@telegram?chats=%40mybot&title=Container+Updates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildTelegramURL(tt.config)
			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, gotURL)
		})
	}
}
