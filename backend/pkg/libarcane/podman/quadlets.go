package podman

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
)

type QuadletInstallOptions struct {
	File          podmantypes.QuadletFile
	ExtraFiles    []podmantypes.QuadletFile
	Replace       bool
	Application   string
	AppsSupported bool
}

type QuadletRemoveOptions struct {
	Force  bool
	Ignore bool
}

func (c *Client) QuadletList(ctx context.Context, filters map[string][]string) ([]podmantypes.Quadlet, error) {
	query, err := filtersQuery(url.Values{}, filters)
	if err != nil {
		return nil, err
	}

	var out []podmantypes.Quadlet
	err = c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/quadlets/json",
		query:  query,
	}, &out)
	return out, err
}

func (c *Client) QuadletFile(ctx context.Context, name string) (string, error) {
	return c.doText(ctx, request{method: http.MethodGet, path: "/quadlets/" + url.PathEscape(name) + "/file"})
}

func (c *Client) QuadletInstall(ctx context.Context, opts QuadletInstallOptions) (podmantypes.QuadletInstallReport, error) {
	archive, err := buildTar(append([]podmantypes.QuadletFile{opts.File}, opts.ExtraFiles...))
	if err != nil {
		return podmantypes.QuadletInstallReport{}, err
	}

	query := url.Values{"replace": {strconv.FormatBool(opts.Replace)}}
	if opts.AppsSupported && opts.Application != "" {
		query.Set("application", opts.Application)
	}

	var out podmantypes.QuadletInstallReport
	err = c.doJSON(ctx, request{
		method:      http.MethodPost,
		path:        "/quadlets",
		query:       query,
		body:        archive,
		contentType: "application/x-tar",
	}, &out)
	return out, err
}

func (c *Client) QuadletRemove(ctx context.Context, name string, opts QuadletRemoveOptions) (podmantypes.QuadletRemoveReport, error) {
	var out podmantypes.QuadletRemoveReport
	err := c.doJSON(ctx, request{
		method: http.MethodDelete,
		path:   "/quadlets/" + url.PathEscape(name),
		query: url.Values{
			"force":  {strconv.FormatBool(opts.Force)},
			"ignore": {strconv.FormatBool(opts.Ignore)},
		},
	}, &out)
	return out, err
}
