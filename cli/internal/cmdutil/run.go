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

	"github.com/spf13/cobra"

	"getarcane.dev/cli/v3/internal/client"
	"getarcane.dev/cli/v3/internal/output"
	"getarcane.dev/types/v3"
)

type (
	ListSpec[T any] struct {
		Query url.Values

		Row      func(T) []string
		Resource string
		Endpoint string

		Headers []string
		Params  ListParams
	}

	Spec[T any] struct {
		Body                         any
		Render                       func(T) error
		Method, Path, FailureMessage string
		Timeout                      time.Duration
	}

	DeleteSpec struct {
		Body                                         any
		Prompt, Path, FailureMessage, SuccessMessage string
		Force                                        bool
	}
)

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

	resp, err := c.Request(cmd.Context(), http.MethodGet, path, nil)
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
			var envelope types.Paginated[jsontext.Value]
			if unmarshalErr := json.Unmarshal(body, &envelope); unmarshalErr != nil {
				return fmt.Errorf("failed to parse response: %w", unmarshalErr)
			}
			merged.failures = append(merged.failures, envelope.FailedEnvironments...)
		}
		return PrintRawJSON(body)
	}

	var result types.Paginated[T]
	if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
		return fmt.Errorf("failed to parse response: %w", unmarshalErr)
	}

	loading.Finish("", nil)
	loading.Stop()
	headers, rows := spec.Headers, make([][]string, len(result.Data))
	if merged != nil {
		merged.failures = append(merged.failures, result.FailedEnvironments...)
		headers = slices.Concat([]string{"AGENT"}, spec.Headers)
	}
	for i, item := range result.Data {
		rows[i] = spec.Row(item)
		if merged == nil {
			continue
		}
		var ref types.EnvironmentRef
		if tagged, ok := any(item).(interface{ Environment() types.EnvironmentRef }); ok {
			ref = tagged.Environment()
		}
		rows[i] = slices.Concat([]string{cmp.Or(ref.Name, merged.names[cmp.Or(ref.ID, c.EnvID())], ref.ID)}, rows[i])
	}

	return PrintTable(cmd, headers, rows, TableSummary{Resource: spec.Resource, Total: result.Pagination.TotalItems, Paginated: true})
}

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

func RunDelete(cmd *cobra.Command, c *client.Client, spec DeleteSpec) error {
	confirmed, err := ConfirmOrCancel(cmd, ConfirmOptions{Label: spec.Prompt, Force: spec.Force})
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
