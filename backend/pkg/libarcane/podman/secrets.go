package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
)

func (c *Client) SecretList(ctx context.Context, filters map[string][]string) ([]podmantypes.Secret, error) {
	query, err := filtersQuery(url.Values{}, filters)
	if err != nil {
		return nil, err
	}

	var out []podmantypes.Secret
	err = c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/secrets/json",
		query:  query,
	}, &out)
	return out, err
}

func (c *Client) SecretCreate(ctx context.Context, req podmantypes.SecretCreateRequest) (string, error) {
	query := url.Values{"name": {req.Name}}
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

	var out struct {
		ID string `json:"ID"`
	}
	err := c.doJSON(ctx, request{
		method:      http.MethodPost,
		path:        "/secrets/create",
		query:       query,
		body:        strings.NewReader(req.Data),
		contentType: "application/octet-stream",
	}, &out)
	return out.ID, err
}

func (c *Client) SecretRemove(ctx context.Context, nameOrID string) error {
	return c.doJSON(ctx, request{method: http.MethodDelete, path: "/secrets/" + url.PathEscape(nameOrID)}, nil)
}
