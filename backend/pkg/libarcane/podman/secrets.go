package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
)

func (c *Client) SecretList(ctx context.Context, filters map[string][]string) ([]types.PodmanSecret, error) {
	query, err := filtersQuery(url.Values{}, filters)
	if err != nil {
		return nil, err
	}

	return c.doJSON[[]types.PodmanSecret](ctx, request{
		method: http.MethodGet,
		path:   "/secrets/json",
		query:  query,
	})
}

func (c *Client) SecretInspect(ctx context.Context, nameOrID string) (types.PodmanSecret, error) {
	return c.doJSON[types.PodmanSecret](ctx, request{
		method: http.MethodGet,
		path:   "/secrets/" + url.PathEscape(nameOrID) + "/json",
	})
}

func (c *Client) SecretCreate(ctx context.Context, req types.PodmanSecretCreateRequest, replace bool) (string, error) {
	query := url.Values{"name": {req.Name}}
	if replace {
		query.Set("replace", "true")
	}
	if req.Driver != "" {
		query.Set("driver", req.Driver)
	}
	for key, values := range map[string]map[string]string{
		"driveropts": req.DriverOptions,
		"labels":     req.Labels,
	} {
		if len(values) == 0 {
			continue
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return "", fmt.Errorf("encode secret %s: %w", key, err)
		}
		query.Set(key, string(encoded))
	}

	out, err := c.doJSON[types.PodmanSecretCreateResponse](ctx, request{
		method:      http.MethodPost,
		path:        "/secrets/create",
		query:       query,
		body:        strings.NewReader(req.Data),
		contentType: "application/octet-stream",
	})
	return out.ID, err
}

func (c *Client) SecretRemove(ctx context.Context, nameOrID string) error {
	_, err := c.doText(ctx, request{method: http.MethodDelete, path: "/secrets/" + url.PathEscape(nameOrID)})
	return err
}
