package notifications

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"go.getarcane.app/kit/pkg"
)

// genericPayloadTemplateID names the Shoutrrr template a user's payload template is registered under.
const genericPayloadTemplateID = "arcane"

// genericHTTPClient bounds generic webhook sends; Shoutrrr's default client has no timeout.
var genericHTTPClient = &http.Client{Timeout: 15 * time.Second}

// resolveWebhookURL parses the configured webhook URL, adding a default scheme when omitted.
func resolveWebhookURL(config GenericConfig) (*url.URL, error) {
	if config.WebhookURL == "" {
		return nil, errors.New("webhook URL is empty")
	}

	parsed, err := url.Parse(config.WebhookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}

	hasScheme := strings.Contains(config.WebhookURL, "://")
	if parsed.Host == "" && !hasScheme {
		scheme := kit.Ternary(config.DisableTLS, "http", "https")
		normalized := strings.TrimPrefix(config.WebhookURL, "//")
		parsed, err = url.Parse(fmt.Sprintf("%s://%s", scheme, normalized))
		if err != nil {
			return nil, fmt.Errorf("invalid webhook URL: %w", err)
		}
	}

	if parsed.Host == "" {
		return nil, errors.New("invalid webhook URL: missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid webhook URL scheme: %s", parsed.Scheme)
	}

	return parsed, nil
}

// BuildGenericURL converts GenericConfig to Shoutrrr URL format for generic webhooks.
func BuildGenericURL(config GenericConfig) (string, error) {
	webhookURL, err := resolveWebhookURL(config)
	if err != nil {
		return "", err
	}

	// Unknown query keys (e.g. PushPlus `?token=`) pass through; Shoutrrr config keys set inline win over settings.
	query := webhookURL.Query()
	setDefault := func(key, value string) {
		if value != "" && query.Get(key) == "" {
			query.Set(key, value)
		}
	}

	// Payload templates use the named template registered at send time and default to JSON content.
	hasPayloadTemplate := strings.TrimSpace(config.PayloadTemplate) != ""
	setDefault("template", kit.Ternary(hasPayloadTemplate, genericPayloadTemplateID, "json"))
	setDefault("contenttype", config.ContentType)
	if hasPayloadTemplate {
		setDefault("contenttype", "application/json")
	}
	setDefault("method", config.Method)
	setDefault("titlekey", config.TitleKey)
	setDefault("messagekey", config.MessageKey)

	switch strings.ToLower(webhookURL.Scheme) {
	case "http":
		setDefault("disabletls", "yes")
	case "https":
		setDefault("disabletls", "no")
	}

	// Shoutrrr reads headers from @-prefixed query keys.
	for key, value := range config.CustomHeaders {
		query.Set("@"+key, value)
	}

	shoutrrrURL := &url.URL{
		Scheme:   "generic",
		Host:     webhookURL.Host,
		Path:     webhookURL.Path,
		RawQuery: query.Encode(),
	}

	return shoutrrrURL.String(), nil
}

// EventVars builds the per-event payload template variables.
// Names must not collide with Shoutrrr generic config keys, which would mutate the per-send config.
func EventVars(environmentName, environmentID string, event NotificationEventType) map[string]string {
	return map[string]string{
		"environment":   environmentName,
		"environmentId": environmentID,
		"event":         string(event),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
}

// jsonEscapeString escapes value for embedding inside a JSON string literal, without the surrounding quotes.
func jsonEscapeString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	return strings.TrimSuffix(strings.TrimPrefix(string(encoded), `"`), `"`)
}

// RenderGenericPayloadTemplate renders the payload template with the JSON-escaped title, message, and event vars.
func RenderGenericPayloadTemplate(config GenericConfig, title, message string, vars map[string]string) (string, error) {
	tmpl, err := template.New(genericPayloadTemplateID).Parse(config.PayloadTemplate)
	if err != nil {
		return "", fmt.Errorf("invalid webhook payload template: %w", err)
	}

	// Keyed by titlekey/messagekey to mirror Shoutrrr's send params.
	data := make(map[string]string, len(vars)+2)
	for key, value := range vars {
		data[key] = jsonEscapeString(value)
	}
	data[cmp.Or(config.TitleKey, "title")] = jsonEscapeString(title)
	data[cmp.Or(config.MessageKey, "message")] = jsonEscapeString(message)

	var rendered bytes.Buffer
	if executeErr := tmpl.Execute(&rendered, data); executeErr != nil {
		return "", fmt.Errorf("failed to render webhook payload template: %w", executeErr)
	}
	return rendered.String(), nil
}

