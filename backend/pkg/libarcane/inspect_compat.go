package libarcane

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	networktypes "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// WrapDockerAPIClientForInspectCompatibility wraps a Docker API client so
// inspect calls can recover from daemon responses that encode address fields
// with CIDR suffixes that newer typed Moby structs reject.
func WrapDockerAPIClientForInspectCompatibility(apiClient client.APIClient) client.APIClient {
	if apiClient == nil {
		return nil
	}
	if _, ok := apiClient.(*inspectCompatibilityClient); ok {
		return apiClient
	}
	return &inspectCompatibilityClient{APIClient: apiClient}
}

type inspectCompatibilityClient struct {
	client.APIClient
}

func (c *inspectCompatibilityClient) ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	return ContainerCreateWithCompatibility(ctx, c.APIClient, options)
}

func (c *inspectCompatibilityClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return ContainerInspectWithCompatibility(ctx, c.APIClient, containerID, options)
}

func (c *inspectCompatibilityClient) NetworkInspect(ctx context.Context, networkID string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	return NetworkInspectWithCompatibility(ctx, c.APIClient, networkID, options)
}

func (c *inspectCompatibilityClient) NetworkList(ctx context.Context, options client.NetworkListOptions) (client.NetworkListResult, error) {
	return NetworkListWithCompatibility(ctx, c.APIClient, options)
}

// ContainerInspectWithCompatibility retries inspect decoding against raw daemon
// JSON when the primary typed decode fails on a ParseAddr-style CIDR issue.
func ContainerInspectWithCompatibility(ctx context.Context, apiClient client.APIClient, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if apiClient == nil {
		return client.ContainerInspectResult{}, fmt.Errorf("docker api client is nil")
	}

	result, err := apiClient.ContainerInspect(ctx, containerID, options)
	if err == nil || !isInspectAddressParseErrorInternal(err) {
		return result, err
	}

	query := url.Values{}
	if options.Size {
		query.Set("size", "1")
	}

	raw, fetchErr := fetchDockerAPIJSONInternal(ctx, apiClient, "/containers/"+strings.TrimSpace(containerID)+"/json", query)
	if fetchErr != nil {
		return client.ContainerInspectResult{}, err
	}

	normalized, changed, normalizeContainerInspectRawJSONInternalErr := normalizeContainerInspectRawJSONInternal(raw)
	if normalizeContainerInspectRawJSONInternalErr != nil || !changed {
		return client.ContainerInspectResult{}, err
	}

	var repaired containertypes.InspectResponse
	if unmarshalErr := json.Unmarshal(normalized, &repaired); unmarshalErr != nil {
		return client.ContainerInspectResult{}, err
	}

	return client.ContainerInspectResult{
		Container: repaired,
		Raw:       normalized,
	}, nil
}

// NetworkInspectWithCompatibility retries inspect decoding against raw daemon
// JSON when the primary typed decode fails on a ParseAddr-style CIDR issue.
func NetworkInspectWithCompatibility(ctx context.Context, apiClient client.APIClient, networkID string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	if apiClient == nil {
		return client.NetworkInspectResult{}, fmt.Errorf("docker api client is nil")
	}

	result, err := apiClient.NetworkInspect(ctx, networkID, options)
	if err == nil || !isInspectAddressParseErrorInternal(err) {
		return result, err
	}

	query := url.Values{}
	if options.Verbose {
		query.Set("verbose", "true")
	}
	if scope := strings.TrimSpace(options.Scope); scope != "" {
		query.Set("scope", scope)
	}

	raw, fetchErr := fetchDockerAPIJSONInternal(ctx, apiClient, "/networks/"+strings.TrimSpace(networkID), query)
	if fetchErr != nil {
		return client.NetworkInspectResult{}, err
	}

	normalized, changed, normalizeNetworkInspectRawJSONInternalErr := normalizeNetworkInspectRawJSONInternal(raw)
	if normalizeNetworkInspectRawJSONInternalErr != nil || !changed {
		return client.NetworkInspectResult{}, err
	}

	var repaired networktypes.Inspect
	if unmarshalErr := json.Unmarshal(normalized, &repaired); unmarshalErr != nil {
		return client.NetworkInspectResult{}, err
	}

	return client.NetworkInspectResult{
		Network: repaired,
		Raw:     normalized,
	}, nil
}

