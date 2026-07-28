package notifications

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// resolveWebhookURLInternal parses and normalises the configured webhook URL,
// adding a default scheme when the user omitted one. It is the single source
// of truth for scheme normalisation and host validation used by both
// BuildGenericURL and sendGenericDirectInternal.
func resolveWebhookURLInternal(config models.GenericConfig) (*url.URL, error) {
	if config.WebhookURL == "" {
		return nil, errors.New("webhook URL is empty")
	}

	parsed, err := url.Parse(config.WebhookURL)
	if err != nil {
		return nil, errors.WrapIf(err, "invalid webhook URL")
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
			return nil, errors.WrapIf(err, "invalid webhook URL")
		}
	}

	if parsed.Host == "" {
		return nil, errors.New("invalid webhook URL: missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, errors.Errorf("invalid webhook URL scheme: %s", parsed.Scheme)
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
		return errors.New("webhook URL is empty")
	}

	// When the caller needs response-body validation, or has supplied a custom
	// body template, we make the HTTP request ourselves. Shoutrrr's generic
	// service only resolves `template` as the ID of a template registered on
	// the service instance, which the sender API does not expose — and its
	// built-in JSON template can only emit a flat object, so nested payloads
	// (Lark/Feishu, Teams, provider-specific envelopes) are unreachable through
	// it. Otherwise we delegate to shoutrrr, preserving existing behaviour.
	if config.SuccessBodyContains != "" || strings.TrimSpace(config.Template) != "" {
		return sendGenericDirectInternal(ctx, config, title, message)
	}

	shoutrrrURL, err := BuildGenericURL(config)
	if err != nil {
		return errors.WrapIf(err, "failed to build shoutrrr Generic URL")
	}

	sender, err := shoutrrr.CreateSenderWithOptions(shoutrrrTypes.SenderOptions{}, shoutrrrURL)
	if err != nil {
		return errors.WrapIf(err, "failed to create shoutrrr Generic sender")
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
			return errors.WrapIf(err, "failed to send Generic webhook message with title via shoutrrr")
		}
	}
	return nil
}

// resolveGenericKeys returns the effective title/message payload keys, applying
// the same defaults Shoutrrr's generic service uses.
func resolveGenericKeys(config models.GenericConfig) (titleKey, messageKey string) {
	titleKey = config.TitleKey
	if titleKey == "" {
		titleKey = "title"
	}
	messageKey = config.MessageKey
	if messageKey == "" {
		messageKey = "message"
	}
	return titleKey, messageKey
}

// RenderGenericTemplate renders a user-supplied Go text/template into a webhook
// request body.
//
// The template is given `title` and `message`, plus aliases under the
// configured TitleKey/MessageKey so a template can use whichever naming the
// user already configured. Values are exposed pre-escaped for JSON string
// contexts, so `{"text": "{{.message}}"}` stays valid even when the message
// contains quotes, backslashes or newlines. text/template (not html/template)
// is used deliberately: the output is a webhook body, and HTML entity encoding
// would corrupt payloads.
func RenderGenericTemplate(config models.GenericConfig, title, message string) (string, error) {
	tmpl, err := template.New("generic-webhook").Option("missingkey=zero").Parse(config.Template)
	if err != nil {
		return "", errors.WrapIf(err, "invalid webhook payload template")
	}

	titleKey, messageKey := resolveGenericKeys(config)

	// Escape for embedding inside a JSON string literal, which is where these
	// values land in practice. json.Marshal yields a quoted string; trim the
	// surrounding quotes so the template author keeps control of the quoting.
	escape := func(value string) string {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return value
		}
		return strings.TrimSuffix(strings.TrimPrefix(string(encoded), `"`), `"`)
	}

	// The canonical names are authoritative; the configured-key aliases are only
	// added when they do not shadow one, so a TitleKey of "message" cannot
	// repoint `.message` at the title.
	data := map[string]string{
		"title":      escape(title),
		"message":    escape(message),
		"titleRaw":   title,
		"messageRaw": message,
	}
	for key, value := range map[string]string{titleKey: title, messageKey: message} {
		if _, taken := data[key]; !taken {
			data[key] = escape(value)
		}
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return "", errors.WrapIf(err, "failed to render webhook payload template")
	}

	body := rendered.String()

	// A template can be syntactically valid Go yet still emit malformed JSON —
	// an unbalanced brace produces no parse error, only a puzzling 4xx from the
	// remote endpoint. When the payload is meant to be JSON, validate it here so
	// the mistake surfaces against the user's own configuration instead.
	if isJSONContentType(config.ContentType) && !jsontext.Value(body).IsValid() {
		return "", errors.New("webhook payload template did not render valid JSON")
	}

	return body, nil
}

// isJSONContentType reports whether the configured content type denotes JSON.
// An empty content type counts as JSON because that is the default applied to
// outgoing generic webhook requests.
func isJSONContentType(contentType string) bool {
	if contentType == "" {
		return true
	}
	// Ignore any parameters such as "; charset=utf-8".
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// buildGenericPayload produces the request body: the user's rendered template
// when one is configured, otherwise the flat JSON object keyed by the
// configured title/message keys.
func buildGenericPayload(config models.GenericConfig, title, message string) ([]byte, error) {
	if strings.TrimSpace(config.Template) != "" {
		rendered, err := RenderGenericTemplate(config, title, message)
		if err != nil {
			return nil, err
		}
		return []byte(rendered), nil
	}

	titleKey, messageKey := resolveGenericKeys(config)

	payload := map[string]string{messageKey: message}
	if title != "" {
		payload[titleKey] = title
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to marshal webhook payload")
	}
	return body, nil
}

// sendGenericDirectInternal makes the webhook HTTP call directly, giving access
// to the response body so that provider-level success/failure can be detected
// even when the HTTP status is always 200.
func sendGenericDirectInternal(ctx context.Context, config models.GenericConfig, title, message string) error {
	webhookURL, err := resolveWebhookURLInternal(config)
	if err != nil {
		return err
	}

	body, err := buildGenericPayload(config, title, message)
	if err != nil {
		return err
	}

	method := strings.ToUpper(config.Method)
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, webhookURL.String(), bytes.NewReader(body))
	if err != nil {
		return errors.WrapIf(err, "failed to create webhook request")
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
		return errors.WrapIf(err, "failed to send webhook request")
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.WrapIf(err, "failed to read webhook response body")
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return errors.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if !strings.Contains(string(respBody), config.SuccessBodyContains) {
		return errors.Errorf("webhook response did not contain expected success indicator %q: %s", config.SuccessBodyContains, string(respBody))
	}

	return nil
}
