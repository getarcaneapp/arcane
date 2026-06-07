package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// resolveWebhookURLInternal parses and normalises the configured webhook URL,
// adding a default scheme when the user omitted one. It is the single source
// of truth for scheme normalisation and host validation used by both
// BuildGenericURL and sendGenericDirectInternal.
func resolveWebhookURLInternal(config models.GenericConfig) (*url.URL, error) {
	if config.WebhookURL == "" {
		return nil, fmt.Errorf("webhook URL is empty")
	}

	parsed, err := url.Parse(config.WebhookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}

	hasScheme := strings.Contains(config.WebhookURL, "://")
	if parsed.Host == "" && !hasScheme {
		scheme := "https"
		if config.DisableTLS {
			scheme = "http"
		}
		normalized := strings.TrimPrefix(config.WebhookURL, "//")
		parsed, err = url.Parse(fmt.Sprintf("%s://%s", scheme, normalized))
		if err != nil {
			return nil, fmt.Errorf("invalid webhook URL: %w", err)
		}
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("invalid webhook URL: missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid webhook URL scheme: %s", parsed.Scheme)
	}

	return parsed, nil
}

// BuildGenericURL converts GenericConfig to Shoutrrr URL format for generic webhooks
func BuildGenericURL(config models.GenericConfig) (string, error) {
	webhookURL, err := resolveWebhookURLInternal(config)
	if err != nil {
		return "", err
	}

	// Start from the user's existing query parameters. Shoutrrr's generic
	// service preserves any query keys it does not recognise, so provider
	// tokens embedded in the webhook URL (e.g. PushPlus's `?token=...`) flow
	// straight through to the outbound HTTP request untouched.
	//
	// For Shoutrrr config keys (template, contenttype, method, titlekey,
	// messagekey, disabletls) we only fill in defaults / configured values
	// when the user has not already set the same key inline in the URL.
	// That way an explicit `?template=custom` or `?disabletls=yes` from the
	// user is always respected and never silently overwritten by the
	// provider settings or the URL-scheme-derived TLS flag.
	query := webhookURL.Query()

	setDefault := func(key, value string) {
		if value == "" {
			return
		}
		if query.Get(key) != "" {
			return
		}
		query.Set(key, value)
	}

	// Default to the JSON template — Shoutrrr's JSON template marshals the
	// notification params as a flat JSON object at the root level, which is
	// the format most providers (PushPlus, custom APIs, Home Assistant, etc.)
	// expect.
	setDefault("template", "json")
	setDefault("contenttype", config.ContentType)
	setDefault("method", config.Method)
	setDefault("titlekey", config.TitleKey)
	setDefault("messagekey", config.MessageKey)

	// Determine TLS setting from the webhook URL scheme (http/https) when the
	// user has not already passed `disabletls` explicitly.
	switch strings.ToLower(webhookURL.Scheme) {
	case "http":
		setDefault("disabletls", "yes")
	case "https":
		setDefault("disabletls", "no")
	}

	// Add custom headers as query parameters with @ prefix
	if len(config.CustomHeaders) > 0 {
		for key, value := range config.CustomHeaders {
			// Shoutrrr uses @ prefix for headers
			query.Set("@"+key, value)
		}
	}

	shoutrrrURL := &url.URL{
		Scheme:   "generic",
		Host:     webhookURL.Host,
		Path:     webhookURL.Path,
		RawQuery: query.Encode(),
	}

	return shoutrrrURL.String(), nil
}

// SendGenericWithTitle sends a message with title via Shoutrrr Generic webhook.
// When config.SuccessBodyContains is set the response body is also inspected —
// this is necessary for providers (e.g. PushPlus) that always return HTTP 200
// but embed a success/failure indicator inside the JSON body.
func SendGenericWithTitle(ctx context.Context, config models.GenericConfig, title, message string) error {
	if config.WebhookURL == "" {
		return fmt.Errorf("webhook URL is empty")
	}

	// When the caller needs response-body validation we make the HTTP request
	// ourselves so that we can inspect the body.  Otherwise we delegate to
	// shoutrrr, which preserves the existing behaviour for everyone who does
	// not set SuccessBodyContains.
	if config.SuccessBodyContains != "" {
		return sendGenericDirectInternal(ctx, config, title, message)
	}

	shoutrrrURL, err := BuildGenericURL(config)
	if err != nil {
		return fmt.Errorf("failed to build shoutrrr Generic URL: %w", err)
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr Generic sender: %w", err)
	}

	// Build params with title. Always use "title" as the param key — Shoutrrr's
	// generic service maps it to the configured titlekey in the JSON payload.
	params := shoutrrrTypes.Params{}
	if title != "" {
		params["title"] = title
	}

	errs := sender.Send(message, &params)
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to send Generic webhook message with title via shoutrrr: %w", err)
		}
	}
	return nil
}

// sendGenericDirectInternal makes the webhook HTTP call directly, giving access
// to the response body so that provider-level success/failure can be detected
// even when the HTTP status is always 200.
func sendGenericDirectInternal(ctx context.Context, config models.GenericConfig, title, message string) error {
	webhookURL, err := resolveWebhookURLInternal(config)
	if err != nil {
		return err
	}

	// Build JSON payload using the configured message/title keys.
	msgKey := config.MessageKey
	if msgKey == "" {
		msgKey = "message"
	}
	titleKey := config.TitleKey
	if titleKey == "" {
		titleKey = "title"
	}

	payload := map[string]string{msgKey: message}
	if title != "" {
		payload[titleKey] = title
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	method := strings.ToUpper(config.Method)
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, webhookURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	contentType := config.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)

	for k, v := range config.CustomHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
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
