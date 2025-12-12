package auth

import (
	"encoding/json"
	"fmt"
	"syscall"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/config"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/auth"
	"go.getarcane.app/types/base"
	"golang.org/x/term"
)

var jsonOutput bool

// AuthCmd is the parent command for authentication operations
var AuthCmd = &cobra.Command{
	Use:     "auth",
	Aliases: []string{"authentication"},
	Short:   "Authentication operations",
}

var loginCmd = &cobra.Command{
	Use:          "login",
	Short:        "Login to Arcane",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		if username == "" {
			fmt.Print("Username: ")
			fmt.Scanln(&username)
		}
		
		if password == "" {
			fmt.Print("Password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
			password = string(bytePassword)
			fmt.Println() // Add newline after hidden password input
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		loginReq := auth.Login{
			Username: username,
			Password: password,
		}

		reqBody, err := json.Marshal(loginReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.AuthLogin(), reqBody)
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
		defer resp.Body.Close()

		var result base.ApiResponse[auth.LoginResponse]
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

		// Save JWT token to config
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		cfg.APIKey = result.Data.Token
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}

		output.Success("Login successful")
		output.KeyValue("JWT token saved to config", "~/.config/arcane/config.yaml")
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:          "logout",
	Short:        "Logout from Arcane",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.AuthLogout(), nil)
		if err != nil {
			return fmt.Errorf("logout failed: %w", err)
		}
		defer resp.Body.Close()

		// Clear token from config regardless of API response
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		cfg.APIKey = ""
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to clear token: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[interface{}]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				resultBytes, _ := json.MarshalIndent(result.Data, "", "  ")
				fmt.Println(string(resultBytes))
			}
			return nil
		}

		output.Success("Logout successful")
		output.KeyValue("JWT token cleared from config", "")
		return nil
	},
}

var meCmd = &cobra.Command{
	Use:          "me",
	Short:        "Get current user information",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.AuthMe())
		if err != nil {
			return fmt.Errorf("failed to get user info: %w", err)
		}
		defer resp.Body.Close()

		var result base.ApiResponse[interface{}]
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

		output.Header("Current User")
		userBytes, _ := json.MarshalIndent(result.Data, "", "  ")
		fmt.Println(string(userBytes))
		return nil
	},
}

var passwordCmd = &cobra.Command{
	Use:          "password",
	Short:        "Change password",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		currentPassword, _ := cmd.Flags().GetString("current")
		newPassword, _ := cmd.Flags().GetString("new")

		if currentPassword == "" {
			fmt.Print("Current password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return fmt.Errorf("failed to read current password: %w", err)
			}
			currentPassword = string(bytePassword)
			fmt.Println()
		}
		
		if newPassword == "" {
			fmt.Print("New password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return fmt.Errorf("failed to read new password: %w", err)
			}
			newPassword = string(bytePassword)
			fmt.Println()
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		changeReq := auth.PasswordChange{
			CurrentPassword: currentPassword,
			NewPassword:     newPassword,
		}

		reqBody, err := json.Marshal(changeReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.AuthPassword(), reqBody)
		if err != nil {
			return fmt.Errorf("password change failed: %w", err)
		}
		defer resp.Body.Close()

		if jsonOutput {
			var result base.ApiResponse[interface{}]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				resultBytes, _ := json.MarshalIndent(result.Data, "", "  ")
				fmt.Println(string(resultBytes))
			}
			return nil
		}

		output.Success("Password changed successfully")
		return nil
	},
}

var refreshCmd = &cobra.Command{
	Use:          "refresh",
	Short:        "Refresh authentication token",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		refreshToken, _ := cmd.Flags().GetString("refresh-token")

		if refreshToken == "" {
			fmt.Print("Refresh token: ")
			fmt.Scanln(&refreshToken)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		refreshReq := auth.Refresh{
			RefreshToken: refreshToken,
		}

		reqBody, err := json.Marshal(refreshReq)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.AuthRefresh(), reqBody)
		if err != nil {
			return fmt.Errorf("token refresh failed: %w", err)
		}
		defer resp.Body.Close()

		var result base.ApiResponse[auth.TokenRefreshResponse]
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

		// Save new JWT token to config
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		cfg.APIKey = result.Data.Token
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}

		output.Success("Token refreshed successfully")
		output.KeyValue("New JWT token saved to config", "~/.config/arcane/config.yaml")
		return nil
	},
}

func init() {
	AuthCmd.AddCommand(loginCmd)
	AuthCmd.AddCommand(logoutCmd)
	AuthCmd.AddCommand(meCmd)
	AuthCmd.AddCommand(passwordCmd)
	AuthCmd.AddCommand(refreshCmd)

	// Login command flags
	loginCmd.Flags().StringP("username", "u", "", "Username")
	loginCmd.Flags().StringP("password", "p", "", "Password")
	loginCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Password command flags
	passwordCmd.Flags().String("current", "", "Current password")
	passwordCmd.Flags().String("new", "", "New password")
	passwordCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Refresh command flags
	refreshCmd.Flags().String("refresh-token", "", "Refresh token")
	refreshCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Global JSON output flags
	logoutCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	meCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}