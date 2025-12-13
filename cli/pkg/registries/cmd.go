package registries

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/containerregistry"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
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
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
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

		if jsonOutput {
			var result base.ApiResponse[interface{}]
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

		if jsonOutput {
			var result base.ApiResponse[interface{}]
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

var deleteCmd = &cobra.Command{
	Use:          "delete <registry-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete container registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete registry %s? (y/N): ", args[0])
			var response string
			if _, err := fmt.Scanln(&response); err != nil {
				fmt.Println("Cancelled")
				return nil
			}
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
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

		output.Success("Registry deleted successfully")
		return nil
	},
}

func init() {
	RegistriesCmd.AddCommand(listCmd)
	RegistriesCmd.AddCommand(syncCmd)
	RegistriesCmd.AddCommand(testCmd)
	RegistriesCmd.AddCommand(deleteCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of registries to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	testCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
