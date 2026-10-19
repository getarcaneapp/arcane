package podman

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
)

func (c *Client) KubePlay(ctx context.Context, req podmantypes.KubePlayRequest) (podmantypes.KubePlayReport, error) {
	query := url.Values{
		"replace": {strconv.FormatBool(req.Replace)},
		"network": req.Networks,
	}
	if req.Start != nil {
		query.Set("start", strconv.FormatBool(*req.Start))
	}

	var out podmantypes.KubePlayReport
	err := c.doJSON(ctx, request{
		method:      http.MethodPost,
		path:        "/play/kube",
		query:       query,
		body:        strings.NewReader(req.YAML),
		contentType: "application/yaml",
	}, &out)
	return out, err
}

func (c *Client) KubeDown(ctx context.Context, req podmantypes.KubeDownRequest) (podmantypes.KubeDownReport, error) {
	var out podmantypes.KubeDownReport
	err := c.doJSON(ctx, request{
		method:      http.MethodDelete,
		path:        "/play/kube",
		query:       url.Values{"force": {strconv.FormatBool(req.Force)}},
		body:        strings.NewReader(req.YAML),
		contentType: "application/yaml",
	}, &out)
	return out, err
}

func (c *Client) KubeGenerate(ctx context.Context, req podmantypes.KubeGenerateRequest) (string, error) {
	query := url.Values{
		"names":   req.Names,
		"service": {strconv.FormatBool(req.Service)},
	}
	if req.Type != "" {
		query.Set("type", string(req.Type))
	}

	return c.doText(ctx, request{
		method: http.MethodGet,
		path:   "/generate/kube",
		query:  query,
	})
}
