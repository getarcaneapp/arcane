package cmdutil

import (
	"encoding/json/v2"
	"net/url"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/spf13/cobra"
)

// ListSpec describes one standard list command: fetch a paginated endpoint,
// echo the raw body for --json (so unmodeled fields like counts and groups
// survive, see ReadJSONBody), otherwise render a table.
type ListSpec[T any] struct {
	// Resource names the collection in error messages and the trailing
	// "Showing x of y <resource>" line.
	Resource string
	// Endpoint is the list path (environment already applied).
	Endpoint string
	// Params configures pagination from the command's flags.
	Params ListParams
	// Query holds extra query parameters, applied after pagination.
	Query url.Values
	// JSON selects raw JSON passthrough output.
	JSON bool

	Headers []string
	Row     func(T) []string
}

// RunList executes one standard list command as described by spec.
func RunList[T any](cmd *cobra.Command, c *client.Client, spec ListSpec[T]) error {
	path, err := ApplyPaginationParams(cmd, spec.Endpoint, spec.Params)
	if err != nil {
		return errors.WrapIf(err, "failed to build pagination query")
	}
	if len(spec.Query) > 0 {
		path = AppendQuery(path, spec.Query)
	}

	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return errors.WrapIff(err, "failed to list %s", spec.Resource)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := ReadJSONBody(resp)
	if err != nil {
		return errors.WrapIff(err, "failed to list %s", spec.Resource)
	}

	if spec.JSON {
		return PrintRawJSON(body)
	}

	var result base.Paginated[T]
	if err := json.Unmarshal(body, &result); err != nil {
		return errors.WrapIf(err, "failed to parse response")
	}

	rows := make([][]string, len(result.Data))
	for i, item := range result.Data {
		rows[i] = spec.Row(item)
	}

	output.Table(spec.Headers, rows)
	output.Showing(len(result.Data), result.Pagination.TotalItems, spec.Resource)
	return nil
}

// PostActionSpec describes one resolve→POST→decode→report action command. T
// is the API envelope's data payload.
type PostActionSpec struct {
	// Path is the fully built action path.
	Path string
	// Body is the optional JSON request body.
	Body any
	// FailureMessage wraps transport and status errors.
	FailureMessage string
	// SuccessMessage is the preformatted human-readable success line.
	SuccessMessage string
	// Timeout optionally extends the client timeout for slow actions.
	Timeout time.Duration
	// JSON prints the decoded response data instead of the success line.
	JSON bool
}

// RunPostAction executes one POST action command as described by spec.
func RunPostAction[T any](cmd *cobra.Command, c *client.Client, spec PostActionSpec) error {
	if spec.Timeout > 0 {
		c.SetTimeout(spec.Timeout)
	}

	result, err := c.PostJSON[T](cmd.Context(), spec.Path, spec.Body)
	if err != nil {
		return errors.WrapIff(err, "%s", spec.FailureMessage)
	}

	if spec.JSON {
		return PrintJSON(result.Data)
	}

	output.Success("%s", spec.SuccessMessage)
	return nil
}
