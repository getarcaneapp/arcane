package variables

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"
	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
	forceFlag  bool
)

var (
	createKey     string
	createValue   string
	createSecret  bool
	createAllEnvs bool
	createEnvIDs  []string
)

var (
	updateKey     string
	updateValue   string
	updateSecret  bool
	updateAllEnvs bool
	updateEnvIDs  []string
)

// VariablesCmd is the parent command for global variable operations.
var VariablesCmd = &cobra.Command{
	Use:     "variables",
	Aliases: []string{"variable", "vars"},
	Short:   "Manage global variables",
}

const secretMask = "********"

func variableScope(allEnvironments bool, environmentIDs []string) string {
	if allEnvironments {
		return "all environments"
	}
	if len(environmentIDs) == 0 {
		return "-"
	}
	return strings.Join(environmentIDs, ",")
}

func printSyncStatuses(statuses []env.EnvironmentSyncStatus) {
	headers := []string{"ENVIRONMENT ID", "NAME", "STATUS", "LAST SYNCED", "ERROR"}
	rows := make([][]string, len(statuses))
	for i, status := range statuses {
		lastSynced := "-"
		if status.LastSyncedAt != nil {
			lastSynced = status.LastSyncedAt.Format(time.RFC3339)
		}
		syncError := status.Error
		if syncError == "" {
			syncError = "-"
		}
		rows[i] = []string{
			status.EnvironmentID,
			status.EnvironmentName,
			status.Status,
			lastSynced,
			syncError,
		}
	}
	output.Table(headers, rows)
}

func printMutationResult(result env.GlobalVariableMutationResponse, message string) error {
	if jsonOutput {
		return cmdutil.PrintJSON(result)
	}

	output.Success("%s", message)
	if result.Variable != nil {
		output.KeyValue("ID", result.Variable.ID)
		output.KeyValue("Key", result.Variable.Key)
		output.KeyValue("Secret", strconv.FormatBool(result.Variable.IsSecret))
		output.KeyValue("Scope", variableScope(result.Variable.AllEnvironments, result.Variable.EnvironmentIDs))
	}
	return nil
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List global variables",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Variables())
		if err != nil {
			return errors.WrapIf(err, "failed to list variables")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to list variables")
		}

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list variables")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.ApiResponse[[]env.GlobalVariable]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"ID", "KEY", "SCOPE", "SECRET", "VALUE"}
		rows := make([][]string, len(result.Data))
		for i, variable := range result.Data {
			value := variable.Value
			if variable.IsSecret {
				value = secretMask
			}
			rows[i] = []string{
				variable.ID,
				variable.Key,
				variableScope(variable.AllEnvironments, variable.EnvironmentIDs),
				strconv.FormatBool(variable.IsSecret),
				value,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d variables\n", len(result.Data))
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a global variable",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		req := env.CreateGlobalVariableRequest{
			Key:             createKey,
			Value:           createValue,
			IsSecret:        createSecret,
			AllEnvironments: createAllEnvs,
			EnvironmentIDs:  createEnvIDs,
		}

		resp, err := c.Post(cmd.Context(), types.Variables(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create variable")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to create variable")
		}

		var result base.ApiResponse[env.GlobalVariableMutationResponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		return printMutationResult(result.Data, fmt.Sprintf("Variable %s created successfully", createKey))
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <id>",
	Short:        "Update a global variable",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		var req env.UpdateGlobalVariableRequest
		if cmd.Flags().Changed("key") {
			req.Key = &updateKey
		}
		if cmd.Flags().Changed("value") {
			req.Value = &updateValue
		}
		if cmd.Flags().Changed("secret") {
			req.IsSecret = &updateSecret
		}
		if cmd.Flags().Changed("all-environments") {
			req.AllEnvironments = &updateAllEnvs
		}
		if cmd.Flags().Changed("environment") {
			req.EnvironmentIDs = &updateEnvIDs
		}

		if req.Key == nil && req.Value == nil && req.IsSecret == nil && req.AllEnvironments == nil && req.EnvironmentIDs == nil {
			return errors.New("no updates provided (set at least one flag)")
		}

		resp, err := c.Put(cmd.Context(), types.Variable(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update variable")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update variable")
		}

		var result base.ApiResponse[env.GlobalVariableMutationResponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		return printMutationResult(result.Data, "Variable updated successfully")
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a global variable",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete variable %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.Variable(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete variable")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete variable")
		}

		var result base.ApiResponse[env.GlobalVariableMutationResponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		return printMutationResult(result.Data, "Variable deleted successfully")
	},
}

var syncStatusFlag bool

var syncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "Sync global variables to every environment (--status shows the last sync result per environment)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		var response *http.Response
		failureMessage := "failed to sync variables"
		if syncStatusFlag {
			failureMessage = "failed to get variable sync status"
			response, err = c.Get(cmd.Context(), types.VariablesSyncStatus())
		} else {
			response, err = c.Post(cmd.Context(), types.VariablesSync(), nil)
		}
		if err != nil {
			return errors.WrapIff(err, "%s", failureMessage)
		}
		defer func() { _ = response.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(response); err != nil {
			return errors.WrapIff(err, "%s", failureMessage)
		}

		var result base.ApiResponse[[]env.EnvironmentSyncStatus]
		if err := cmdutil.DecodeJSON(response, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		printSyncStatuses(result.Data)
		return nil
	},
}

func init() {
	VariablesCmd.AddCommand(listCmd)
	VariablesCmd.AddCommand(createCmd)
	VariablesCmd.AddCommand(updateCmd)
	VariablesCmd.AddCommand(deleteCmd)
	VariablesCmd.AddCommand(syncCmd)

	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&createKey, "key", "", "Variable key")
	createCmd.Flags().StringVar(&createValue, "value", "", "Variable value")
	createCmd.Flags().BoolVar(&createSecret, "secret", false, "Store the value as a secret")
	createCmd.Flags().BoolVar(&createAllEnvs, "all-environments", false, "Scope the variable to all environments")
	createCmd.Flags().StringArrayVar(&createEnvIDs, "environment", nil, "Environment ID to scope the variable to (repeatable)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("key")

	updateCmd.Flags().StringVar(&updateKey, "key", "", "Variable key")
	updateCmd.Flags().StringVar(&updateValue, "value", "", "Variable value")
	updateCmd.Flags().BoolVar(&updateSecret, "secret", false, "Store the value as a secret")
	updateCmd.Flags().BoolVar(&updateAllEnvs, "all-environments", false, "Scope the variable to all environments")
	updateCmd.Flags().StringArrayVar(&updateEnvIDs, "environment", nil, "Environment ID to scope the variable to (repeatable)")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip the confirmation prompt")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	syncCmd.Flags().BoolVar(&syncStatusFlag, "status", false, "Show the last sync result per environment instead of triggering a sync")
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
