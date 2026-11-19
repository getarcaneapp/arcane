package secretsource

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
	"strconv"
	"strings"
	"time"

	"kit.getarcane.app/cache"

	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	httpFetch struct {
		inputs      map[string]string
		credentials map[string]string
		path        string
		sourceID    string
		cacheKey    string
		definition  types.SecretSourceDefinition
	}

	httpResponse struct {
		body   jsontext.Value
		status int
	}
)

func (e *Engine) fetchHTTP(ctx context.Context, fetch httpFetch) (jsontext.Value, error) {
	base, err := httpx.ValidateOutboundHTTPURL(fetch.inputs[BaseURLInput])
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", BaseURLInput, err)
	}

	login, cached, err := e.login(ctx, base, fetch)
	if err != nil {
		return nil, err
	}
	response, err := e.send(ctx, base, "fetch", *fetch.definition.Fetch, templateData(fetch.inputs, fetch.credentials, fetch.path, login))
	if err != nil {
		return nil, err
	}
	if cached && (response.status == http.StatusUnauthorized || response.status == http.StatusForbidden) {
		e.tokens.Delete(fetch.cacheKey)
		if login, _, err = e.login(ctx, base, fetch); err != nil {
			return nil, err
		}
		if response, err = e.send(ctx, base, "fetch", *fetch.definition.Fetch, templateData(fetch.inputs, fetch.credentials, fetch.path, login)); err != nil {
			return nil, err
		}
	}
	if response.status < 200 || response.status > 299 {
		return nil, fmt.Errorf("fetch failed: %s returned HTTP %d", base.Host, response.status)
	}
	return response.body, nil
}

func (e *Engine) login(ctx context.Context, base *url.URL, fetch httpFetch) (map[string]string, bool, error) {
	if fetch.definition.Login == nil {
		return map[string]string{}, false, nil
	}
	if token, ok, _ := e.tokens.Get(fetch.cacheKey); ok {
		return token.values, true, nil
	}

	response, err := e.send(ctx, base, "login", *fetch.definition.Login, templateData(fetch.inputs, fetch.credentials, fetch.path, map[string]string{}))
	if err != nil {
		return nil, false, err
	}
	if response.status < 200 || response.status > 299 {
		return nil, false, fmt.Errorf("login failed: %s returned HTTP %d", base.Host, response.status)
	}

	values := make(map[string]string, len(fetch.definition.Login.Capture))
	for name, path := range fetch.definition.Login.Capture {
		value, ok, captureErr := firstScalar(response.body, path)
		if captureErr != nil {
			return nil, false, fmt.Errorf("login response: %w", captureErr)
		}
		if !ok {
			return nil, false, fmt.Errorf("login response has no %q value", path)
		}
		values[name] = value
	}

	ttl := defaultTokenTTL
	if seconds, parseErr := strconv.ParseFloat(values[expiresInCaptureID], 64); parseErr == nil && seconds > 0 {
		ttl = time.Duration(seconds * 0.9 * float64(time.Second))
	}
	e.tokens.Set(cache.Entry[string, loginToken]{
		Key: fetch.cacheKey,
		Value: loginToken{
			values:   values,
			sourceID: fetch.sourceID,
		},
		TTL: ttl,
	})
	return values, false, nil
}

func (e *Engine) send(ctx context.Context, base *url.URL, name string, step types.SecretSourceHTTPStep, data map[string]any) (httpResponse, error) {
	path, err := render(name+".path", step.Path, data)
	if err != nil {
		return httpResponse{}, err
	}
	query, err := renderMap(name+".query", step.Query, data)
	if err != nil {
		return httpResponse{}, err
	}
	if strings.ContainsAny(path, "?#") {
		return httpResponse{}, fmt.Errorf("%s: request path must not contain ? or #; use the step's query parameters", name)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return httpResponse{}, fmt.Errorf("%s: invalid request path: %w", name, err)
	}
	if ref.Scheme != "" || ref.Host != "" || ref.User != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return httpResponse{}, fmt.Errorf("%s: request path must be a relative path starting with a single /", name)
	}
	for segment := range strings.SplitSeq(strings.ReplaceAll(ref.Path, "\\", "/"), "/") {
		if segment == ".." {
			return httpResponse{}, fmt.Errorf("%s: request path must not contain .. segments", name)
		}
	}
	values := url.Values{}
	for key, value := range query {
		if value != "" {
			values.Set(key, value)
		}
	}
	target := url.URL{
		Scheme:   base.Scheme,
		Host:     base.Host,
		Path:     strings.TrimSuffix(base.Path, "/") + ref.Path,
		RawPath:  strings.TrimSuffix(base.EscapedPath(), "/") + ref.EscapedPath(),
		RawQuery: values.Encode(),
	}
	headers, err := renderMap(name+".headers", step.Headers, data)
	if err != nil {
		return httpResponse{}, err
	}
	body, contentType, err := requestBody(name, step, data)
	if err != nil {
		return httpResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, cmp.Or(strings.ToUpper(step.Method), http.MethodGet), target.String(), body)
	if err != nil {
		return httpResponse{}, fmt.Errorf("%s: failed to build request: %w", name, err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return httpResponse{}, fmt.Errorf("%s request to %s failed: %w", name, base.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return httpResponse{}, fmt.Errorf("%s: failed to read response: %w", name, err)
	}
	if len(raw) > maxResponseBytes {
		return httpResponse{}, fmt.Errorf("%s: response exceeds %d bytes", name, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return httpResponse{status: resp.StatusCode}, nil
	}
	value := jsontext.Value(raw)
	if !value.IsValid() {
		return httpResponse{}, fmt.Errorf("%s: response from %s is not valid JSON", name, base.Host)
	}
	return httpResponse{
		body:   value,
		status: resp.StatusCode,
	}, nil
}

func requestBody(name string, step types.SecretSourceHTTPStep, data map[string]any) (io.Reader, string, error) {
	switch {
	case step.JSON != nil:
		rendered, err := renderValue(name+".json", step.JSON, data)
		if err != nil {
			return nil, "", err
		}
		encoded, err := json.Marshal(rendered)
		if err != nil {
			return nil, "", fmt.Errorf("%s: failed to encode JSON body: %w", name, err)
		}
		return bytes.NewReader(encoded), "application/json", nil
	case step.Form != nil:
		rendered, err := renderMap(name+".form", step.Form, data)
		if err != nil {
			return nil, "", err
		}
		form := url.Values{}
		for key, value := range rendered {
			form.Set(key, value)
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", nil
	}
}
