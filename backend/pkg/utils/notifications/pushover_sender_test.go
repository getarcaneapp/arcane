package notifications

import (
	"net/url"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPushoverURL(t *testing.T) {
	tests := []struct {
		name    string
		config  models.PushoverConfig
		wantURL string
		wantErr string
		check   func(*url.URL)
	}{
		{
			name: "basic config",
			config: models.PushoverConfig{
				Token: "token123",
				User:  "userKey",
			},
			wantURL: "pushover://:token123@userKey",
		},
		{
			name: "config with devices priority and title",
			config: models.PushoverConfig{
				Token:    "token123",
				User:     "userKey",
				Devices:  []string{"device1", "device2"},
				Priority: 1,
				Title:    "Container Update",
			},
			check: func(u *url.URL) {
				password, ok := u.User.Password()
				require.True(t, ok)
				assert.Equal(t, "token123", password)
				assert.Equal(t, "userKey", u.Host)

				q := u.Query()
				assert.Equal(t, "device1,device2", q.Get("devices"))
				assert.Equal(t, "1", q.Get("priority"))
				assert.Equal(t, "Container Update", q.Get("title"))
			},
		},
		{
			name: "missing token",
			config: models.PushoverConfig{
				User: "userKey",
			},
			wantErr: "pushover token is required",
		},
		{
			name: "missing user",
			config: models.PushoverConfig{
				Token: "token123",
			},
			wantErr: "pushover user key is required",
		},
		{
			name: "invalid priority",
			config: models.PushoverConfig{
				Token:    "token123",
				User:     "userKey",
				Priority: 3,
			},
			wantErr: "pushover priority must be between -2 and 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildPushoverURL(tt.config)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.wantURL != "" {
				assert.Equal(t, tt.wantURL, gotURL)
			}
			if tt.check != nil {
				parsed, parseErr := url.Parse(gotURL)
				require.NoError(t, parseErr)
				tt.check(parsed)
			}
		})
	}
}
