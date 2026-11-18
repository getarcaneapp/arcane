// Package secretapi holds the HTTP plumbing shared by the secret-manager
// clients: base URL validation, bounded reads, and error messages that never
// echo more than a short excerpt of what the server sent back.
package secretapi

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type (
	// APIError is a non-2xx answer from a secret manager.
	APIError struct {
		Service    string
		Message    string
		StatusCode int
	}

	// Requester sends JSON requests to one service.
	Requester struct {
		HTTP    *http.Client
		BaseURL *url.URL
		// Authorize adds credentials to each request. It may be nil.
		Authorize func(ctx context.Context, req *http.Request) error
		// Service names the service in errors, e.g. "OpenBao".
		Service string
	}
)

const (
	// MaxResponseBytes caps every response body.
	MaxResponseBytes = 10 << 20

	// maxMessageLen caps the server text quoted in errors.
	maxMessageLen = 300
)

var (
	// ErrUnexpectedShape means a 2xx answer did not decode into the expected
	// shape. Decoder messages can quote values, so the cause is not kept.
	ErrUnexpectedShape = errors.New("unexpected response shape")
)

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s returned HTTP %d", e.Service, e.StatusCode)
	}
	return fmt.Sprintf("%s returned HTTP %d: %s", e.Service, e.StatusCode, e.Message)
}

// IsNotFound reports whether err is an HTTP 404 from a secret manager.
func IsNotFound(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

// IsUnauthorized reports whether err is an HTTP 401 or 403.
func IsUnauthorized(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)
}

// ParseBaseURL validates the address of a service, such as
// http://openbao:8200. what names it in errors ("the OpenBao address").
func ParseBaseURL(raw, what, example string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s is required, e.g. %s", what, example)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", what, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid %s: scheme must be http or https, got %q", what, parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("invalid %s: missing host", what)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("invalid %s: credentials in the URL are not allowed", what)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

// NewRequester returns a requester with a default HTTP client when httpClient
// is nil.
func NewRequester(httpClient *http.Client, baseURL *url.URL, service string) *Requester {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Requester{HTTP: httpClient, BaseURL: baseURL, Service: service}
}

// Do sends one request. path is joined to the base URL as is, so callers
// escape path segments themselves. body is JSON-encoded when not nil, and a
// 2xx response is decoded into out when out is not nil.
func (r *Requester) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	raw, err := r.DoRaw(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("decode %s response: %w", r.Service, ErrUnexpectedShape)
	}
	return nil
}

// DoRaw is Do without decoding; it returns the 2xx body.
func (r *Requester) DoRaw(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	endpoint := r.BaseURL.String() + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode %s request: %w", r.Service, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", r.Service, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Authorize != nil {
		if err := r.Authorize(ctx, req); err != nil {
			return nil, err
		}
	}

	resp, err := r.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s is not reachable at %s: %w", r.Service, r.BaseURL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", r.Service, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{Service: r.Service, StatusCode: resp.StatusCode, Message: errorMessageInternal(raw)}
	}
	return raw, nil
}

// errorMessageInternal pulls a short message out of the common error shapes:
// {"errors":["..."]}, {"messages":["..."]}, {"message":"..."}, {"error":"..."}.
func errorMessageInternal(raw []byte) string {
	var shape struct {
		Errors   []jsontext.Value `json:"errors"`
		Messages []string         `json:"messages"`
		Message  string           `json:"message"`
		Error    jsontext.Value   `json:"error"`
	}
	message := ""
	if json.Unmarshal(raw, &shape) == nil {
		var parts []string
		for _, item := range shape.Errors {
			parts = append(parts, scalarTextInternal(item))
		}
		parts = append(parts, shape.Messages...)
		if shape.Message != "" {
			parts = append(parts, shape.Message)
		}
		if len(shape.Error) > 0 {
			parts = append(parts, scalarTextInternal(shape.Error))
		}
		message = strings.Join(nonEmptyInternal(parts), "; ")
	} else if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("<")) {
		message = string(raw)
	}
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > maxMessageLen {
		message = message[:maxMessageLen]
	}
	return message
}

func scalarTextInternal(value jsontext.Value) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var object struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(value, &object) == nil {
		return object.Message
	}
	return ""
}

func nonEmptyInternal(parts []string) []string {
	out := parts[:0]
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

// StringValues turns a decoded JSON object into variable values. Strings are
// kept, numbers and booleans become their JSON text, and anything else
// (objects, arrays, null) is reported as skipped.
func StringValues(object map[string]jsontext.Value) (map[string]string, []string) {
	values := make(map[string]string, len(object))
	var skipped []string
	for key, raw := range object {
		trimmed := bytes.TrimSpace(raw)
		switch {
		case len(trimmed) == 0:
			skipped = append(skipped, key)
		case trimmed[0] == '"':
			var text string
			if json.Unmarshal(trimmed, &text) != nil {
				skipped = append(skipped, key)
				continue
			}
			values[key] = text
		case string(trimmed) == "true" || string(trimmed) == "false":
			values[key] = string(trimmed)
		case trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9'):
			values[key] = string(trimmed)
		default:
			skipped = append(skipped, key)
		}
	}
	return values, skipped
}

// PathEscapeSegments escapes each segment of a slash-separated path.
func PathEscapeSegments(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
