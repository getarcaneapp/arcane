package cmdutil

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
)

// ListSpec describes a paginated list with raw JSON passthrough or table rendering.
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

	Headers []string
	Row     func(T) []string
}

// RunList executes one standard list command as described by spec.
func RunList[T any](cmd *cobra.Command, c *client.Client, spec ListSpec[T]) error {
	loading := output.StartTracker(cmd.Context(), cmd.ErrOrStderr(), !JSONOutputEnabled(cmd), "Loading "+spec.Resource)
	loading.NextStep()
	defer loading.Stop()
	path, err := ApplyPaginationParams(cmd, spec.Endpoint, spec.Params)
	if err != nil {
		return fmt.Errorf("failed to build pagination query: %w", err)
	}
	if len(spec.Query) > 0 {
		path = AppendQuery(path, spec.Query)
	}

	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", spec.Resource, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := ReadJSONBody(resp)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", spec.Resource, err)
	}

	merged, _ := cmd.Context().Value(mergedScopeKey{}).(*mergedScope)
	if JSONOutputEnabled(cmd) {
		if merged != nil {
			var envelope base.Paginated[jsontext.Value]
			if unmarshalErr := json.Unmarshal(body, &envelope); unmarshalErr != nil {
				return fmt.Errorf("failed to parse response: %w", unmarshalErr)
			}
			merged.failures = append(merged.failures, envelope.FailedEnvironments...)
		}
		return PrintRawJSON(body)
	}

	var result base.Paginated[T]
	if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
		return fmt.Errorf("failed to parse response: %w", unmarshalErr)
	}

	loading.Finish("", nil)
	loading.Stop()
	headers, rows := spec.Headers, make([][]string, len(result.Data))
	if merged != nil {
		merged.failures = append(merged.failures, result.FailedEnvironments...)
		headers = slices.Concat([]string{"ENVIRONMENT"}, spec.Headers)
	}
	for i, item := range result.Data {
		rows[i] = spec.Row(item)
		if merged == nil {
			continue
		}
		var ref base.EnvironmentRef
		if tagged, ok := any(item).(interface{ Environment() base.EnvironmentRef }); ok {
			ref = tagged.Environment()
		}
		rows[i] = slices.Concat([]string{cmp.Or(ref.Name, merged.names[cmp.Or(ref.ID, c.EnvID())], ref.ID)}, rows[i])
	}

	return PrintTable(cmd, headers, rows, TableSummary{Resource: spec.Resource, Total: result.Pagination.TotalItems, Paginated: true})
}

// Spec describes a request with raw JSON data output or typed text rendering.
// Render is required in text mode; a nil Render returns a programmer error before requesting.
type Spec[T any] struct {
	Method, Path, FailureMessage string
	Body                         any
	Timeout                      time.Duration
	Render                       func(T) error
}

// Run enforces a success envelope and prints raw data in JSON mode or invokes Render in text mode.
func Run[T any](cmd *cobra.Command, c *client.Client, spec Spec[T]) error {
	jsonMode := JSONOutputEnabled(cmd)
	if !jsonMode && spec.Render == nil {
		return errors.New("request runner requires Render in text mode")
	}
	if spec.Timeout > 0 {
		c.SetTimeout(spec.Timeout)
	}
	loading := output.StartTracker(cmd.Context(), cmd.ErrOrStderr(), !jsonMode, cmd.Short)
	loading.NextStep()
	defer loading.Stop()
	result, err := c.DoEnvelope[jsontext.Value](cmd.Context(), spec.Method, spec.Path, spec.Body)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.FailureMessage, err)
	}
	loading.Finish("", nil)
	loading.Stop()
	merged, isMerged := cmd.Context().Value(mergedScopeKey{}).(*mergedScope)
	if isMerged {
		merged.failures = append(merged.failures, result.FailedEnvironments...)
	}
	if jsonMode && isMerged {
		return PrintJSON(result)
	}
	if jsonMode {
		if len(result.Data) == 0 {
			return PrintJSON(nil)
		}
		return PrintRawJSON(result.Data)
	}
	var data T
	if len(result.Data) > 0 {
		if decodeErr := json.Unmarshal(result.Data, &data); decodeErr != nil {
			return fmt.Errorf("%s: failed to decode response: %w", spec.FailureMessage, decodeErr)
		}
	}
	return spec.Render(data)
}

// DeleteSpec describes a confirmed DELETE with optional body and human-readable success output.
type DeleteSpec struct {
	Force                                        bool
	Prompt, Path, FailureMessage, SuccessMessage string
	Body                                         any
}

// RunDelete returns nil on cancellation, otherwise printing raw data in JSON mode or SuccessMessage in text mode.
func RunDelete(cmd *cobra.Command, c *client.Client, spec DeleteSpec) error {
	confirmed, err := ConfirmOrCancel(cmd, spec.Force, spec.Prompt)
	if err != nil || !confirmed {
		return err
	}
	return Run(cmd, c, Spec[jsontext.Value]{
		Method:         http.MethodDelete,
		Path:           spec.Path,
		Body:           spec.Body,
		FailureMessage: spec.FailureMessage,
		Render: func(jsontext.Value) error {
			output.Success("%s", spec.SuccessMessage)
			return nil
		},
	})
}
