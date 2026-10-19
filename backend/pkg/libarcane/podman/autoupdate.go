package podman

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
)

func (c *Client) AutoUpdate(ctx context.Context, opts podmantypes.AutoUpdateOptions) (podmantypes.AutoUpdateResult, error) {
	query, err := filtersQuery(url.Values{"dryRun": {strconv.FormatBool(opts.DryRun)}}, opts.Filters)
	if err != nil {
		return podmantypes.AutoUpdateResult{}, err
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

	var out struct {
		Reports []podmantypes.AutoUpdateReport `json:"Reports"`
		Errors  []string                       `json:"Errors"`
	}
	err = c.doJSON(ctx, request{
		method: http.MethodPost,
		path:   "/autoupdate",
		query:  query,
	}, &out)
	return podmantypes.AutoUpdateResult{
		Mode:    podmantypes.AutoUpdateModeREST,
		Reports: out.Reports,
		Errors:  out.Errors,
	}, err
}