// NetworkListWithCompatibility retries list decoding against raw daemon JSON
// when the primary typed decode fails on a ParseAddr-style CIDR issue.
func NetworkListWithCompatibility(ctx context.Context, apiClient client.APIClient, options client.NetworkListOptions) (client.NetworkListResult, error) {
	if apiClient == nil {
		return client.NetworkListResult{}, fmt.Errorf("docker api client is nil")
	}

	result, err := apiClient.NetworkList(ctx, options)
	if err == nil || !isInspectAddressParseErrorInternal(err) {
		return result, err
	}

	query, queryErr := buildNetworkListQueryInternal(options)
	if queryErr != nil {
		return client.NetworkListResult{}, err
	}

	raw, fetchErr := fetchDockerAPIJSONInternal(ctx, apiClient, "/networks", query)
	if fetchErr != nil {
		return client.NetworkListResult{}, err
	}

	normalized, changed, normalizeErr := normalizeNetworkListRawJSONInternal(raw)
	if normalizeErr != nil || !changed {
		return client.NetworkListResult{}, err
	}

	var repaired []networktypes.Summary
	if unmarshalErr := json.Unmarshal(normalized, &repaired); unmarshalErr != nil {
		return client.NetworkListResult{}, err
	}

	return client.NetworkListResult{
		Items: repaired,
	}, nil
}

