package registries

import (
	"encoding/json"
	"fmt"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool

	registryUpdateURL      string
	registryUpdateUsername string
	registryUpdatePassword string
	registryUpdateEnabled  bool
	registryUpdateDisabled bool
)

var RegistriesCmd = &cobra.Command{
	Use:     "registries",
	Aliases: []string{"registry", "reg"},
	Short:   "Manage container registries",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List container registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerRegistries()
		effectiveLimit := cmdutil.EffectiveLimit(cmd, "registries", "limit", limitFlag, 20)
		if effectiveLimit > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, effectiveLimit)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list registries: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[containerregistry.ContainerRegistry]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "URL", "USERNAME", "ENABLED", "INSECURE"}
		rows := make([][]string, len(result.Data))
		for i, reg := range result.Data {
			enabled := "false"
			if reg.Enabled {
				enabled = "true"
			}
			insecure := "false"
			if reg.Insecure {
				insecure = "true"
			}
			rows[i] = []string{
				reg.ID,
				reg.URL,
				reg.Username,
				enabled,
				insecure,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d registries\n", result.Pagination.TotalItems)
		return nil
	},
}

var syncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "Sync container registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ContainerRegistrySync(), nil)
		if err != nil {
			return fmt.Errorf("failed to sync registries: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to sync registries: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Registries synced successfully")
		return nil
	},
}

var testCmd = &cobra.Command{
	Use:          "test <registry-id>",
	Short:        "Test container registry connection",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ContainerRegistryTest(args[0]), nil)
		if err != nil {
			return fmt.Errorf("failed to test registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to test registry: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Registry connection test successful")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <registry-id>",
	Short:        "Get container registry details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.ContainerRegistry(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[containerregistry.ContainerRegistry]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Registry Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("URL", result.Data.URL)
		output.KeyValue("Username", result.Data.Username)
		output.KeyValue("Enabled", result.Data.Enabled)
		output.KeyValue("Insecure", result.Data.Insecure)
		output.KeyValue("Created", result.Data.CreatedAt.Format("2006-01-02 15:04"))
		output.KeyValue("Updated", result.Data.UpdatedAt.Format("2006-01-02 15:04"))
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <registry-id>",
	Short:        "Update container registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		req := make(map[string]any)
		if cmd.Flags().Changed("enabled") && cmd.Flags().Changed("disabled") {
			return fmt.Errorf("--enabled and --disabled are mutually exclusive")
		}
		if cmd.Flags().Changed("url") {
			req["url"] = registryUpdateURL
		}
		if cmd.Flags().Changed("username") {
			req["username"] = registryUpdateUsername
		}
		if cmd.Flags().Changed("password") {
			req["password"] = registryUpdatePassword
		}
		if cmd.Flags().Changed("enabled") {
			req["enabled"] = true
		}
		if cmd.Flags().Changed("disabled") {
			req["enabled"] = false
		}

		if len(req) == 0 {
			return fmt.Errorf("no updates provided; use --url, --username, --password, --enabled, or --disabled")
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.ContainerRegistry(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update registry: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Registry updated successfully")
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <registry-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete container registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete registry %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.Endpoints.ContainerRegistry(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete registry: %w", err)
		}

		output.Success("Registry deleted successfully")
		return nil
	},
}

func init() {
	RegistriesCmd.AddCommand(listCmd)
	RegistriesCmd.AddCommand(getCmd)
	RegistriesCmd.AddCommand(syncCmd)
	RegistriesCmd.AddCommand(testCmd)
	RegistriesCmd.AddCommand(updateCmd)
	RegistriesCmd.AddCommand(deleteCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of registries to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	testCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVar(&registryUpdateURL, "url", "", "Registry URL")
	updateCmd.Flags().StringVar(&registryUpdateUsername, "username", "", "Username")
	updateCmd.Flags().StringVar(&registryUpdatePassword, "password", "", "Password")
	updateCmd.Flags().BoolVar(&registryUpdateEnabled, "enabled", false, "Enable registry")
	updateCmd.Flags().BoolVar(&registryUpdateDisabled, "disabled", false, "Disable registry")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
