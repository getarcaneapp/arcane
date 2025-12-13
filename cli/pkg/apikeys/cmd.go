package apikeys

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/apikey"
	"go.getarcane.app/types/base"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

// ApiKeysCmd is the parent command for API key operations
var ApiKeysCmd = &cobra.Command{
	Use:     "api-keys",
	Aliases: []string{"apikey", "keys", "key"},
	Short:   "Manage API keys",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List API keys",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ApiKeys()
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list API keys: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[apikey.ApiKey]
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

		headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED", "LAST USED"}
		rows := make([][]string, len(result.Data))
		for i, key := range result.Data {
			description := ""
			if key.Description != nil {
				description = *key.Description
			}
			lastUsed := "Never"
			if key.LastUsedAt != nil {
				lastUsed = key.LastUsedAt.Format("2006-01-02 15:04")
			}
			rows[i] = []string{
				key.ID,
				key.Name,
				description,
				key.CreatedAt.Format("2006-01-02 15:04"),
				lastUsed,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d API keys\n", result.Pagination.TotalItems)
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create <name>",
	Short:        "Create a new API key",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		createReq := apikey.CreateApiKey{
			Name: args[0],
		}
		if description != "" {
			createReq.Description = &description
		}
		// TODO: Parse expiresAt string to time.Time if needed
		// if expiresAt != "" {
		//     parsedTime, err := time.Parse(time.RFC3339, expiresAt)
		//     if err == nil {
		//         createReq.ExpiresAt = &parsedTime
		//     }
		// }

		reqBody, err := json.Marshal(createReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ApiKeys(), reqBody)
		if err != nil {
			return fmt.Errorf("failed to create API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[apikey.ApiKeyCreatedDto]
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

		output.Success("API key created successfully")
		output.Header("API Key Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Key", result.Data.Key)
		output.KeyValue("Created", result.Data.CreatedAt.Format("2006-01-02 15:04"))
		output.Warning("Store the token securely - it will not be shown again!")
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete API key",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete API key %s? (y/N): ", args[0])
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.ApiKey(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if jsonOutput {
			var result base.ApiResponse[interface{}]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("API key deleted successfully")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <id>",
	Short:        "Get API key details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.ApiKey(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[apikey.ApiKey]
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

		output.Header("API Key Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		if result.Data.Description != nil {
			output.KeyValue("Description", *result.Data.Description)
		}
		output.KeyValue("Key Prefix", result.Data.KeyPrefix)
		output.KeyValue("Created", result.Data.CreatedAt.Format("2006-01-02 15:04"))
		if result.Data.LastUsedAt != nil {
			output.KeyValue("Last Used", result.Data.LastUsedAt.Format("2006-01-02 15:04"))
		}
		if result.Data.ExpiresAt != nil {
			output.KeyValue("Expires", result.Data.ExpiresAt.Format("2006-01-02 15:04"))
		}
		return nil
	},
}

func init() {
	ApiKeysCmd.AddCommand(listCmd)
	ApiKeysCmd.AddCommand(createCmd)
	ApiKeysCmd.AddCommand(getCmd)
	ApiKeysCmd.AddCommand(deleteCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of API keys to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringP("description", "d", "", "Description for the API key")
	createCmd.Flags().String("expires-at", "", "Expiration date (ISO 8601 format)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
