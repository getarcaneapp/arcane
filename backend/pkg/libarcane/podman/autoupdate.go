package podman

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
)

func (c *Client) AutoUpdate(ctx context.Context, opts types.PodmanAutoUpdateOptions) (types.PodmanAutoUpdateResult, error) {
	query, err := filtersQuery(url.Values{"dryRun": {strconv.FormatBool(opts.DryRun)}}, opts.Filters)
	if err != nil {
		return types.PodmanAutoUpdateResult{}, err
	}
	if opts.Rollback != nil {
		query.Set("rollback", strconv.FormatBool(*opts.Rollback))
	}
	if opts.TLSVerify != nil {
		query.Set("tlsVerify", strconv.FormatBool(*opts.TLSVerify))
	}
	if opts.Authfile != "" {
		query.Set("authfile", opts.Authfile)
	}

	out, err := c.doJSON[types.PodmanAutoUpdateResult](ctx, request{
		method: http.MethodPost,
		path:   "/autoupdate",
		query:  query,
	})
	return types.PodmanAutoUpdateResult{
		Mode:    types.PodmanAutoUpdateModeREST,
		Reports: out.Reports,
		Errors:  out.Errors,
	}, err
}
