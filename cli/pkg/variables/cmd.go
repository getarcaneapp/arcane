package variables

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/env"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
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

func printMutationResult(result env.GlobalVariableMutationResponse, message string) error {
	output.Success("%s", message)
	if result.Variable != nil {
		output.KeyValue("ID", result.Variable.ID)
		output.KeyValue("Key", result.Variable.Key)
		output.KeyValue("Secret", strconv.FormatBool(result.Variable.IsSecret))
		output.KeyValue("Inject", strconv.FormatBool(result.Variable.InjectIntoContainers))
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]env.GlobalVariable]{
				Method:         http.MethodGet,
				Path:           types.Variables(),
				FailureMessage: "failed to list variables",
				Render: func(data []env.GlobalVariable) error {
					headers := []string{"ID", "KEY", "SCOPE", "SECRET", "INJECT", "VALUE"}
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
							strconv.FormatBool(variable.InjectIntoContainers),
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
		inject  bool
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

			req := env.CreateGlobalVariableRequest{
				Key:                  opts.key,
				Value:                opts.value,
				IsSecret:             opts.secret,
				AllEnvironments:      opts.allEnvs,
				InjectIntoContainers: opts.inject,
				EnvironmentIDs:       opts.envIDs,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[env.GlobalVariableMutationResponse]{
				Method:         http.MethodPost,
				Path:           types.Variables(),
				FailureMessage: "failed to create variable",
				Body:           req,
				Render: func(data env.GlobalVariableMutationResponse) error {
					return printMutationResult(data, fmt.Sprintf("Variable %s created successfully", opts.key))
				},
			})
		},
	}
	command.Flags().StringVar(&opts.key, "key", "", "Variable key")
	command.Flags().StringVar(&opts.value, "value", "", "Variable value")
	command.Flags().BoolVar(&opts.secret, "secret", false, "Store the value as a secret")
	command.Flags().BoolVar(&opts.inject, "inject", false, "Inject the variable into every container's environment (otherwise it is only available for ${VAR} interpolation)")
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
		inject  bool
		allEnvs bool
		envIDs  []string
	}
	command := &cobra.Command{
		Use:          "update <id>",
		Short:        "Update a global variable",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.ContainsFunc([]string{"key", "value", "secret", "inject", "all-environments", "environment"}, cmd.Flags().Changed) {
				return errors.New("no updates provided (set at least one flag)")
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			req := env.UpdateGlobalVariableRequest{
				Key:                  kit.Ternary(cmd.Flags().Changed("key"), &opts.key, nil),
				Value:                kit.Ternary(cmd.Flags().Changed("value"), &opts.value, nil),
				IsSecret:             kit.Ternary(cmd.Flags().Changed("secret"), &opts.secret, nil),
				AllEnvironments:      kit.Ternary(cmd.Flags().Changed("all-environments"), &opts.allEnvs, nil),
				InjectIntoContainers: kit.Ternary(cmd.Flags().Changed("inject"), &opts.inject, nil),
				EnvironmentIDs:       kit.Ternary(cmd.Flags().Changed("environment"), &opts.envIDs, nil),
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[env.GlobalVariableMutationResponse]{
				Method:         http.MethodPut,
				Path:           types.Variable(args[0]),
				FailureMessage: "failed to update variable",
				Body:           req,
				Render: func(data env.GlobalVariableMutationResponse) error {
					return printMutationResult(data, "Variable updated successfully")
				},
			})
		},
	}
	command.Flags().StringVar(&opts.key, "key", "", "Variable key")
	command.Flags().StringVar(&opts.value, "value", "", "Variable value")
	command.Flags().BoolVar(&opts.secret, "secret", false, "Store the value as a secret")
	command.Flags().BoolVar(&opts.inject, "inject", false, "Inject the variable into every container's environment (otherwise it is only available for ${VAR} interpolation)")
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
				Path:           types.Variable(args[0]),
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

			method, path, failureMessage := http.MethodPost, types.VariablesSync(), "failed to sync variables"
			if opts.syncStatus {
				method, path, failureMessage = http.MethodGet, types.VariablesSyncStatus(), "failed to get variable sync status"
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]env.EnvironmentSyncStatus]{
				Method:         method,
				Path:           path,
				FailureMessage: failureMessage,
				Render: func(data []env.EnvironmentSyncStatus) error {
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
