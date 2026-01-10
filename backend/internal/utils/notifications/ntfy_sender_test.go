package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildNtfyURL(t *testing.T) {
	tests := []struct {
		name    string
		config  models.NtfyConfig
		wantErr bool
		check   func(string) bool
	}{
		{
			name: "basic config with default host",
			config: models.NtfyConfig{
				Topic:    "test-topic",
				Cache:    true,
				Firebase: true,
			},
			wantErr: false,
			check: func(url string) bool {
				return url == "ntfy://ntfy.sh/test-topic?cache=yes&firebase=yes"
			},
		},
		{
			name: "config with custom host",
			config: models.NtfyConfig{
				Host:     "ntfy.example.com",
				Topic:    "alerts",
				Cache:    true,
				Firebase: true,
			},
			wantErr: false,
			check: func(url string) bool {
				return url == "ntfy://ntfy.example.com/alerts?cache=yes&firebase=yes"
			},
		},
		{
			name: "config with port",
			config: models.NtfyConfig{
				Host:     "ntfy.example.com",
				Port:     8080,
				Topic:    "updates",
				Cache:    true,
				Firebase: true,
			},
			wantErr: false,
			check: func(url string) bool {
				return url == "ntfy://ntfy.example.com:8080/updates?cache=yes&firebase=yes"
			},
		},
		{
			name: "config with auth",
			config: models.NtfyConfig{
				Host:     "ntfy.example.com",
				Port:     443,
				Topic:    "private",
				Username: "user",
				Password: "pass",
				Cache:    true,
				Firebase: true,
			},
			wantErr: false,
			check: func(url string) bool {
				return url == "ntfy://user:pass@ntfy.example.com:443/private?cache=yes&firebase=yes"
			},
		},
		{
			name: "config with priority and tags",
			config: models.NtfyConfig{
				Host:     "ntfy.sh",
				Topic:    "alerts",
				Priority: "high",
				Tags:     []string{"warning", "server"},
				Cache:    true,
				Firebase: true,
			},
			wantErr: false,
			check: func(url string) bool {
				return url == "ntfy://ntfy.sh/alerts?cache=yes&firebase=yes&priority=high&tags=warning%2Cserver"
			},
		},
		{
			name: "missing topic",
			config: models.NtfyConfig{
				Host: "ntfy.sh",
			},
			wantErr: true,
		},
		{
			name: "config with all options",
			config: models.NtfyConfig{
				Host:                   "ntfy.example.com",
				Port:                   8080,
				Topic:                  "test",
				Username:               "user",
				Password:               "pass",
				Priority:               "max",
				Tags:                   []string{"urgent"},
				Icon:                   "https://example.com/icon.png",
				Cache:                  false,
				Firebase:               false,
				DisableTLSVerification: true,
			},
			wantErr: false,
			check: func(url string) bool {
				return url == "ntfy://user:pass@ntfy.example.com:8080/test?cache=no&disabletls=yes&firebase=no&icon=https%3A%2F%2Fexample.com%2Ficon.png&priority=max&tags=urgent"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildNtfyURL(tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.True(t, tt.check(gotURL), "URL mismatch: %s", gotURL)
			}
		})
	}
}
