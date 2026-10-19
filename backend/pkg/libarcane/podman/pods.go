package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

type PodAction string

const (
	PodActionStart   PodAction = "start"
	PodActionStop    PodAction = "stop"
	PodActionRestart PodAction = "restart"
	PodActionPause   PodAction = "pause"
	PodActionUnpause PodAction = "unpause"
	PodActionKill    PodAction = "kill"
)

type PodActionOptions struct {
	Timeout *int
	Signal  string
}

type PodPortMapping struct {
	ContainerPort uint16 `json:"container_port"`
	HostPort      uint16 `json:"host_port,omitempty"`
	HostIP        string `json:"host_ip,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Range         uint16 `json:"range,omitempty"`
}

type PodNamespace struct {
	NSMode string `json:"nsmode,omitempty"`
	Value  string `json:"value,omitempty"`
}

type PodNetworkOptions struct {
	Aliases       []string `json:"aliases,omitempty"`
	InterfaceName string   `json:"interface_name,omitempty"`
}

type PodSpec struct {
	Name             string                       `json:"name"`
	Hostname         string                       `json:"hostname,omitempty"`
	Labels           map[string]string            `json:"labels,omitempty"`
	NoInfra          bool                         `json:"no_infra,omitempty"`
	SharedNamespaces []string                     `json:"shared_namespaces,omitempty"`
	PortMappings     []PodPortMapping             `json:"portmappings,omitempty"`
	NetNS            *PodNamespace                `json:"netns,omitempty"`
	Networks         map[string]PodNetworkOptions `json:"Networks,omitempty"`
	Advanced         json.RawMessage              `json:"-"`
}

func (c *Client) PodList(ctx context.Context, filters map[string][]string) ([]podmantypes.Pod, error) {
	query, err := filtersQuery(url.Values{}, filters)
	if err != nil {
		return nil, err
	}

	var out []podmantypes.Pod
	err = c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/pods/json",
		query:  query,
	}, &out)
	return out, err
}

func (c *Client) PodInspect(ctx context.Context, nameOrID string) (podmantypes.PodInspect, error) {
	var out podmantypes.PodInspect
	err := c.doJSON(ctx, request{method: http.MethodGet, path: "/pods/" + url.PathEscape(nameOrID) + "/json"}, &out)
	return out, err
}

func (c *Client) PodCreate(ctx context.Context, spec PodSpec) (string, error) {
	typed, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode pod spec: %w", err)
	}

	fields := map[string]json.RawMessage{}
	if typedErr := json.Unmarshal(typed, &fields); typedErr != nil {
		return "", fmt.Errorf("encode pod spec: %w", typedErr)
	}
	if len(spec.Advanced) > 0 {
		if advancedErr := json.Unmarshal(spec.Advanced, &fields); advancedErr != nil {
			return "", common.Classify(common.ErrBadRequest, fmt.Errorf("decode advanced pod spec: %w", advancedErr))
		}
	}

	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("encode pod spec: %w", err)
	}

	var out struct {
		ID string `json:"Id"`
	}
	err = c.doJSON(ctx, request{
		method:      http.MethodPost,
		path:        "/pods/create",
		body:        bytes.NewReader(encoded),
		contentType: "application/json",
	}, &out)
	return out.ID, err
}

func (c *Client) PodAction(ctx context.Context, nameOrID string, action PodAction, opts PodActionOptions) error {
	query := url.Values{}
	if opts.Timeout != nil && action == PodActionStop {
		query.Set("t", strconv.Itoa(*opts.Timeout))
	}
	if opts.Signal != "" && action == PodActionKill {
		query.Set("signal", opts.Signal)
	}

	return c.doJSON(ctx, request{
		method: http.MethodPost,
		path:   "/pods/" + url.PathEscape(nameOrID) + "/" + string(action),
		query:  query,
	}, nil)
}

func (c *Client) PodRemove(ctx context.Context, nameOrID string, force bool) error {
	return c.doJSON(ctx, request{
		method: http.MethodDelete,
		path:   "/pods/" + url.PathEscape(nameOrID),
		query:  url.Values{"force": {strconv.FormatBool(force)}},
	}, nil)
}

func (c *Client) PodPrune(ctx context.Context) ([]podmantypes.PodPruneReport, error) {
	var out []podmantypes.PodPruneReport
	err := c.doJSON(ctx, request{method: http.MethodPost, path: "/pods/prune"}, &out)
	return out, err
}

func (c *Client) PodTop(ctx context.Context, nameOrID, psArgs string) (podmantypes.PodTop, error) {
	query := url.Values{"stream": {"false"}}
	if psArgs != "" {
		query.Set("ps_args", psArgs)
	}

	var out podmantypes.PodTop
	err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/pods/" + url.PathEscape(nameOrID) + "/top",
		query:  query,
	}, &out)
	return out, err
}

func (c *Client) PodStats(ctx context.Context, namesOrIDs []string) ([]podmantypes.PodStats, error) {
	query := url.Values{
		"stream":     {"false"},
		"namesOrIDs": namesOrIDs,
	}
	if len(namesOrIDs) == 0 {
		query.Set("all", "true")
	}

	var out []podmantypes.PodStats
	err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/pods/stats",
		query:  query,
	}, &out)
	return out, err
}
