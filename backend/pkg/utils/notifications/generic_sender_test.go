package notifications

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
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

// TestBuildGenericURL_PayloadTemplate verifies that a configured payload
// template switches the Shoutrrr template to the named "arcane" template and
// defaults the content type to JSON, while a user-supplied inline template
// query key still wins.
func TestBuildGenericURL_PayloadTemplate(t *testing.T) {
	gotURL, err := BuildGenericURL(models.GenericConfig{
		WebhookURL:      "https://webhook.example.com/notify",
		PayloadTemplate: `{"text": "{{.message}}"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, gotURL, "template=arcane")
	assert.Contains(t, gotURL, "contenttype=application%2Fjson")
	assert.NotContains(t, gotURL, "template=json")

	gotURL, err = BuildGenericURL(models.GenericConfig{
		WebhookURL:      "https://webhook.example.com/notify?template=custom",
		PayloadTemplate: `{"text": "{{.message}}"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, gotURL, "template=custom")
	assert.NotContains(t, gotURL, "template=arcane")
}

// TestRenderGenericPayloadTemplate_EscapesJSON guards the escaping contract: a
// template embedding values inside JSON string literals must render valid JSON
// for hostile message content, and the event vars must be available.
func TestRenderGenericPayloadTemplate_EscapesJSON(t *testing.T) {
	config := models.GenericConfig{
		PayloadTemplate: `{"text": "{{.title}}: {{.message}}", "env": "{{.environment}}", "event": "{{.event}}"}`,
	}
	vars := map[string]string{"environment": "Local Docker", "environmentId": "0", "event": "image_update", "timestamp": "2026-07-28T00:00:00Z"}

	body, err := RenderGenericPayloadTemplate(config, "Image \"Update\"", "he said \"stop\"\nnew line \\ end", vars)
	require.NoError(t, err)

	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &decoded), "rendered body must be valid JSON: %s", body)
	assert.Equal(t, "Image \"Update\": he said \"stop\"\nnew line \\ end", decoded["text"])
	assert.Equal(t, "Local Docker", decoded["env"])
	assert.Equal(t, "image_update", decoded["event"])
}

// TestSendGenericWithTitle_PayloadTemplate exercises the Shoutrrr send path
// end to end: the payload template registered on the located service must
// reach the endpoint as the rendered request body, with the configured custom
// headers and JSON content type applied.
func TestSendGenericWithTitle_PayloadTemplate(t *testing.T) {
	var gotBody []byte
	var gotContentType, gotMethod, gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotContentType = r.Header.Get("Content-Type")
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := models.GenericConfig{
		WebhookURL:      server.URL + "/v1/spaces/FOO/messages",
		PayloadTemplate: `{"text": "{{.title}}\n{{.message}} ({{.environment}})"}`,
		CustomHeaders:   map[string]string{"Authorization": "Bearer t0ken"},
	}
	vars := map[string]string{"environment": "Local Docker", "environmentId": "0", "event": "image_update", "timestamp": "2026-07-28T00:00:00Z"}

	require.NoError(t, SendGenericWithTitle(t.Context(), config, "Image Update", "nginx:latest updated", vars))

	assert.JSONEq(t, `{"text": "Image Update\nnginx:latest updated (Local Docker)"}`, string(gotBody))
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "Bearer t0ken", gotAuth)
}

// TestSendGenericWithTitle_TemplateWithSuccessBodyContains verifies the two
// direct-send features compose: template rendering plus response validation.
func TestSendGenericWithTitle_TemplateWithSuccessBodyContains(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":900,"msg":"failed"}`))
	}))
	defer server.Close()

	config := models.GenericConfig{
		WebhookURL:          server.URL,
		PayloadTemplate:     `{"text": "{{.message}}"}`,
		SuccessBodyContains: `"code":200`,
	}

	err := SendGenericWithTitle(t.Context(), config, "", "hello", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not contain expected success indicator")
	assert.JSONEq(t, `{"text": "hello"}`, string(gotBody))
}

func TestValidateGenericPayloadTemplate(t *testing.T) {
	tests := []struct {
		name    string
		config  models.GenericConfig
		wantErr string
	}{
		{
			name:   "no template is always valid",
			config: models.GenericConfig{},
		},
		{
			name: "valid JSON template",
			config: models.GenericConfig{
				PayloadTemplate: `{"text": "{{.message}}", "when": "{{.timestamp}}"}`,
			},
		},
		{
			name: "invalid template syntax is reported",
			config: models.GenericConfig{
				PayloadTemplate: `{"text": "{{.message}`,
			},
			wantErr: "invalid webhook payload template",
		},
		{
			name: "template rendering malformed JSON is rejected",
			config: models.GenericConfig{
				PayloadTemplate: `{"text": "{{.message}}"`,
			},
			wantErr: "did not render valid JSON",
		},
		{
			name: "malformed JSON is allowed for non-JSON content types",
			config: models.GenericConfig{
				ContentType:     "text/plain",
				PayloadTemplate: `event={{.event}} message={{.message}}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGenericPayloadTemplate(tt.config)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
