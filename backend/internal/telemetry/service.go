package telemetry

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const maxPayloadBytes = 512 << 10

var (
	ErrPayloadTooLarge = errors.New("telemetry payload exceeds 512 KiB")
	ErrInvalidPayload  = errors.New("telemetry payload is not OTLP/JSON from arcane-frontend")
	// resourceKeys names each signal's top-level OTLP/JSON resource list.
	resourceKeys = map[string]string{"traces": "resourceSpans", "metrics": "resourceMetrics", "logs": "resourceLogs"}
)

type otlpAttribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
	} `json:"value"`
}

// otlpEnvelope decodes only the resource identities of an OTLP/JSON export.
type otlpEnvelope map[string][]struct {
	Resource struct {
		Attributes []otlpAttribute `json:"attributes"`
	} `json:"resource"`
}

// Service relays browser OTLP/JSON payloads to the configured collector so the
// collector and its credentials stay server-side.
type Service struct {
	destinations map[string]destination
	client       *http.Client
}

type destination struct {
	endpoint string
	headers  http.Header
}

// SDKDisabled reports whether OTEL_SDK_DISABLED turns all telemetry off.
func SDKDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true")
}

// Endpoint returns the OTLP/HTTP URL for signal, or "" when its exporter is
// off, uses gRPC, or has no endpoint configured.
func Endpoint(signal string) string {
	upper := strings.ToUpper(signal)
	if SDKDisabled() || strings.TrimSpace(os.Getenv("OTEL_"+upper+"_EXPORTER")) != "otlp" {
		return ""
	}
	protocol := cmp.Or(os.Getenv("OTEL_EXPORTER_OTLP_"+upper+"_PROTOCOL"), os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"), "http/protobuf")
	if !strings.HasPrefix(protocol, "http/") {
		return ""
	}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_" + upper + "_ENDPOINT")
	if endpoint == "" {
		base := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		if base == "" {
			return ""
		}
		endpoint = strings.TrimRight(base, "/") + "/v1/" + signal
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

// NewService returns nil when no signal can be relayed.
func NewService() *Service {
	destinations := make(map[string]destination, 3)
	for _, signal := range []string{"traces", "metrics", "logs"} {
		endpoint := Endpoint(signal)
		if endpoint == "" {
			continue
		}
		headers := http.Header{}
		rawHeaders := cmp.Or(os.Getenv("OTEL_EXPORTER_OTLP_"+strings.ToUpper(signal)+"_HEADERS"), os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
		for pair := range strings.SplitSeq(rawHeaders, ",") {
			key, value, ok := strings.Cut(pair, "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				continue
			}
			if decoded, err := url.PathUnescape(strings.TrimSpace(value)); err == nil {
				value = decoded
			}
			headers.Set(key, value)
		}
		destinations[signal] = destination{endpoint: endpoint, headers: headers}
	}
	if len(destinations) == 0 {
		return nil
	}

	return &Service{
		destinations: destinations,
		client: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Forward posts an OTLP/JSON payload of at most 512 KiB for signal to the collector
// and returns its status code. Every resource must identify as arcane-frontend.
func (s *Service) Forward(ctx context.Context, signal string, body io.Reader) (int, error) {
	dest, ok := s.destinations[signal]
	if !ok {
		return 0, fmt.Errorf("telemetry signal %q is not relayed", signal)
	}
	payload, err := io.ReadAll(io.LimitReader(body, maxPayloadBytes+1))
	if err != nil {
		return 0, fmt.Errorf("read %s payload: %w", signal, err)
	}
	if len(payload) > maxPayloadBytes {
		return 0, ErrPayloadTooLarge
	}
	var envelope otlpEnvelope
	if json.Unmarshal(payload, &envelope) != nil || len(envelope) != 1 || len(envelope[resourceKeys[signal]]) == 0 {
		return 0, ErrInvalidPayload
	}
	for _, entry := range envelope[resourceKeys[signal]] {
		if !slices.ContainsFunc(entry.Resource.Attributes, func(attr otlpAttribute) bool {
			return attr.Key == "service.name" && attr.Value.StringValue == "arcane-frontend"
		}) {
			return 0, ErrInvalidPayload
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("create %s forward request: %w", signal, err)
	}
	req.Header = dest.headers.Clone()
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("forward %s: %w", signal, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxPayloadBytes))
	return resp.StatusCode, nil
}
