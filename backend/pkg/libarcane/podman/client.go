package podman

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"golang.org/x/mod/semver"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

const apiPrefix = "/v4.0.0/libpod"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Runtime struct {
	Client   *Client
	Version  types.PodmanVersion
	Rootless bool
	Systemd  Systemd
}

type Features struct {
	QuadletsSupported    bool
	QuadletAppsSupported bool
	AutoUpdateMode       types.PodmanAutoUpdateMode
}

type request struct {
	method      string
	path        string
	query       url.Values
	body        io.Reader
	contentType string
}

type apiError struct {
	Cause    string `json:"cause"`
	Message  string `json:"message"`
	Response int    `json:"response"`
}

func NewClient(host string) (*Client, error) {
	parsed, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse podman host %q: %w", host, err)
	}

	switch parsed.Scheme {
	case "unix":
		return &Client{
			baseURL: "http://podman",
			httpClient: &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, "unix", parsed.Path)
					},
				},
			},
		}, nil
	case "tcp", "http":
		return &Client{
			baseURL:    "http://" + parsed.Host,
			httpClient: &http.Client{},
		}, nil
	case "https":
		return &Client{
			baseURL:    "https://" + parsed.Host,
			httpClient: &http.Client{},
		}, nil
	default:
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("unsupported podman host scheme %q", parsed.Scheme))
	}
}

func DetectFeatures(version string, systemdAvailable bool) Features {
	v := kit.EnsurePrefix(strings.TrimSpace(version), "v")
	return Features{
		QuadletsSupported:    semver.Compare(v, "v5.8.0") >= 0,
		QuadletAppsSupported: semver.Compare(v, "v6.1.0") >= 0,
		AutoUpdateMode: func() types.PodmanAutoUpdateMode {
			switch {
			case semver.Compare(v, "v6.2.0") >= 0:
				return types.PodmanAutoUpdateModeREST
			case systemdAvailable:
				return types.PodmanAutoUpdateModeSystemd
			default:
				return types.PodmanAutoUpdateModeUnavailable
			}
		}(),
	}
}

func (c *Client) Version(ctx context.Context) (types.PodmanVersion, error) {
	return c.doJSON[types.PodmanVersion](ctx, request{method: http.MethodGet, path: "/version"})
}

func (c *Client) Info(ctx context.Context) (types.PodmanInfo, error) {
	return c.doJSON[types.PodmanInfo](ctx, request{method: http.MethodGet, path: "/info"})
}

type containerResponse struct {
	types.PodmanContainer
	Ports []PodPortMapping `json:"Ports"`
}

func (c *Client) ContainerList(ctx context.Context, labels, pods []string) ([]types.PodmanContainer, error) {
	query, err := filtersQuery(url.Values{"all": {"true"}}, map[string][]string{
		"label": labels,
		"pod":   pods,
	})
	if err != nil {
		return nil, err
	}

	raw, err := c.doJSON[[]containerResponse](ctx, request{
		method: http.MethodGet,
		path:   "/containers/json",
		query:  query,
	})
	if err != nil {
		return nil, err
	}

	containers := make([]types.PodmanContainer, 0, len(raw))
	for _, item := range raw {
		ports := make([]types.PodPortMapping, 0, len(item.Ports))
		for _, port := range item.Ports {
			ports = append(ports, types.PodPortMapping(port))
		}
		containers = append(containers, types.PodmanContainer{
			ID:        item.ID,
			Names:     item.Names,
			Image:     item.Image,
			ImageID:   item.ImageID,
			State:     item.State,
			Status:    item.Status,
			Created:   item.Created,
			StartedAt: item.StartedAt,
			ExitCode:  item.ExitCode,
			Exited:    item.Exited,
			Labels:    item.Labels,
			Pod:       item.Pod,
			PodName:   item.PodName,
			IsInfra:   item.IsInfra,
			Ports:     ports,
		})
	}
	return containers, nil
}

func (c *Client) doJSON[T any](ctx context.Context, req request) (T, error) {
	var out T
	body, err := c.do(ctx, req)
	if err != nil {
		return out, err
	}
	defer func() { _ = body.Close() }()

	if decodeErr := json.NewDecoder(body).Decode(&out); decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		return out, fmt.Errorf("decode podman %s %s: %w", req.method, req.path, decodeErr)
	}
	return out, nil
}

func (c *Client) doText(ctx context.Context, req request) (string, error) {
	body, err := c.do(ctx, req)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("read podman %s %s: %w", req.method, req.path, err)
	}
	return string(data), nil
}

func (c *Client) do(ctx context.Context, req request) (io.ReadCloser, error) {
	target := c.baseURL + apiPrefix + req.path
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, target, req.body)
	if err != nil {
		return nil, fmt.Errorf("build podman request: %w", err)
	}
	if req.contentType != "" {
		httpReq.Header.Set("Content-Type", req.contentType)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("podman %s %s: %w", req.method, req.path, err))
	}
	if resp.StatusCode < http.StatusBadRequest {
		return resp.Body, nil
	}
	defer func() { _ = resp.Body.Close() }()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var apiErr apiError
	message := strings.TrimSpace(string(data))
	if json.Unmarshal(data, &apiErr) == nil {
		message = cmp.Or(apiErr.Message, apiErr.Cause, message)
	}
	err = fmt.Errorf("podman %s %s: %s", req.method, req.path, cmp.Or(message, resp.Status))

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return nil, common.Classify(common.ErrBadRequest, err)
	case http.StatusNotFound:
		return nil, common.Classify(common.ErrNotFound, err)
	case http.StatusConflict:
		return nil, common.Classify(common.ErrConflict, err)
	case http.StatusServiceUnavailable:
		return nil, common.Classify(common.ErrUnavailable, err)
	default:
		return nil, err
	}
}

func filtersQuery(query url.Values, filters map[string][]string) (url.Values, error) {
	set := make(map[string][]string, len(filters))
	for key, values := range filters {
		if len(values) > 0 {
			set[key] = values
		}
	}
	if len(set) == 0 {
		return query, nil
	}

	encoded, err := json.Marshal(set)
	if err != nil {
		return nil, fmt.Errorf("encode podman filters: %w", err)
	}
	query.Set("filters", string(encoded))
	return query, nil
}
