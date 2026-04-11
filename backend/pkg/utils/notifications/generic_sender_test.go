package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGenericURL(t *testing.T) {
	tests := []struct {
		name    string
		config  models.GenericConfig
		wantURL string
		wantErr string
	}{
		{
			name: "basic HTTPS webhook",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=no&template=json",
		},
		{
			name: "basic HTTP webhook",
			config: models.GenericConfig{
				WebhookURL: "http://webhook.example.com/notify",
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=yes&template=json",
		},
		{
			name: "webhook without scheme defaults to HTTPS",
			config: models.GenericConfig{
				WebhookURL: "webhook.example.com/notify",
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=no&template=json",
		},
		{
			name: "webhook without scheme with port",
			config: models.GenericConfig{
				WebhookURL: "webhook.example.com:8080/notify",
			},
			wantURL: "generic://webhook.example.com:8080/notify?disabletls=no&template=json",
		},
		{
			name: "webhook without scheme with DisableTLS",
			config: models.GenericConfig{
				WebhookURL: "webhook.example.com/notify",
				DisableTLS: true,
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=yes&template=json",
		},
		{
			name: "webhook with port",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com:8443/api/notify",
			},
			wantURL: "generic://webhook.example.com:8443/api/notify?disabletls=no&template=json",
		},
		{
			name: "webhook with custom content type",
			config: models.GenericConfig{
				WebhookURL:  "https://webhook.example.com/notify",
				ContentType: "application/x-www-form-urlencoded",
			},
			wantURL: "generic://webhook.example.com/notify?contenttype=application%2Fx-www-form-urlencoded&disabletls=no&template=json",
		},
		{
			name: "webhook with POST method",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
				Method:     "POST",
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=no&method=POST&template=json",
		},
		{
			name: "webhook with custom title and message keys",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
				TitleKey:   "subject",
				MessageKey: "body",
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=no&messagekey=body&template=json&titlekey=subject",
		},
		{
			name: "webhook with DisableTLS ignored for HTTPS",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
				DisableTLS: true,
			},
			wantURL: "generic://webhook.example.com/notify?disabletls=no&template=json",
		},
		{
			name: "webhook with single custom header",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
				CustomHeaders: map[string]string{
					"Authorization": "Bearer token123",
				},
			},
			wantURL: "generic://webhook.example.com/notify?%40Authorization=Bearer+token123&disabletls=no&template=json",
		},
		{
			name: "webhook with multiple custom headers",
			config: models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
				CustomHeaders: map[string]string{
					"Authorization": "Bearer token123",
					"X-Api-Key":     "secret-key",
					"X-Source":      "Arcane",
				},
			},
			// Note: URL encoding may vary in order due to map iteration
			wantURL: "generic://webhook.example.com/notify",
		},
		{
			name: "webhook with all options",
			config: models.GenericConfig{
				WebhookURL:  "https://webhook.example.com:8443/api/v1/notify",
				ContentType: "application/json",
				Method:      "PUT",
				TitleKey:    "heading",
				MessageKey:  "content",
				DisableTLS:  true,
				CustomHeaders: map[string]string{
					"Authorization": "Bearer token123",
				},
			},
			wantURL: "generic://webhook.example.com:8443/api/v1/notify",
		},
		{
			name: "webhook URL with query params preserved",
			config: models.GenericConfig{
				WebhookURL: "http://www.pushplus.plus/send?token=abc123",
			},
			wantURL: "generic://www.pushplus.plus/send?disabletls=yes&template=json&token=abc123",
		},
		{
			name: "webhook URL with multiple query params preserved",
			config: models.GenericConfig{
				WebhookURL: "https://api.example.com/webhook?token=abc&channel=general",
			},
			wantURL: "generic://api.example.com/webhook?channel=general&disabletls=no&template=json&token=abc",
		},
		{
			name: "PushPlus webhook with content message key",
			config: models.GenericConfig{
				WebhookURL: "http://www.pushplus.plus/send?token=abc123",
				Method:     "POST",
				MessageKey: "content",
			},
			// Shoutrrr's generic service preserves `token=abc123` through to the
			// outbound HTTP request untouched, while consuming the config keys
			// (disabletls, template, method, messagekey). This is what PushPlus
			// needs: POST http://www.pushplus.plus/send?token=abc123 with
			// {"title":"...","content":"..."} at the root.
			wantURL: "generic://www.pushplus.plus/send?disabletls=yes&messagekey=content&method=POST&template=json&token=abc123",
		},
		{
			name: "empty webhook URL",
			config: models.GenericConfig{
				WebhookURL: "",
			},
			wantErr: "webhook URL is empty",
		},
		{
			name: "invalid webhook URL",
			config: models.GenericConfig{
				WebhookURL: "://invalid-url",
			},
			wantErr: "invalid webhook URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildGenericURL(tt.config)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)

			// For tests with multiple headers or all options, just check prefix
			if tt.name == "webhook with multiple custom headers" || tt.name == "webhook with all options" {
				assert.Contains(t, gotURL, tt.wantURL)
			} else {
				assert.Equal(t, tt.wantURL, gotURL)
			}
		})
	}
}

