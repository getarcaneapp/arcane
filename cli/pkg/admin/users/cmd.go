package users

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/user"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	startFlag  int
	forceFlag  bool
	jsonOutput bool
)

var (
	userCreateUsername    string
	userCreatePassword    string
	userCreateDisplayName string
	userCreateEmail       string
)

var (
	userUpdateUsername    string
	userUpdateDisplayName string
	userUpdateEmail       string
)

// UsersCmd is the parent command for user operations
var UsersCmd = &cobra.Command{
	Use:     "users",
	Aliases: []string{"user", "usr"},
	Short:   "Manage users",
	Long: "Manage users. Role assignments are managed separately via " +
		"`arcane admin roles assign` — the legacy `--role` flag has been " +
		"removed alongside the binary admin/user model.",
}

// summarizeRoleAssignments turns a user's role-assignment list into a short
// human label for the list table — e.g. "Admin (global)", "Editor on 2 envs",
// or "Admin (global) + 1 more". Returns "—" when the user holds none.
func summarizeRoleAssignments(assignments []user.RoleAssignmentSummary) string {
	if len(assignments) == 0 {
		return "—"
	}
	type bucket struct {
		envScoped int
		hasGlobal bool
	}
	buckets := map[string]*bucket{}
	order := []string{}
	for _, a := range assignments {
		b, ok := buckets[a.RoleID]
		if !ok {
			b = &bucket{}
			buckets[a.RoleID] = b
			order = append(order, a.RoleID)
		}
		if a.EnvironmentID == nil {
			b.hasGlobal = true
		} else {
			b.envScoped++
		}
	}
	parts := make([]string, 0, len(order))
	for _, roleID := range order {
		b := buckets[roleID]
		switch {
		case b.hasGlobal && b.envScoped == 0:
			parts = append(parts, roleID+" (global)")
		case b.hasGlobal && b.envScoped > 0:
			parts = append(parts, fmt.Sprintf("%s (global +%d envs)", roleID, b.envScoped))
		case b.envScoped == 1:
			parts = append(parts, roleID+" on 1 env")
		default:
			parts = append(parts, fmt.Sprintf("%s on %d envs", roleID, b.envScoped))
		}
	}
	return strings.Join(parts, ", ")
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
		path, err = cmdutil.ApplyPaginationParams(cmd, path, "users", "limit", limitFlag, 20, "start", startFlag)
		if err != nil {
			return fmt.Errorf("failed to build pagination query: %w", err)
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

		headers := []string{"ID", "USERNAME", "DISPLAY NAME", "EMAIL", "ROLE ASSIGNMENTS"}
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
			rows[i] = []string{
				usr.ID,
				usr.Username,
				displayName,
				email,
				summarizeRoleAssignments(usr.RoleAssignments),
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "users")
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new user",
	Long: "Create a new user. The user is created without any role " +
		"assignments — grant them via `arcane admin roles assign <userId>` " +
		"after creation.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		if userCreatePassword == "" {
			fmt.Print("Password: ")
			bytePassword, err := term.ReadPassword(os.Stdin.Fd())
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
			userCreatePassword = string(bytePassword)
			fmt.Println()
		}

		req := user.CreateUser{
			Username: userCreateUsername,
			Password: userCreatePassword,
		}
		if cmd.Flags().Changed("display-name") {
			req.DisplayName = &userCreateDisplayName
		}
		if cmd.Flags().Changed("email") {
			req.Email = &userCreateEmail
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.Users(), req)
		if err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}

		var result base.ApiResponse[user.User]
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

		output.Success("User %s created successfully", result.Data.Username)
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Username", result.Data.Username)
		output.Info("No roles assigned. Use `arcane admin roles assign %s --role <roleId>` to grant access.", result.Data.ID)
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <user-id>",
	Short:        "Get user details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.User(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[user.User]
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

		output.Header("User Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Username", result.Data.Username)
		if result.Data.DisplayName != nil {
			output.KeyValue("Display Name", *result.Data.DisplayName)
		}
		if result.Data.Email != nil {
			output.KeyValue("Email", *result.Data.Email)
		}
		output.KeyValue("Created", result.Data.CreatedAt)
		output.KeyValue("Role Assignments", summarizeRoleAssignments(result.Data.RoleAssignments))
		if len(result.Data.RoleAssignments) > 0 {
			rows := make([][]string, len(result.Data.RoleAssignments))
			for i, a := range result.Data.RoleAssignments {
				env := "global"
				if a.EnvironmentID != nil {
					env = *a.EnvironmentID
				}
				rows[i] = []string{a.RoleID, env, a.Source}
			}
			output.Table([]string{"ROLE", "SCOPE", "SOURCE"}, rows)
		}
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <user-id>",
	Short:        "Update user profile (role assignments managed separately)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		var req user.UpdateUser
		if cmd.Flags().Changed("username") {
			req.Username = &userUpdateUsername
		}
		if cmd.Flags().Changed("display-name") {
			req.DisplayName = &userUpdateDisplayName
		}
		if cmd.Flags().Changed("email") {
			req.Email = &userUpdateEmail
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.User(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update user: %w", err)
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

		output.Success("User updated successfully")
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
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete user %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.User(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}

		output.Success("User deleted successfully")
		return nil
	},
}

func init() {
	UsersCmd.AddCommand(listCmd)
	UsersCmd.AddCommand(createCmd)
	UsersCmd.AddCommand(getCmd)
	UsersCmd.AddCommand(updateCmd)
	UsersCmd.AddCommand(deleteCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of users to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, "Offset for pagination")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&userCreateUsername, "username", "", "Username")
	createCmd.Flags().StringVar(&userCreatePassword, "password", "", "Password (if omitted, will prompt securely)")
	createCmd.Flags().StringVar(&userCreateDisplayName, "display-name", "", "Display name")
	createCmd.Flags().StringVar(&userCreateEmail, "email", "", "Email address")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("username")

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVar(&userUpdateUsername, "username", "", "New username")
	updateCmd.Flags().StringVar(&userUpdateDisplayName, "display-name", "", "Display name")
	updateCmd.Flags().StringVar(&userUpdateEmail, "email", "", "Email address")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
