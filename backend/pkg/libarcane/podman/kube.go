package podman

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
)

func (c *Client) KubePlay(ctx context.Context, req types.KubePlayRequest) (types.KubePlayReport, error) {
	query := url.Values{
		"replace": {strconv.FormatBool(req.Replace)},
		"network": req.Networks,
	}
	if req.Start != nil {
		query.Set("start", strconv.FormatBool(*req.Start))
	}

	return c.doJSON[types.KubePlayReport](ctx, request{
		method:      http.MethodPost,
		path:        "/play/kube",
		query:       query,
		body:        strings.NewReader(req.YAML),
		contentType: "application/yaml",
	})
}

func (c *Client) KubeDown(ctx context.Context, req types.KubeDownRequest) (types.KubeDownReport, error) {
	return c.doJSON[types.KubeDownReport](ctx, request{
		method:      http.MethodDelete,
		path:        "/play/kube",
		query:       url.Values{"force": {strconv.FormatBool(req.Force)}},
		body:        strings.NewReader(req.YAML),
		contentType: "application/yaml",
	})
}

func (c *Client) KubeGenerate(ctx context.Context, req types.KubeGenerateRequest) (string, error) {
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
