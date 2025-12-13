package users

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/user"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

// UsersCmd is the parent command for user operations
var UsersCmd = &cobra.Command{
	Use:     "users",
	Aliases: []string{"user", "usr"},
	Short:   "Manage users",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List users",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Users()
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list users: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[user.User]
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

		headers := []string{"ID", "USERNAME", "DISPLAY NAME", "EMAIL", "ROLES"}
		rows := make([][]string, len(result.Data))
		for i, usr := range result.Data {
			displayName := ""
			if usr.DisplayName != nil {
				displayName = *usr.DisplayName
			}
			email := ""
			if usr.Email != nil {
				email = *usr.Email
			}
			roles := strings.Join(usr.Roles, ", ")
			rows[i] = []string{
				usr.ID,
				usr.Username,
				displayName,
				email,
				roles,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d users\n", result.Pagination.TotalItems)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <user-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete user",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete user %s? (y/N): ", args[0])
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.User(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("User deleted successfully")
		return nil
	},
}

func init() {
	UsersCmd.AddCommand(listCmd)
	UsersCmd.AddCommand(deleteCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of users to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
