package settings

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/category"
	"go.getarcane.app/types/search"
	"go.getarcane.app/types/settings"
)

var jsonOutput bool

// SettingsCmd is the parent command for settings operations
var SettingsCmd = &cobra.Command{
	Use:     "settings",
	Aliases: []string{"setting", "config"},
	Short:   "Manage settings",
}

var getCmd = &cobra.Command{
	Use:          "get",
	Short:        "Get environment settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.Settings(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get settings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]settings.SettingDto]
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

		headers := []string{"KEY", "TYPE", "VALUE", "PUBLIC"}
		rows := make([][]string, len(result.Data))
		for i, s := range result.Data {
			rows[i] = []string{
				s.Key,
				s.Type,
				s.Value,
				fmt.Sprintf("%t", s.IsPublic),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d settings\n", len(result.Data))
		return nil
	},
}

var categoriesCmd = &cobra.Command{
	Use:          "categories",
	Short:        "List setting categories",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.SettingsCategories())
		if err != nil {
			return fmt.Errorf("failed to get categories: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]category.Category]
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

		headers := []string{"ID", "TITLE", "DESCRIPTION"}
		rows := make([][]string, len(result.Data))
		for i, cat := range result.Data {
			rows[i] = []string{
				cat.ID,
				cat.Title,
				cat.Description,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d categories\n", len(result.Data))
		return nil
	},
}

var searchCmd = &cobra.Command{
	Use:          "search <query>",
	Short:        "Search settings",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		reqBody, err := json.Marshal(search.Request{Query: args[0]})
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.SettingsSearch(), reqBody)
		if err != nil {
			return fmt.Errorf("failed to search settings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[search.Response]
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

		output.Header("Search Results")
		fmt.Printf("Query: %s\n", result.Data.Query)
		fmt.Printf("Found: %d results\n\n", result.Data.Count)

		for _, cat := range result.Data.Results {
			output.KeyValue(cat.Title, cat.Description)
		}

		return nil
	},
}

func init() {
	SettingsCmd.AddCommand(getCmd)
	SettingsCmd.AddCommand(categoriesCmd)
	SettingsCmd.AddCommand(searchCmd)

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	categoriesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	searchCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
