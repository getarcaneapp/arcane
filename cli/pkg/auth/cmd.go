package auth

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for authentication operations
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auth",
		Aliases: []string{"authentication"},
		Short:   "Authentication operations",
	}
	cmd.AddCommand(newFederatedCmd(), newLoginCmd(), newLogoutCmd(), newMeCmd(), newPasswordCmd(), newRefreshCmd(), newOidcStatusCmd(), newAPIKeysCmd())
	return cmd
}

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "login",
		Short:        "Login to Arcane using OIDC device authorization",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.UnauthClientFromCommand(cmd)
			if err != nil {
				return err
			}

			reqBody, err := json.Marshal(types.OidcDeviceAuthRequest{})
			if err != nil {
				return fmt.Errorf("failed to marshal request: %w", err)
			}

			resp, err := c.Post(cmd.Context(), clitypes.OIDCDeviceCode(), reqBody)
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

			var deviceAuth types.OidcDeviceAuthResponse
			if unmarshalErr := json.Unmarshal(bodyBytes, &deviceAuth); unmarshalErr != nil {
				return fmt.Errorf("failed to parse response: %w", unmarshalErr)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Device Login\nVerification URL: %s\n", deviceAuth.VerificationUri)
				if deviceAuth.VerificationUriComplete != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Verification URL (complete): %s\n", deviceAuth.VerificationUriComplete)
				}
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "User code: %s\nComplete authorization in your browser to finish login.\n", deviceAuth.UserCode)
			} else {
				output.Header("Device Login")
				output.KeyValue("Verification URL", deviceAuth.VerificationUri)
				if deviceAuth.VerificationUriComplete != "" {
					output.KeyValue("Verification URL (complete)", deviceAuth.VerificationUriComplete)
				}
				output.KeyValue("User code", deviceAuth.UserCode)
				output.Info("Complete authorization in your browser to finish login.")
			}

			tokenResult, tokenErr := pollDeviceToken(cmd, c, deviceAuth)
			if tokenErr != nil {
				return tokenErr
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(tokenResult)
			}

			cfg, loadErr := config.Load()
			if loadErr != nil {
				return fmt.Errorf("failed to load config: %w", loadErr)
			}
			cfg.JWTToken = tokenResult.Token
			cfg.RefreshToken = tokenResult.RefreshToken
			cfg.APIKey = ""
			if saveErr := config.SaveTokens(cmd.Context(), cfg); saveErr != nil {
				return fmt.Errorf("failed to save token: %w", saveErr)
			}

			output.Success("Login successful")
			path, _ := config.AuthPath()
			output.KeyValue("Session saved to", path)
			return nil
		},
	}
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "logout",
		Short:        "Logout from Arcane",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			result, err := c.PostJSON[types.MessageResponse](cmd.Context(), clitypes.AuthLogout(), nil)
			if err != nil {
				return fmt.Errorf("logout failed: %w", err)
			}

			// Clear token from config after successful API logout.
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if saveErr := config.ClearTokens(cmd.Context(), cfg); saveErr != nil {
				return fmt.Errorf("failed to clear token: %w", saveErr)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result.Data)
			}

			output.Success("Logout successful")
			path, _ := config.ConfigPath()
			output.KeyValue("JWT token cleared from config", path)
			return nil
		},
	}
}

func newMeCmd() *cobra.Command {
	var opts struct {
		effective bool
	}
	command := &cobra.Command{
		Use:          "me",
		Short:        "Get current user information",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			if opts.effective {
				return cmdutil.Run(cmd, c, cmdutil.Spec[types.EffectiveAccess]{
					Method:         http.MethodGet,
					Path:           clitypes.MeEffectiveAccess(),
					FailureMessage: "failed to get effective access",
					Render:         cmdutil.RenderEffectiveAccess,
				})
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.User]{
				Method:         http.MethodGet,
				Path:           clitypes.AuthMe(),
				FailureMessage: "failed to get user info",
				Render: func(data types.User) error {
					output.Header("Current User")
					return cmdutil.PrintJSON(data)
				},
			})
		},
	}
	command.Flags().BoolVar(&opts.effective, "effective", false, "Show effective permissions per environment, with selectors and granting bindings")
	return command
}

func newPasswordCmd() *cobra.Command {
	var opts struct {
		currentPassword string
		newPassword     string
	}
	cmd := &cobra.Command{
		Use:          "password",
		Short:        "Change password",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			currentPassword := opts.currentPassword
			newPassword := opts.newPassword

			if currentPassword == "" {
				var err error
				if currentPassword, err = output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "Current password", "current"); err != nil {
					return fmt.Errorf("failed to read current password: %w", err)
				}
			}

			if newPassword == "" {
				var err error
				if newPassword, err = output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "New password", "new"); err != nil {
					return fmt.Errorf("failed to read new password: %w", err)
				}
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.AuthPassword(),
				Body:           types.PasswordChange{CurrentPassword: currentPassword, NewPassword: newPassword},
				FailureMessage: "password change failed",
				Render: func(types.MessageResponse) error {
					output.Success("Password changed successfully")
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.currentPassword, "current", "", "Current password")
	cmd.Flags().StringVar(&opts.newPassword, "new", "", "New password")
	return cmd
}