func TestBuildGenericURL_HTTPSchemeHandling(t *testing.T) {
	tests := []struct {
		name       string
		webhookURL string
		wantHost   string
	}{
		{
			name:       "HTTPS URL",
			webhookURL: "https://webhook.example.com/notify",
			wantHost:   "webhook.example.com",
		},
		{
			name:       "HTTP URL",
			webhookURL: "http://webhook.example.com/notify",
			wantHost:   "webhook.example.com",
		},
		{
			name:       "URL with custom port",
			webhookURL: "https://webhook.example.com:9443/notify",
			wantHost:   "webhook.example.com:9443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.GenericConfig{
				WebhookURL: tt.webhookURL,
			}

			gotURL, err := BuildGenericURL(config)
			require.NoError(t, err)

			// Verify the scheme is always "generic"
			assert.Contains(t, gotURL, "generic://")

			// Verify the host is preserved
			assert.Contains(t, gotURL, tt.wantHost)
		})
	}
}

func TestBuildGenericURL_CustomHeadersEncoding(t *testing.T) {
	config := models.GenericConfig{
		WebhookURL: "https://webhook.example.com/notify",
		CustomHeaders: map[string]string{
			"Authorization":  "Bearer token-with-special-chars!@#",
			"X-Custom-Value": "value with spaces",
		},
	}

	gotURL, err := BuildGenericURL(config)
	require.NoError(t, err)

	// Verify headers are prefixed with @
	assert.Contains(t, gotURL, "%40Authorization=")
	assert.Contains(t, gotURL, "%40X-Custom-Value=")

	// Verify special characters and spaces are encoded
	assert.Contains(t, gotURL, "value+with+spaces")
}

func TestBuildGenericURL_DisableTLSFlag(t *testing.T) {
	tests := []struct {
		name       string
		disableTLS bool
		wantParam  string
	}{
		{
			name:       "TLS enabled (default)",
			disableTLS: false,
			wantParam:  "disabletls=no",
		},
		{
			name:       "TLS disabled",
			disableTLS: true,
			wantParam:  "disabletls=yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.GenericConfig{
				WebhookURL: "webhook.example.com/notify",
				DisableTLS: tt.disableTLS,
			}

			gotURL, err := BuildGenericURL(config)
			require.NoError(t, err)

			assert.Contains(t, gotURL, tt.wantParam)
		})
	}
}

func TestBuildGenericURL_CustomKeys(t *testing.T) {
	tests := []struct {
		name       string
		titleKey   string
		messageKey string
		wantTitle  string
		wantMsg    string
	}{
		{
			name:       "default keys (empty)",
			titleKey:   "",
			messageKey: "",
			wantTitle:  "",
			wantMsg:    "",
		},
		{
			name:       "custom title key only",
			titleKey:   "subject",
			messageKey: "",
			wantTitle:  "titlekey=subject",
			wantMsg:    "",
		},
		{
			name:       "custom message key only",
			titleKey:   "",
			messageKey: "body",
			wantTitle:  "",
			wantMsg:    "messagekey=body",
		},
		{
			name:       "both custom keys",
			titleKey:   "heading",
			messageKey: "content",
			wantTitle:  "titlekey=heading",
			wantMsg:    "messagekey=content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.GenericConfig{
				WebhookURL: "https://webhook.example.com/notify",
				TitleKey:   tt.titleKey,
				MessageKey: tt.messageKey,
			}

			gotURL, err := BuildGenericURL(config)
			require.NoError(t, err)

			if tt.wantTitle != "" {
				assert.Contains(t, gotURL, tt.wantTitle)
			} else if tt.titleKey == "" {
				assert.NotContains(t, gotURL, "titlekey=")
			}

			if tt.wantMsg != "" {
				assert.Contains(t, gotURL, tt.wantMsg)
			} else if tt.messageKey == "" {
				assert.NotContains(t, gotURL, "messagekey=")
			}
		})
	}
}

// TestBuildGenericURL_PreservesUserShoutrrrConfigKeys verifies that an
// explicit Shoutrrr config key embedded by the user in the webhook URL is
// never silently overwritten by the provider defaults, the configured field
// values, or the URL-scheme-derived TLS flag.
func TestBuildGenericURL_PreservesUserShoutrrrConfigKeys(t *testing.T) {
	tests := []struct {
		name      string
		config    models.GenericConfig
		wantInURL []string
		notInURL  []string
	}{
		{
			name: "user template wins over default json",
			config: models.GenericConfig{
				WebhookURL: "https://example.com/api?template=custom",
			},
			wantInURL: []string{"template=custom"},
			notInURL:  []string{"template=json"},
		},
		{
			name: "user disabletls wins over scheme-derived value",
			config: models.GenericConfig{
				WebhookURL: "https://example.com/api?disabletls=yes",
			},
			wantInURL: []string{"disabletls=yes"},
			notInURL:  []string{"disabletls=no"},
		},
		{
			name: "user messagekey wins over configured value",
			config: models.GenericConfig{
				WebhookURL: "https://example.com/api?messagekey=user_msg",
				MessageKey: "configured_msg",
			},
			wantInURL: []string{"messagekey=user_msg"},
			notInURL:  []string{"messagekey=configured_msg"},
		},
		{
			name: "user method wins over configured value",
			config: models.GenericConfig{
				WebhookURL: "https://example.com/api?method=PUT",
				Method:     "POST",
			},
			wantInURL: []string{"method=PUT"},
			notInURL:  []string{"method=POST"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildGenericURL(tt.config)
			require.NoError(t, err)

			for _, want := range tt.wantInURL {
				assert.Contains(t, gotURL, want)
			}
			for _, notWant := range tt.notInURL {
				assert.NotContains(t, gotURL, notWant)
			}
		})
	}
}
