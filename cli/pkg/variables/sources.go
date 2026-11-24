package variables

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"kit.getarcane.app"

	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

type (
	sourceFlags struct {
		inputs         map[string]string
		credentials    map[string]string
		name           string
		preset         string
		definitionFile string
		credentialFile string
		path           string
		keyPrefix      string
		envIDs         []string
		inject         bool
		disabled       bool
	}
)

func newSourcesCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "sources",
		Aliases: []string{"source", "secret-sources"},
		Short:   "Manage external secret sources",
	}
	command.AddCommand(newSourcesListCmd())
	command.AddCommand(newSourcesPresetsCmd())
	command.AddCommand(newSourcesCreateCmd())
	command.AddCommand(newSourcesUpdateCmd())
	command.AddCommand(newSourcesDeleteCmd())
	command.AddCommand(newSourcesRefreshCmd())
	command.AddCommand(newSourcesTestCmd())
	return command
}

func bindSourceFlags(command *cobra.Command, opts *sourceFlags) {
	command.Flags().StringVar(&opts.name, "name", "", "Source name")
	command.Flags().StringVar(&opts.preset, "preset", "", "Preset ID (see 'variables sources presets')")
	command.Flags().StringVar(&opts.definitionFile, "definition-file", "", "Custom definition YAML file; overrides --preset")
	command.Flags().StringToStringVar(&opts.inputs, "input", nil, "Input value as key=value (repeatable)")
	command.Flags().StringToStringVar(&opts.credentials, "credential", nil, "Credential value as key=value (repeatable; an empty value clears the key on update)")
	command.Flags().StringVar(&opts.credentialFile, "credentials-file", "", "File of KEY=VALUE credential lines (use - for stdin); --credential overrides its keys")
	command.Flags().StringVar(&opts.path, "path", "", "Path passed to the definition")
	command.Flags().StringVar(&opts.keyPrefix, "key-prefix", "", "Prefix added to every key")
	command.Flags().StringArrayVar(&opts.envIDs, "agent", nil, "Agent ID to scope the source to (repeatable; pass \"all\" for every agent)")
	command.Flags().BoolVar(&opts.inject, "inject", false, "Inject the keys into every container's environment")
	command.Flags().BoolVar(&opts.disabled, "disabled", false, "Keep the source disabled")
}

func sourceRequest(cmd *cobra.Command, opts sourceFlags) (map[string]any, error) {
	credentials := map[string]string{}
	if opts.credentialFile != "" {
		content, err := readCredentialFile(cmd, opts.credentialFile)
		if err != nil {
			return nil, err
		}
		for number, line := range strings.Split(string(content), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
			if !ok || strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("credentials file line %d is not KEY=VALUE", number+1)
			}
			credentials[strings.TrimSpace(key)] = value
		}
	}
	maps.Copy(credentials, opts.credentials)

	req := map[string]any{}
	for _, field := range []struct {
		value any
		flag  string
		key   string
	}{
		{flag: "name", key: "name", value: opts.name},
		{flag: "preset", key: "presetId", value: opts.preset},
		{flag: "input", key: "inputs", value: opts.inputs},
		{flag: "credential", key: "credentials", value: credentials},
		{flag: "credentials-file", key: "credentials", value: credentials},
		{flag: "path", key: "path", value: opts.path},
		{flag: "key-prefix", key: "keyPrefix", value: opts.keyPrefix},
		{flag: "agent", key: "allEnvironments", value: slices.Equal(opts.envIDs, []string{"all"})},
		{flag: "agent", key: "environmentIds", value: kit.Ternary(slices.Equal(opts.envIDs, []string{"all"}), nil, opts.envIDs)},
		{flag: "inject", key: "injectIntoContainers", value: opts.inject},
		{flag: "disabled", key: "enabled", value: !opts.disabled},
	} {
		if cmd.Flags().Changed(field.flag) {
			req[field.key] = field.value
		}
	}
	if opts.definitionFile != "" {
		content, err := os.ReadFile(opts.definitionFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read definition file: %w", err)
		}
		req["definition"] = string(content)
	}
	return req, nil
}

func readCredentialFile(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		content, err := output.ReadWithContext(cmd.Context(), cmd.InOrStdin(), io.ReadAll)
		if err != nil {
			return nil, fmt.Errorf("failed to read credentials from stdin: %w", err)
		}
		return content, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}
	return content, nil
}

func printSourceMutation(result types.SecretSourceMutationResponse, message string) error {
	output.Success("%s", message)
	if result.Source != nil {
		output.KeyValue("ID", result.Source.ID)
		output.KeyValue("Name", result.Source.Name)
		output.KeyValue("Kind", result.Source.Kind)
		output.KeyValue("Scope", scopeLabel(result.Source.AllEnvironments, result.Source.EnvironmentIDs))
		output.KeyValue("Keys", strconv.Itoa(result.Source.LastKeyCount))
		output.KeyValue("Last error", cmp.Or(result.Source.LastError, "-"))
	}
	return nil
}

func newSourcesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List secret sources",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.SecretSource]{
				Method:         http.MethodGet,
				Path:           clitypes.VariableSources(),
				FailureMessage: "failed to list secret sources",
				Render: func(data []types.SecretSource) error {
					headers := []string{"ID", "NAME", "KIND", "PRESET", "SCOPE", "ENABLED", "KEYS", "LAST FETCH", "ERROR"}
					rows := make([][]string, len(data))
					for i, source := range data {
						lastFetch := "-"
						if source.LastFetchedAt != nil {
							lastFetch = source.LastFetchedAt.Format(time.RFC3339)
						}
						rows[i] = []string{
							source.ID,
							source.Name,
							source.Kind,
							cmp.Or(source.PresetID, "-"),
							scopeLabel(source.AllEnvironments, source.EnvironmentIDs),
							strconv.FormatBool(source.Enabled),
							strconv.Itoa(source.LastKeyCount),
							lastFetch,
							cmp.Or(source.LastError, "-"),
						}
					}
					return output.Table(headers, rows)
				},
			})
		},
	}
}

func newSourcesPresetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "presets",
		Short:        "List built-in secret source presets",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.SecretSourcePreset]{
				Method:         http.MethodGet,
				Path:           clitypes.VariableSourcePresets(),
				FailureMessage: "failed to list secret source presets",
				Render: func(data []types.SecretSourcePreset) error {
					headers := []string{"ID", "NAME", "KIND", "INPUTS", "CREDENTIALS"}
					rows := make([][]string, len(data))
					for i, preset := range data {
						rows[i] = []string{
							preset.Definition.ID,
							preset.Definition.Name,
							preset.Definition.Kind,
							fieldList(preset.Definition.Inputs),
							fieldList(preset.Definition.Credentials),
						}
					}
					return output.Table(headers, rows)
				},
			})
		},
	}
}

func fieldList(fields []types.SecretSourceField) string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, kit.Ternary(field.Required, field.Key+"*", field.Key))
	}
	return cmp.Or(strings.Join(names, ","), "-")
}

func newSourcesCreateCmd() *cobra.Command {
	var opts sourceFlags
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create a secret source",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := sourceRequest(cmd, opts)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("agent") {
				req["allEnvironments"] = true
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.SecretSourceMutationResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.VariableSources(),
				FailureMessage: "failed to create secret source",
				Body:           req,
				Render: func(data types.SecretSourceMutationResponse) error {
					return printSourceMutation(data, fmt.Sprintf("Secret source %s created", opts.name))
				},
			})
		},
	}
	bindSourceFlags(command, &opts)
	_ = command.MarkFlagRequired("name")
	return command
}

func newSourcesUpdateCmd() *cobra.Command {
	var opts sourceFlags
	command := &cobra.Command{
		Use:          "update <id>",
		Short:        "Update a secret source",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := sourceRequest(cmd, opts)
			if err != nil {
				return err
			}
			if len(req) == 0 {
				return errors.New("no updates provided (set at least one flag)")
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.SecretSourceMutationResponse]{
				Method:         http.MethodPatch,
				Path:           clitypes.VariableSource(args[0]),
				FailureMessage: "failed to update secret source",
				Body:           req,
				Render: func(data types.SecretSourceMutationResponse) error {
					return printSourceMutation(data, "Secret source updated")
				},
			})
		},
	}
	bindSourceFlags(command, &opts)
	return command
}

func newSourcesDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a secret source",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete secret source %s?", args[0]),
				Path:           clitypes.VariableSource(args[0]),
				FailureMessage: "failed to delete secret source",
				SuccessMessage: "Secret source deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Skip the confirmation prompt")
	return command
}

func newSourcesRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "refresh <id>",
		Short:        "Fetch a secret source now and re-sync variables",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.SecretSourceMutationResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.VariableSourceRefresh(args[0]),
				FailureMessage: "failed to refresh secret source",
				Render: func(data types.SecretSourceMutationResponse) error {
					return printSourceMutation(data, "Secret source refreshed")
				},
			})
		},
	}
}

func newSourcesTestCmd() *cobra.Command {
	var opts sourceFlags
	var id string
	command := &cobra.Command{
		Use:          "test",
		Short:        "Test a saved or unsaved secret source (prints key names only)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := sourceRequest(cmd, opts)
			if err != nil {
				return err
			}
			if id != "" {
				req["id"] = id
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			result, err := c.DoEnvelope[types.SecretSourceTestResult](cmd.Context(), http.MethodPost, clitypes.VariableSourcesTest(), req)
			if err != nil {
				return fmt.Errorf("failed to test secret source: %w", err)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				if printErr := cmdutil.PrintJSON(result.Data); printErr != nil {
					return printErr
				}
			}
			if !result.Data.Success {
				return fmt.Errorf("secret source test failed: %s", result.Data.Error)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return nil
			}
			output.Success("Secret source returned %d keys (%d skipped)", result.Data.KeyCount, result.Data.SkippedKeyCount)
			for _, key := range result.Data.Keys {
				if _, writeErr := fmt.Fprintln(output.Stdout(), key); writeErr != nil {
					return writeErr
				}
			}
			return nil
		},
	}
	bindSourceFlags(command, &opts)
	command.Flags().StringVar(&id, "id", "", "Saved source to test; other flags override its stored values")
	return command
}