func newRefreshCmd() *cobra.Command {
	var opts struct {
		refreshToken string
	}
	cmd := &cobra.Command{
		Use:          "refresh",
		Short:        "Refresh authentication token",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			refreshToken := opts.refreshToken

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			refreshToken = cmp.Or(refreshToken, cfg.RefreshToken)
			if refreshToken == "" {
				if refreshToken, err = output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "Refresh token", "refresh-token"); err != nil {
					return fmt.Errorf("failed to read refresh token: %w", err)
				}
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.TokenRefreshResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.AuthRefresh(),
				Body:           types.AuthRefresh{RefreshToken: refreshToken},
				FailureMessage: "token refresh failed",
				Render: func(data types.TokenRefreshResponse) error {
					cfg.JWTToken = data.Token
					cfg.APIKey = ""
					cfg.RefreshToken = cmp.Or(data.RefreshToken, cfg.RefreshToken)
					if saveErr := config.SaveTokens(cmd.Context(), cfg); saveErr != nil {
						return fmt.Errorf("failed to save token: %w", saveErr)
					}
					output.Success("Token refreshed successfully")
					path, _ := config.AuthPath()
					output.KeyValue("Session saved to", path)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.refreshToken, "refresh-token", "", "Refresh token")
	return cmd
}

func newOidcStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "oidc",
		Aliases:      []string{"oidc-status"},
		Short:        "Show OIDC configuration status",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			result, err := c.DoJSON[types.OidcStatusInfo](cmd.Context(), http.MethodGet, clitypes.OIDCStatus(), nil)
			if err != nil {
				return fmt.Errorf("failed to get OIDC status: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result)
			}

			output.Header("OIDC Status")
			output.KeyValue("Merge Accounts", strconv.FormatBool(result.MergeAccounts))
			return nil
		},
	}
}

func pollDeviceToken(cmd *cobra.Command, c *client.Client, deviceAuth types.OidcDeviceAuthResponse) (types.AuthenticationResponse, error) {
	pollInterval := time.Duration(deviceAuth.Interval) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	expiresAt := time.Now().Add(time.Duration(deviceAuth.ExpiresIn) * time.Second)

	tokenReqBody, err := json.Marshal(types.OidcDeviceTokenRequest{DeviceCode: deviceAuth.DeviceCode})
	if err != nil {
		return types.AuthenticationResponse{}, fmt.Errorf("failed to marshal token request: %w", err)
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if time.Now().After(expiresAt) {
			return types.AuthenticationResponse{}, errors.New("device authorization expired; run login again")
		}

		select {
		case <-ticker.C:
		case <-cmd.Context().Done():
			return types.AuthenticationResponse{}, cmd.Context().Err()
		}

		tokenResp, postErr := c.Post(cmd.Context(), clitypes.OIDCDeviceToken(), tokenReqBody)
		if postErr != nil {
			return types.AuthenticationResponse{}, fmt.Errorf("device token exchange failed: %w", postErr)
		}

		tokenBody, postErr := io.ReadAll(tokenResp.Body)
		_ = tokenResp.Body.Close()
		if postErr != nil {
			return types.AuthenticationResponse{}, fmt.Errorf("failed to read token response: %w", postErr)
		}

		if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
			trimmed := strings.TrimSpace(string(tokenBody))

			var detail struct {
				Detail string `json:"detail"`
				Error  string `json:"error"`
			}
			if json.Unmarshal([]byte(trimmed), &detail) == nil {
				trimmed = cmp.Or(detail.Detail, detail.Error, trimmed)
			}

			lower := strings.ToLower(trimmed)
			switch {
			case strings.Contains(lower, "authorization_pending"):
				continue
			case strings.Contains(lower, "slow_down"):
				pollInterval += 5 * time.Second
				ticker.Reset(pollInterval)
				continue
			case strings.Contains(lower, "expired_token"):
				return types.AuthenticationResponse{}, errors.New("device authorization expired; run login again")
			case strings.Contains(lower, "access_denied"):
				return types.AuthenticationResponse{}, errors.New("device authorization denied")
			case strings.Contains(lower, "mfa_required"):
				return types.AuthenticationResponse{}, errors.New("this account has MFA enabled, which browser-based CLI login cannot complete; create a personal API key in " +
					"Arcane (Account -> API keys) and run: arcane config set api-key <key>")
			default:
				return types.AuthenticationResponse{}, fmt.Errorf("device token exchange failed (status %d): %s", tokenResp.StatusCode, strings.TrimSpace(string(tokenBody)))
			}
		}

		var tokenResult types.AuthenticationResponse
		if decodeTokenResponseErr := json.Unmarshal(tokenBody, &tokenResult); decodeTokenResponseErr != nil {
			return types.AuthenticationResponse{}, fmt.Errorf("failed to parse token response: %w", decodeTokenResponseErr)
		}
		if !tokenResult.Success || tokenResult.Token == "" {
			return types.AuthenticationResponse{}, errors.New("device token exchange failed: unexpected response from server")
		}

		return tokenResult, nil
	}
}
