package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"
	"golang.org/x/term"
)

const defaultAdminUsername = "arcane"

var resetPasswordUsername string

// AdminCmd contains administration commands that run inside the Arcane
// container without starting the HTTP server.
var AdminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Manage Arcane administration",
}

var resetPasswordCmd = &cobra.Command{
	Use:          "reset-password",
	Short:        "Reset the password for a global administrator",
	Long:         "Reset the password for a global administrator. This command must be explicitly enabled with ALLOW_CLI_PASSWORD_RESET=true.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runResetPasswordCommandInternal,
}

func init() {
	resetPasswordCmd.Flags().StringVar(&resetPasswordUsername, "username", defaultAdminUsername, "Username of the global administrator")
	AdminCmd.AddCommand(resetPasswordCmd)
}

func runResetPasswordCommandInternal(cmd *cobra.Command, _ []string) error {
	cfg := config.Load()
	if err := ensurePasswordResetEnabledInternal(cfg); err != nil {
		return err
	}

	username := strings.TrimSpace(resetPasswordUsername)
	if username == "" {
		return errors.New("username cannot be empty")
	}

	db, err := database.Initialize(cmd.Context(), cfg.DatabaseURL, database.MigrationOptions{
		AllowDowngrade: cfg.AllowDowngrade,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.WarnContext(cmd.Context(), "Failed to close database after password reset", "error", closeErr)
		}
	}()

	policy := passwordPolicyFromDBInternal(cmd.Context(), db)

	password, err := readNewPasswordInternal(cmd.OutOrStdout(), policy)
	if err != nil {
		return err
	}

	if err := resetPasswordInternal(cmd.Context(), db, username, password, policy); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Password reset successfully for global administrator %q\n", username); err != nil {
		return fmt.Errorf("failed to write password reset result: %w", err)
	}
	return nil
}

func ensurePasswordResetEnabledInternal(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("password reset config is nil")
	}
	if !cfg.AllowCLIPasswordReset {
		return errors.New("CLI password reset is disabled; set ALLOW_CLI_PASSWORD_RESET=true to enable it")
	}
	return nil
}

func readNewPasswordInternal(out io.Writer, policy string) (string, error) {
	password, err := readPasswordInternal(out, "New password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read new password: %w", err)
	}

	confirmation, err := readPasswordInternal(out, "Confirm new password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read password confirmation: %w", err)
	}

	if err := validatePasswordPairInternal(password, confirmation, policy); err != nil {
		return "", err
	}
	return password, nil
}

func readPasswordInternal(out io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	if _, printErr := fmt.Fprintln(out); err == nil {
		err = printErr
	}
	if err != nil {
		return "", err
	}
	return string(password), nil
}

func validatePasswordPairInternal(password, confirmation, policy string) error {
	if err := validation.ValidatePasswordPolicy(password, policy); err != nil {
		return err
	}
	if password != confirmation {
		return errors.New("passwords do not match")
	}
	return nil
}

func passwordPolicyFromDBInternal(ctx context.Context, db *database.DB) string {
	var setting settings.SettingVariable
	err := db.WithContext(ctx).First(&setting, "key = ?", "authPasswordPolicy").Error
	return kit.Ternary(err != nil || strings.TrimSpace(setting.Value) == "", validation.PasswordPolicyStrong, setting.Value)
}

func resetPasswordInternal(ctx context.Context, db *database.DB, username, password, policy string) error {
	if db == nil {
		return errors.New("database is nil")
	}

	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if err := validation.ValidatePasswordPolicy(password, policy); err != nil {
		return err
	}

	roleService := role.NewRoleService(db)
	userService := user.NewUserService(db, roleService)
	target, err := userService.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			return fmt.Errorf("global administrator %q not found", username)
		}
		return fmt.Errorf("failed to find user: %w", err)
	}

	permissions, err := roleService.ResolvePermissions(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to resolve user permissions: %w", err)
	}
	if permissions == nil || !permissions.IsGlobalAdmin() {
		return fmt.Errorf("user %q does not have effective global administrator permissions", username)
	}

	if _, err := userService.SetPasswordAndRevokeSessionsExcept(ctx, target, password, ""); err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	return nil
}