func fetchDockerAPIJSONInternal(ctx context.Context, apiClient client.APIClient, resourcePath string, query url.Values) ([]byte, error) {
	dialer := apiClient.Dialer()
	if dialer == nil {
		return nil, fmt.Errorf("docker api client does not expose a dialer")
	}

	reqURL := &url.URL{
		Scheme:   "http",
		Host:     client.DummyHost,
		Path:     buildDockerAPIPathInternal(apiClient.ClientVersion(), resourcePath),
		RawQuery: query.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	// The official Moby client uses a TLS-aware dialer here when the daemon is
	// configured with client certificates, so this fallback continues to work
	// for remote tcp+TLS and local socket transports without reimplementing the
	// client's private request machinery.
	conn, err := dialer(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	if err := req.Write(conn); err != nil {
		return nil, err
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker api GET %s failed with %s: %s", resourcePath, resp.Status, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func buildDockerAPIPathInternal(version, resourcePath string) string {
	if trimmedVersion := strings.TrimSpace(strings.TrimPrefix(version, "v")); trimmedVersion != "" {
		return path.Join("/v"+trimmedVersion, resourcePath)
	}
	return path.Join("/", resourcePath)
}

func normalizeContainerInspectRawJSONInternal(raw []byte) ([]byte, bool, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false, err
	}

	changed := false

	networkSettings, ok := asMapInternal(payload["NetworkSettings"])
	if ok {
		changed = normalizeAddressStringFieldInternal(networkSettings, "Gateway") || changed
		changed = normalizeAddressStringFieldInternal(networkSettings, "IPv6Gateway") || changed
		changed = normalizeAddressWithPrefixFieldInternal(networkSettings, "IPAddress", "IPPrefixLen") || changed
		changed = normalizeAddressWithPrefixFieldInternal(networkSettings, "GlobalIPv6Address", "GlobalIPv6PrefixLen") || changed

		if networks, ok := asMapInternal(networkSettings["Networks"]); ok {
			for _, endpointAny := range networks {
				endpoint, ok := asMapInternal(endpointAny)
				if !ok {
					continue
				}

				changed = normalizeAddressStringFieldInternal(endpoint, "Gateway") || changed
				changed = normalizeAddressStringFieldInternal(endpoint, "IPv6Gateway") || changed
				changed = normalizeAddressWithPrefixFieldInternal(endpoint, "IPAddress", "IPPrefixLen") || changed
				changed = normalizeAddressWithPrefixFieldInternal(endpoint, "GlobalIPv6Address", "GlobalIPv6PrefixLen") || changed

				if ipam, ok := asMapInternal(endpoint["IPAMConfig"]); ok {
					changed = normalizeAddressStringFieldInternal(ipam, "IPv4Address") || changed
					changed = normalizeAddressStringFieldInternal(ipam, "IPv6Address") || changed
					changed = normalizeAddressStringSliceFieldInternal(ipam, "LinkLocalIPs") || changed
				}
			}
		}
	}

	if !changed {
		return raw, false, nil
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func normalizeNetworkListRawJSONInternal(raw []byte) ([]byte, bool, error) {
	var payload []any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false, err
	}

	changed := false
	for _, item := range payload {
		networkPayload, ok := asMapInternal(item)
		if !ok {
			continue
		}
		changed = normalizeNetworkObjectInternal(networkPayload) || changed
	}

	if !changed {
		return raw, false, nil
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func normalizeNetworkInspectRawJSONInternal(raw []byte) ([]byte, bool, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false, err
	}

	changed := normalizeNetworkObjectInternal(payload)
	if !changed {
		return raw, false, nil
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func normalizeNetworkObjectInternal(payload map[string]any) bool {
	changed := false
	if ipam, ok := asMapInternal(payload["IPAM"]); ok {
		if configs, ok := asSliceInternal(ipam["Config"]); ok {
			for _, configAny := range configs {
				config, ok := asMapInternal(configAny)
				if !ok {
					continue
				}

				changed = normalizeAddressStringFieldInternal(config, "Gateway") || changed
				changed = normalizeAuxiliaryAddressesInternal(config, "AuxiliaryAddresses") || changed
				changed = normalizeAuxiliaryAddressesInternal(config, "AuxAddress") || changed
			}
		}
	}
	if containers, ok := asMapInternal(payload["Containers"]); ok {
		for _, endpointAny := range containers {
			endpoint, ok := asMapInternal(endpointAny)
			if !ok {
				continue
			}

			changed = normalizePrefixStringFieldInternal(endpoint, "IPv4Address") || changed
			changed = normalizePrefixStringFieldInternal(endpoint, "IPv6Address") || changed
		}
	}
	return changed
}

func normalizeAddressStringFieldInternal(obj map[string]any, key string) bool {
	raw, ok := obj[key].(string)
	if !ok {
		return false
	}

	normalized, changed := normalizeAddressStringInternal(raw)
	if !changed {
		return false
	}

	obj[key] = normalized
	return true
}

func normalizeAddressWithPrefixFieldInternal(obj map[string]any, addrKey, prefixLenKey string) bool {
	raw, ok := obj[addrKey].(string)
	if !ok {
		return false
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if _, err := netip.ParseAddr(trimmed); err == nil {
		if raw == trimmed {
			return false
		}
		obj[addrKey] = trimmed
		return true
	}

	prefix, err := netip.ParsePrefix(trimmed)
	if err != nil {
		return false
	}

	changed := false
	addrValue := prefix.Addr().String()
	if raw != addrValue {
		obj[addrKey] = addrValue
		changed = true
	}

	if prefixLenMissingInternal(obj[prefixLenKey]) {
		obj[prefixLenKey] = prefix.Bits()
		changed = true
	}

	return changed
}

func normalizeAddressStringSliceFieldInternal(obj map[string]any, key string) bool {
	values, ok := asSliceInternal(obj[key])
	if !ok {
		return false
	}

	changed := false
	for i, value := range values {
		raw, ok := value.(string)
		if !ok {
			continue
		}
		normalized, fieldChanged := normalizeAddressStringInternal(raw)
		if !fieldChanged {
			continue
		}
		values[i] = normalized
		changed = true
	}

	if changed {
		obj[key] = values
	}
	return changed
}

func normalizeAuxiliaryAddressesInternal(obj map[string]any, key string) bool {
	auxMap, ok := asMapInternal(obj[key])
	if !ok {
		return false
	}

	changed := false
	for auxKey, auxValue := range auxMap {
		raw, ok := auxValue.(string)
		if !ok {
			continue
		}
		normalized, fieldChanged := normalizeAddressStringInternal(raw)
		if !fieldChanged {
			continue
		}
		auxMap[auxKey] = normalized
		changed = true
	}

	if changed {
		obj[key] = auxMap
	}
	return changed
}

func normalizePrefixStringFieldInternal(obj map[string]any, key string) bool {
	raw, ok := obj[key].(string)
	if !ok {
		return false
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}

	prefix, err := netip.ParsePrefix(trimmed)
	if err != nil {
		return false
	}

	normalized := prefix.String()
	if normalized == raw {
		return false
	}

	obj[key] = normalized
	return true
}

func normalizeAddressStringInternal(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw, false
	}
	if _, err := netip.ParseAddr(trimmed); err == nil {
		return trimmed, trimmed != raw
	}

	prefix, err := netip.ParsePrefix(trimmed)
	if err != nil {
		return raw, false
	}

	normalized := prefix.Addr().String()
	return normalized, normalized != raw
}

func prefixLenMissingInternal(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case float64:
		return v == 0
	case int:
		return v == 0
	case int32:
		return v == 0
	case int64:
		return v == 0
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return true
		}
		parsed, err := strconv.Atoi(trimmed)
		return err != nil || parsed == 0
	default:
		return false
	}
}

func asMapInternal(value any) (map[string]any, bool) {
	out, ok := value.(map[string]any)
	return out, ok
}

func asSliceInternal(value any) ([]any, bool) {
	out, ok := value.([]any)
	return out, ok
}

func buildNetworkListQueryInternal(options client.NetworkListOptions) (url.Values, error) {
	query := url.Values{}
	if len(options.Filters) == 0 {
		return query, nil
	}

	filtersJSON, err := json.Marshal(options.Filters)
	if err != nil {
		return nil, err
	}
	query.Set("filters", string(filtersJSON))
	return query, nil
}

func isInspectAddressParseErrorInternal(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "ParseAddr(") || strings.Contains(msg, "ParsePrefix(")
}
