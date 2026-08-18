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
	"time"

	"emperror.dev/errors"

	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// genericPayloadTemplateID is the Shoutrrr template ID under which a user's
// payload template is registered on the generic service instance.
const genericPayloadTemplateID = "arcane"

// genericHTTPClient bounds every generic webhook request. Shoutrrr's generic
// service falls back to a client with no timeout when none is injected, and
// the payload-template path sends through the located service directly,
// bypassing the router's own per-send timeout.
var genericHTTPClient = &http.Client{Timeout: 15 * time.Second}

// resolveWebhookURLInternal parses and normalises the configured webhook URL,
// adding a default scheme when the user omitted one. It is the single source
// of truth for scheme normalisation and host validation used by both
// BuildGenericURL and sendGenericDirectInternal.
func resolveWebhookURLInternal(config GenericConfig) (*url.URL, error) {
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
func BuildGenericURL(config GenericConfig) (string, error) {
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
	// expect. When the user configured a payload template, point Shoutrrr at
	// the named template registered at send time instead, and default the
	// content type to JSON since that is what payload templates target.
	hasPayloadTemplate := strings.TrimSpace(config.PayloadTemplate) != ""
	if hasPayloadTemplate {
		setDefault("template", genericPayloadTemplateID)
	} else {
		setDefault("template", "json")
	}
	setDefault("contenttype", config.ContentType)
	if hasPayloadTemplate {
		setDefault("contenttype", "application/json")
	}
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

// EventVars builds the per-event variables exposed to a generic webhook
// payload template. The names must stay clear of Shoutrrr generic config keys
// (title, template, contenttype, method, messagekey, titlekey, disabletls) —
// a colliding param would mutate the per-send service config.
func EventVars(environmentName, environmentID string, event NotificationEventType) map[string]string {
	return map[string]string{
		"environment":   environmentName,
		"environmentId": environmentID,
		"event":         string(event),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
}

// jsonEscapeString escapes value for embedding inside a JSON string literal:
// json.Marshal yields a quoted string, and the surrounding quotes are trimmed
// so the template author keeps control of the quoting. Applied only on the
// payload-template path — the default template=json path is escaped by
// Shoutrrr's own json.Marshal.
func jsonEscapeString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	return strings.TrimSuffix(strings.TrimPrefix(string(encoded), `"`), `"`)
}

// genericTemplateDataInternal builds the variable map a payload template is
// rendered with on the direct-HTTP path: the JSON-escaped title and message
// under the configured titlekey/messagekey — mirroring how Shoutrrr's
// createSendParams keys the send params — plus the escaped per-event vars.
func genericTemplateDataInternal(config GenericConfig, title, message string, vars map[string]string) map[string]string {
	titleKey := config.TitleKey
	if titleKey == "" {
		titleKey = "title"
	}
	messageKey := config.MessageKey
	if messageKey == "" {
		messageKey = "message"
	}

	data := make(map[string]string, len(vars)+2)
	for key, value := range vars {
		data[key] = jsonEscapeString(value)
	}
	data[titleKey] = jsonEscapeString(title)
	data[messageKey] = jsonEscapeString(message)
	return data
}

// RenderGenericPayloadTemplate renders the configured payload template with
// the same variables the Shoutrrr send path exposes. Used by the direct-HTTP
// path (SuccessBodyContains) and by save-time validation.
func RenderGenericPayloadTemplate(config GenericConfig, title, message string, vars map[string]string) (string, error) {
	tmpl, err := template.New(genericPayloadTemplateID).Parse(config.PayloadTemplate)
	if err != nil {
		return "", errors.WrapIf(err, "invalid webhook payload template")
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, genericTemplateDataInternal(config, title, message, vars)); err != nil {
		return "", errors.WrapIf(err, "failed to render webhook payload template")
	}
	return rendered.String(), nil
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

// ValidateGenericPayloadTemplate checks a configured payload template at save
// time: it must parse, execute against sample variables, and — when the
// content type is JSON (the default) — render valid JSON. Shoutrrr renders
// the template internally at send time, so validating here surfaces mistakes
// against the user's own configuration instead of as an opaque send failure.
func ValidateGenericPayloadTemplate(config GenericConfig) error {
	if strings.TrimSpace(config.PayloadTemplate) == "" {
		return nil
	}

	sampleVars := EventVars("Local Docker", "0", NotificationEventImageUpdate)
	rendered, err := RenderGenericPayloadTemplate(config, "Sample Title", "sample \"message\"\nwith a newline", sampleVars)
	if err != nil {
		return err
	}

	if isJSONContentType(config.ContentType) && !jsontext.Value(rendered).IsValid() {
		return errors.New("webhook payload template did not render valid JSON")
	}
	return nil
}

// SendGenericWithTitle sends a message with title via Shoutrrr Generic webhook.
// When config.SuccessBodyContains is set the response body is also inspected —
// this is necessary for providers (e.g. PushPlus) that always return HTTP 200
// but embed a success/failure indicator inside the JSON body.
func SendGenericWithTitle(ctx context.Context, config GenericConfig, title, message string, vars map[string]string) error {
	if config.WebhookURL == "" {
		return errors.New("webhook URL is empty")
	}

	// When the caller needs response-body validation we make the HTTP request
	// ourselves so that we can inspect the body.  Otherwise we delegate to
	// shoutrrr, which preserves the existing behaviour for everyone who does
	// not set SuccessBodyContains.
	if config.SuccessBodyContains != "" {
		return sendGenericDirectInternal(ctx, config, title, message, vars)
	}

	shoutrrrURL, err := BuildGenericURL(config)
	if err != nil {
		return errors.WrapIf(err, "failed to build shoutrrr Generic URL")
	}

	senderOptions := shoutrrrTypes.SenderOptions{HTTPClient: genericHTTPClient}

	// A user-supplied inline `?template=<id>` in the webhook URL wins over the
	// default "arcane" ID, so the payload template must be registered under the
	// effective ID for Shoutrrr to resolve it. An inline `template=json`
	// selects Shoutrrr's built-in JSON template instead, which does its own
	// escaping — send through the plain path so params are not double-escaped.
	if strings.TrimSpace(config.PayloadTemplate) != "" {
		if templateID := effectiveGenericTemplateIDInternal(shoutrrrURL); templateID != "json" && templateID != "JSON" {
			return sendGenericTemplatedInternal(config, shoutrrrURL, templateID, senderOptions, title, message, vars)
		}
	}

	sender, err := shoutrrr.CreateSenderWithOptions(senderOptions, shoutrrrURL)
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

// effectiveGenericTemplateIDInternal returns the template ID the built
// Shoutrrr URL selects for payload rendering. BuildGenericURL always sets a
// template query key, so an empty result only happens on an unparseable URL;
// fall back to the default ID in that case.
func effectiveGenericTemplateIDInternal(shoutrrrURL string) string {
	parsed, err := url.Parse(shoutrrrURL)
	if err != nil {
		return genericPayloadTemplateID
	}
	if id := parsed.Query().Get("template"); id != "" {
		return id
	}
	return genericPayloadTemplateID
}

// sendGenericTemplatedInternal sends through Shoutrrr's generic service with
// the user's payload template registered on the service instance under
// templateID — the ID the URL's template key selects. Templates are resolved
// per service instance, which router.Send does not expose, so the service is
// obtained via Locate — the full initService path (scheme extraction, config
// parse, HTTP client injection) — and sent on directly.
func sendGenericTemplatedInternal(config GenericConfig, shoutrrrURL, templateID string, opts shoutrrrTypes.SenderOptions, title, message string, vars map[string]string) error {
	sender, err := shoutrrr.CreateSenderWithOptions(opts)
	if err != nil {
		return errors.WrapIf(err, "failed to create shoutrrr Generic sender")
	}

	service, err := sender.Locate(shoutrrrURL)
	if err != nil {
		return errors.WrapIf(err, "failed to initialize shoutrrr Generic service")
	}

	if err := service.SetTemplateString(templateID, config.PayloadTemplate); err != nil {
		return errors.WrapIf(err, "invalid webhook payload template")
	}

	// Shoutrrr renders the registered template over the send params, remapping
	// the "title" param to the configured titlekey and adding the message under
	// the configured messagekey. Every value is JSON-escaped here, so
	// {"text": "{{.message}}"} stays valid JSON for hostile message content —
	// text/template itself does no escaping.
	params := shoutrrrTypes.Params{"title": jsonEscapeString(title)}
	for key, value := range vars {
		params[key] = jsonEscapeString(value)
	}

	if err := service.Send(jsonEscapeString(message), &params); err != nil {
		return errors.WrapIf(err, "failed to send Generic webhook message via shoutrrr")
	}
	return nil
}

// sendGenericDirectInternal makes the webhook HTTP call directly, giving access
// to the response body so that provider-level success/failure can be detected
// even when the HTTP status is always 200. It renders the payload template when
// one is configured so the two features compose.
func sendGenericDirectInternal(ctx context.Context, config GenericConfig, title, message string, vars map[string]string) error {
	webhookURL, err := resolveWebhookURLInternal(config)
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

		body, err = json.Marshal(payload)
		if err != nil {
			return errors.WrapIf(err, "failed to marshal webhook payload")
		}
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

	resp, err := genericHTTPClient.Do(req)
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
