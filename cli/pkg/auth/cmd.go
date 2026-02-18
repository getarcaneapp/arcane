package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/spf13/cobra"
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
	Short:        "Login to Arcane using OIDC device authorization",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfigUnauthenticated()
		if err != nil {
			return err
		}

		reqBody, err := json.Marshal(auth.OidcDeviceAuthRequest{})
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.OIDCDeviceCode(), reqBody)
		if err != nil {
			return fmt.Errorf("device authorization failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("device authorization failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
		}

		var deviceAuth auth.OidcDeviceAuthResponse
		if err := json.Unmarshal(bodyBytes, &deviceAuth); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		output.Header("Device Login")
		output.KeyValue("Verification URL", deviceAuth.VerificationUri)
		if deviceAuth.VerificationUriComplete != "" {
			output.KeyValue("Verification URL (complete)", deviceAuth.VerificationUriComplete)
		}
		output.KeyValue("User code", deviceAuth.UserCode)
		output.Info("Complete authorization in your browser to finish login.")

		pollInterval := time.Duration(deviceAuth.Interval) * time.Second
		if pollInterval <= 0 {
			pollInterval = 5 * time.Second
		}
		expiresAt := time.Now().Add(time.Duration(deviceAuth.ExpiresIn) * time.Second)

		for {
			if time.Now().After(expiresAt) {
				return fmt.Errorf("device authorization expired; run login again")
			}

			select {
			case <-time.After(pollInterval):
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			}

			tokenReqBody, err := json.Marshal(auth.OidcDeviceTokenRequest{DeviceCode: deviceAuth.DeviceCode})
			if err != nil {
				return fmt.Errorf("failed to marshal token request: %w", err)
			}

			tokenResp, err := c.Post(cmd.Context(), types.Endpoints.OIDCDeviceToken(), tokenReqBody)
			if err != nil {
				return fmt.Errorf("device token exchange failed: %w", err)
			}

			tokenBody, err := io.ReadAll(tokenResp.Body)
			_ = tokenResp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to read token response: %w", err)
			}

			if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
				errCode := extractDeviceAuthErrorCode(string(tokenBody))
				switch errCode {
				case "authorization_pending":
					continue
				case "slow_down":
					pollInterval += 5 * time.Second
					continue
				case "expired_token":
					return fmt.Errorf("device authorization expired; run login again")
				case "access_denied":
					return fmt.Errorf("device authorization denied")
				default:
					return fmt.Errorf("device token exchange failed (status %d): %s", tokenResp.StatusCode, strings.TrimSpace(string(tokenBody)))
				}
			}

			var tokenResult auth.OidcDeviceTokenResponse
			if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
				return fmt.Errorf("failed to parse token response: %w", err)
			}
			if !tokenResult.Success || tokenResult.Token == "" {
				return fmt.Errorf("device token exchange failed: unexpected response from server")
			}

			if jsonOutput {
				resultBytes, err := json.MarshalIndent(tokenResult, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(resultBytes))
				return nil
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.JWTToken = tokenResult.Token
			cfg.RefreshToken = tokenResult.RefreshToken
			cfg.APIKey = ""
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save token: %w", err)
			}

			output.Success("Login successful")
			path, _ := config.ConfigPath()
			output.KeyValue("JWT token saved to config", path)
			return nil
		}
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
		defer func() { _ = resp.Body.Close() }()

		// Clear token from config regardless of API response
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		cfg.JWTToken = ""
		cfg.RefreshToken = ""
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to clear token: %w", err)
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

		output.Success("Logout successful")
		path, _ := config.ConfigPath()
		output.KeyValue("JWT token cleared from config", path)
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
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[any]
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
		userBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal user data: %w", err)
		}
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
			bytePassword, err := term.ReadPassword(os.Stdin.Fd())
			if err != nil {
				return fmt.Errorf("failed to read current password: %w", err)
			}
			currentPassword = string(bytePassword)
			fmt.Println()
		}

		if newPassword == "" {
			fmt.Print("New password: ")
			bytePassword, err := term.ReadPassword(os.Stdin.Fd())
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
		defer func() { _ = resp.Body.Close() }()

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
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

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if refreshToken == "" {
			refreshToken = cfg.RefreshToken
		}
		if refreshToken == "" {
			fmt.Print("Refresh token: ")
			if _, err := fmt.Scanln(&refreshToken); err != nil {
				return fmt.Errorf("failed to read refresh token: %w", err)
			}
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
		defer func() { _ = resp.Body.Close() }()

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
		cfg.JWTToken = result.Data.Token
		cfg.APIKey = ""
		if result.Data.RefreshToken != "" {
			cfg.RefreshToken = result.Data.RefreshToken
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}

		output.Success("Token refreshed successfully")
		path, _ := config.ConfigPath()
		output.KeyValue("New JWT token saved to config", path)
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

func extractDeviceAuthErrorCode(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ""
	}

	var detail struct {
		Detail string `json:"detail"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &detail); err == nil {
		if detail.Detail != "" {
			trimmed = detail.Detail
		} else if detail.Error != "" {
			trimmed = detail.Error
		}
	}

	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "authorization_pending"):
		return "authorization_pending"
	case strings.Contains(lower, "slow_down"):
		return "slow_down"
	case strings.Contains(lower, "expired_token"):
		return "expired_token"
	case strings.Contains(lower, "access_denied"):
		return "access_denied"
	default:
		return ""
	}
}