// ValidateGenericPayloadTemplate checks that a payload template renders, and renders valid JSON for JSON content types.
func ValidateGenericPayloadTemplate(config GenericConfig) error {
	if strings.TrimSpace(config.PayloadTemplate) == "" {
		return nil
	}

	sampleVars := EventVars("Local Docker", "0", NotificationEventImageUpdate)
	rendered, err := RenderGenericPayloadTemplate(config, "Sample Title", "sample \"message\"\nwith a newline", sampleVars)
	if err != nil {
		return err
	}

	// An empty content type defaults to JSON.
	mediaType, _, _ := strings.Cut(config.ContentType, ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	isJSON := config.ContentType == "" || mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
	if isJSON && !jsontext.Value(rendered).IsValid() {
		return errors.New("webhook payload template did not render valid JSON")
	}
	return nil
}

// SendGenericWithTitle sends a message with title via the Shoutrrr generic webhook.
// SuccessBodyContains also checks the response body, for providers that always return HTTP 200.
func SendGenericWithTitle(ctx context.Context, config GenericConfig, title, message string, vars map[string]string) error {
	if config.WebhookURL == "" {
		return errors.New("webhook URL is empty")
	}

	if config.SuccessBodyContains != "" {
		return sendGenericDirect(ctx, config, title, message, vars)
	}

	shoutrrrURL, err := BuildGenericURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Generic URL: %w", err)
	}

	senderOptions := types.SenderOptions{HTTPClient: genericHTTPClient}

	// Register the payload template under the URL's effective template ID; an inline json template escapes on its own.
	if strings.TrimSpace(config.PayloadTemplate) != "" {
		templateID := genericPayloadTemplateID
		if parsed, parseErr := url.Parse(shoutrrrURL); parseErr == nil {
			templateID = cmp.Or(parsed.Query().Get("template"), genericPayloadTemplateID)
		}
		if templateID != "json" && templateID != "JSON" {
			// Templates are per service instance, which router.Send does not expose, so send through the located service.
			sender, createErr := shoutrrr.CreateSenderWithOptions(senderOptions)
			if createErr != nil {
				return fmt.Errorf("failed to create shoutrrr Generic sender: %w", createErr)
			}
			service, locateErr := sender.Locate(shoutrrrURL)
			if locateErr != nil {
				return fmt.Errorf("failed to initialize shoutrrr Generic service: %w", locateErr)
			}
			if setTemplateStringErr := service.SetTemplateString(templateID, config.PayloadTemplate); setTemplateStringErr != nil {
				return fmt.Errorf("invalid webhook payload template: %w", setTemplateStringErr)
			}

			// text/template does no escaping, so every value is JSON-escaped to keep the payload valid.
			params := types.Params{"title": jsonEscapeString(title)}
			for key, value := range vars {
				params[key] = jsonEscapeString(value)
			}
			if sendErr := service.Send(jsonEscapeString(message), &params); sendErr != nil {
				return fmt.Errorf("failed to send Generic webhook message via shoutrrr: %w", sendErr)
			}
			return nil
		}
	}

	sender, err := shoutrrr.CreateSenderWithOptions(senderOptions, shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Generic sender: %w", err)
	}

	// Shoutrrr maps the "title" param to the configured titlekey.
	params := types.Params{"title": title}
	for _, err := range sender.Send(message, &params) {
		if err != nil {
			return fmt.Errorf("failed to send Generic webhook message with title via shoutrrr: %w", err)
		}
	}
	return nil
}

// sendGenericDirect calls the webhook directly so the response body can be checked for SuccessBodyContains.
// Kept separate from SendGenericWithTitle to stay under the gocognit limit.
func sendGenericDirect(ctx context.Context, config GenericConfig, title, message string, vars map[string]string) error {
	webhookURL, err := resolveWebhookURL(config)
	if err != nil {
		return err
	}

	var body []byte
	if strings.TrimSpace(config.PayloadTemplate) != "" {
		rendered, renderErr := RenderGenericPayloadTemplate(config, title, message, vars)
		if renderErr != nil {
			return renderErr
		}
		body = []byte(rendered)
	} else {
		titleKey := cmp.Or(config.TitleKey, "title")
		payload := map[string]string{cmp.Or(config.MessageKey, "message"): message}
		payload[titleKey] = cmp.Or(title, payload[titleKey])

		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal webhook payload: %w", err)
		}
	}

	method := cmp.Or(strings.ToUpper(config.Method), http.MethodPost)
	req, err := http.NewRequestWithContext(ctx, method, webhookURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", cmp.Or(config.ContentType, "application/json"))
	for k, v := range config.CustomHeaders {
		req.Header.Set(k, v)
	}

	resp, err := genericHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read webhook response body: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if !strings.Contains(string(respBody), config.SuccessBodyContains) {
		return fmt.Errorf("webhook response did not contain expected success indicator %q: %s", config.SuccessBodyContains, string(respBody))
	}

	return nil
}
