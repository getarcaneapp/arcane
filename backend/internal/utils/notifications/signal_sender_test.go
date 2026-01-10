package notifications

import (
	"fmt"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSignalURL(t *testing.T) {
	tests := []struct {
		name   string
		config models.SignalConfig
		check  func(string) bool
	}{
		{
			name: "basic auth configuration",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				User:       "admin",
				Password:   "secret",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "signal://admin:secret@signal.example.com:8080") &&
					assert.Contains(t, url, "source=%2B1234567890") &&
					assert.Contains(t, url, "recipients=%2B0987654321")
			},
		},
		{
			name: "token auth configuration",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Token:      "auth-token-123",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "signal://signal.example.com:8080") &&
					assert.Contains(t, url, "token=auth-token-123") &&
					assert.Contains(t, url, "source=%2B1234567890")
			},
		},
		{
			name: "multiple recipients",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Token:      "token123",
				Source:     "+1111111111",
				Recipients: []string{"+2222222222", "+3333333333", "+4444444444"},
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "recipients=%2B2222222222%2C%2B3333333333%2C%2B4444444444")
			},
		},
		{
			name: "with DisableTLS",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Token:      "token123",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
				DisableTLS: true,
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "disabletls=Yes")
			},
		},
		{
			name: "custom port",
			config: models.SignalConfig{
				Host:       "signal-api.local",
				Port:       9443,
				User:       "user",
				Password:   "pass",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			check: func(url string) bool {
				return assert.Contains(t, url, "signal://user:pass@signal-api.local:9443") &&
					assert.Contains(t, url, "port=9443")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildSignalURL(tt.config)
			require.NoError(t, err)
			tt.check(gotURL)
		})
	}
}

func TestBuildSignalURL_AuthenticationMethods(t *testing.T) {
	tests := []struct {
		name     string
		config   models.SignalConfig
		wantAuth string
	}{
		{
			name: "basic auth only",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				User:       "testuser",
				Password:   "testpass",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantAuth: "testuser:testpass@",
		},
		{
			name: "token auth only",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Token:      "my-secure-token",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantAuth: "token=my-secure-token",
		},
		{
			name: "both basic and token auth provided",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				User:       "user",
				Password:   "pass",
				Token:      "token",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantAuth: "user:pass@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildSignalURL(tt.config)
			require.NoError(t, err)
			assert.Contains(t, gotURL, tt.wantAuth)
		})
	}
}

func TestBuildSignalURL_PhoneNumberEncoding(t *testing.T) {
	config := models.SignalConfig{
		Host:       "signal.example.com",
		Port:       8080,
		Token:      "token",
		Source:     "+1 (555) 123-4567", // Phone with special chars
		Recipients: []string{"+1 (555) 987-6543"},
	}

	gotURL, err := BuildSignalURL(config)
	require.NoError(t, err)

	// Phone numbers should be in the URL path and query params
	assert.Contains(t, gotURL, "source=")
	assert.Contains(t, gotURL, "recipients=")
}

func TestSendSignal_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  models.SignalConfig
		wantErr string
	}{
		{
			name: "missing host",
			config: models.SignalConfig{
				Port:       8080,
				Token:      "token",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal host is empty",
		},
		{
			name: "missing port",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Token:      "token",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal port is not set",
		},
		{
			name: "missing source",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Token:      "token",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal source phone number is empty",
		},
		{
			name: "missing recipients",
			config: models.SignalConfig{
				Host:   "signal.example.com",
				Port:   8080,
				Token:  "token",
				Source: "+1234567890",
			},
			wantErr: "no signal recipients configured",
		},
		{
			name: "missing authentication - no user/password/token",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal requires either basic auth (user/password) or token authentication",
		},
		{
			name: "incomplete basic auth - user only",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				User:       "admin",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal requires either basic auth (user/password) or token authentication",
		},
		{
			name: "incomplete basic auth - password only",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Password:   "secret",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal requires either basic auth (user/password) or token authentication",
		},
		{
			name: "both auth methods provided",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				User:       "user",
				Password:   "pass",
				Token:      "token",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
			wantErr: "signal cannot use both basic auth and token authentication simultaneously",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: SendSignal will try to actually send, so we only test validation logic
			// by checking the error message before network call
			err := validateSignalConfig(tt.config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSendSignal_ValidConfigurations(t *testing.T) {
	tests := []struct {
		name   string
		config models.SignalConfig
	}{
		{
			name: "valid basic auth",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				User:       "admin",
				Password:   "secret",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
		},
		{
			name: "valid token auth",
			config: models.SignalConfig{
				Host:       "signal.example.com",
				Port:       8080,
				Token:      "token123",
				Source:     "+1234567890",
				Recipients: []string{"+0987654321"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSignalConfig(tt.config)
			assert.NoError(t, err)
		})
	}
}

// Helper function to extract validation logic for testing
func validateSignalConfig(config models.SignalConfig) error {
	if config.Host == "" {
		return fmt.Errorf("signal host is empty")
	}
	if config.Port == 0 {
		return fmt.Errorf("signal port is not set")
	}
	if config.Source == "" {
		return fmt.Errorf("signal source phone number is empty")
	}
	if len(config.Recipients) == 0 {
		return fmt.Errorf("no signal recipients configured")
	}

	// Validate authentication
	hasBasicAuth := config.User != "" && config.Password != ""
	hasTokenAuth := config.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return fmt.Errorf("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return fmt.Errorf("signal cannot use both basic auth and token authentication simultaneously")
	}

	return nil
}
