package podman

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
)

type QuadletInstallOptions struct {
	File          types.QuadletFile
	ExtraFiles    []types.QuadletFile
	Replace       bool
	Application   string
	AppsSupported bool
}

type QuadletRemoveOptions struct {
	Force  bool
	Ignore bool
}

func (c *Client) QuadletList(ctx context.Context, filters map[string][]string) ([]types.Quadlet, error) {
	query, err := filtersQuery(url.Values{}, filters)
	if err != nil {
		return nil, err
	}

	return c.doJSON[[]types.Quadlet](ctx, request{
		method: http.MethodGet,
		path:   "/quadlets/json",
		query:  query,
	})
}

func (c *Client) QuadletFile(ctx context.Context, name string) (string, error) {
	return c.doText(ctx, request{method: http.MethodGet, path: "/quadlets/" + url.PathEscape(name) + "/file"})
}

func (c *Client) QuadletInstall(ctx context.Context, opts QuadletInstallOptions) (types.QuadletInstallReport, error) {
	archive, err := buildTar(append([]types.QuadletFile{opts.File}, opts.ExtraFiles...))
	if err != nil {
		return types.QuadletInstallReport{}, err
	}

	query := url.Values{"replace": {strconv.FormatBool(opts.Replace)}}
	if opts.AppsSupported && opts.Application != "" {
		query.Set("application", opts.Application)
	}

	return c.doJSON[types.QuadletInstallReport](ctx, request{
		method:      http.MethodPost,
		path:        "/quadlets",
		query:       query,
		body:        archive,
		contentType: "application/x-tar",
	})
}

func (c *Client) QuadletRemove(ctx context.Context, name string, opts QuadletRemoveOptions) (types.QuadletRemoveReport, error) {
	return c.doJSON[types.QuadletRemoveReport](ctx, request{
		method: http.MethodDelete,
		path:   "/quadlets/" + url.PathEscape(name),
		query: url.Values{
			"force":  {strconv.FormatBool(opts.Force)},
			"ignore": {strconv.FormatBool(opts.Ignore)},
		},
	})
}
