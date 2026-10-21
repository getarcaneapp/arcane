package variables

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for global variable operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "variables",
		Aliases: []string{"variable", "vars"},
		Short:   "Manage global variables",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newSyncCmd())
	return command
}

const secretMask = "********"

func printMutationResult(result types.GlobalVariableMutationResponse, message string) error {
	output.Success("%s", message)
	if result.Variable != nil {
		output.KeyValue("ID", result.Variable.ID)
		output.KeyValue("Key", result.Variable.Key)
		output.KeyValue("Secret", strconv.FormatBool(result.Variable.IsSecret))
		scope := "-"
		switch {
		case result.Variable.AllEnvironments:
			scope = "all environments"
		case len(result.Variable.EnvironmentIDs) > 0:
			scope = strings.Join(result.Variable.EnvironmentIDs, ",")
		}
		output.KeyValue("Scope", scope)
	}
	return nil
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List global variables",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.GlobalVariable]{
				Method:         http.MethodGet,
				Path:           clitypes.Variables(),
				FailureMessage: "failed to list variables",
				Render: func(data []types.GlobalVariable) error {
					headers := []string{"ID", "KEY", "SCOPE", "SECRET", "VALUE"}
					rows := make([][]string, len(data))
					for i, variable := range data {
						value := variable.Value
						if variable.IsSecret {
							value = secretMask
						}
						scope := "-"
						switch {
						case variable.AllEnvironments:
							scope = "all environments"
						case len(variable.EnvironmentIDs) > 0:
							scope = strings.Join(variable.EnvironmentIDs, ",")
						}
						rows[i] = []string{
							variable.ID,
							variable.Key,
							scope,
							strconv.FormatBool(variable.IsSecret),
							value,
						}
					}

					if tableErr := output.Table(headers, rows); tableErr != nil {
						return tableErr
					}
					_, _ = fmt.Fprintf(output.Stdout(), "\nTotal: %d variables\n", len(data))
					return nil
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		key     string
		value   string
		secret  bool
		allEnvs bool
		envIDs  []string
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create a global variable",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			req := types.GlobalVariable{
				Key:             opts.key,
				Value:           opts.value,
				IsSecret:        opts.secret,
				AllEnvironments: opts.allEnvs,
				EnvironmentIDs:  opts.envIDs,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GlobalVariableMutationResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.Variables(),
				FailureMessage: "failed to create variable",
				Body:           req,
				Render: func(data types.GlobalVariableMutationResponse) error {
					return printMutationResult(data, fmt.Sprintf("Variable %s created successfully", opts.key))
				},
			})
		},
	}
	command.Flags().StringVar(&opts.key, "key", "", "Variable key")
	command.Flags().StringVar(&opts.value, "value", "", "Variable value")
	command.Flags().BoolVar(&opts.secret, "secret", false, "Store the value as a secret")
	command.Flags().BoolVar(&opts.allEnvs, "all-environments", false, "Scope the variable to all environments")
	command.Flags().StringArrayVar(&opts.envIDs, "environment", nil, "Environment ID to scope the variable to (repeatable)")
	_ = command.MarkFlagRequired("key")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		key     string
		value   string
		secret  bool
		allEnvs bool
		envIDs  []string
	}
	command := &cobra.Command{
		Use:          "update <id>",
		Short:        "Update a global variable",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := map[string]any{}
			for _, field := range []struct {
				flag  string
				key   string
				value any
			}{
				{flag: "key", key: "key", value: opts.key},
				{flag: "value", key: "value", value: opts.value},
				{flag: "secret", key: "isSecret", value: opts.secret},
				{flag: "all-environments", key: "allEnvironments", value: opts.allEnvs},
				{flag: "environment", key: "environmentIds", value: opts.envIDs},
			} {
				if cmd.Flags().Changed(field.flag) {
					req[field.key] = field.value
				}
			}
			if len(req) == 0 {
				return errors.New("no updates provided (set at least one flag)")
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GlobalVariableMutationResponse]{
				Method:         http.MethodPut,
				Path:           clitypes.Variable(args[0]),
				FailureMessage: "failed to update variable",
				Body:           req,
				Render: func(data types.GlobalVariableMutationResponse) error {
					return printMutationResult(data, "Variable updated successfully")
				},
			})
		},
	}
	command.Flags().StringVar(&opts.key, "key", "", "Variable key")
	command.Flags().StringVar(&opts.value, "value", "", "Variable value")
	command.Flags().BoolVar(&opts.secret, "secret", false, "Store the value as a secret")
	command.Flags().BoolVar(&opts.allEnvs, "all-environments", false, "Scope the variable to all environments")
	command.Flags().StringArrayVar(&opts.envIDs, "environment", nil, "Environment ID to scope the variable to (repeatable)")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a global variable",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete variable %s?", args[0]),
				Path:           clitypes.Variable(args[0]),
				FailureMessage: "failed to delete variable",
				SuccessMessage: "Variable deleted successfully",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Skip the confirmation prompt")
	return command
}

func newSyncCmd() *cobra.Command {
	var opts struct {
		syncStatus bool
	}
	command := &cobra.Command{
		Use:          "sync",
		Short:        "Sync global variables to every environment (--status shows the last sync result per environment)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			method, path, failureMessage := http.MethodPost, clitypes.VariablesSync(), "failed to sync variables"
			if opts.syncStatus {
				method, path, failureMessage = http.MethodGet, clitypes.VariablesSyncStatus(), "failed to get variable sync status"
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.EnvironmentSyncStatus]{
				Method:         method,
				Path:           path,
				FailureMessage: failureMessage,
				Render: func(data []types.EnvironmentSyncStatus) error {
					headers := []string{"ENVIRONMENT ID", "NAME", "STATUS", "LAST SYNCED", "ERROR"}
					rows := make([][]string, len(data))
					for i, status := range data {
						lastSynced := "-"
						if status.LastSyncedAt != nil {
							lastSynced = status.LastSyncedAt.Format(time.RFC3339)
						}
						syncError := cmp.Or(status.Error, "-")
						rows[i] = []string{
							status.EnvironmentID,
							status.EnvironmentName,
							status.Status,
							lastSynced,
							syncError,
						}
					}
					return output.Table(headers, rows)
				},
			})
		},
	}
	command.Flags().BoolVar(&opts.syncStatus, "status", false, "Show the last sync result per environment instead of triggering a sync")
	return command
}
