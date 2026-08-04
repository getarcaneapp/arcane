package admin

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"emperror.dev/errors"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
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
		return errors.WrapIf(err, "failed to initialize database")
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
		return errors.WrapIf(err, "failed to write password reset result")
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
		return "", errors.WrapIf(err, "failed to read new password")
	}

	confirmation, err := readPasswordInternal(out, "Confirm new password: ")
	if err != nil {
		return "", errors.WrapIf(err, "failed to read password confirmation")
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
	var setting models.SettingVariable
	err := db.WithContext(ctx).First(&setting, "key = ?", "authPasswordPolicy").Error
	if err != nil || strings.TrimSpace(setting.Value) == "" {
		return validation.PasswordPolicyStrong
	}
	return setting.Value
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

	roleService := services.NewRoleService(db)
	userService := services.NewUserService(db).WithRoleService(roleService)
	target, err := userService.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			return errors.Errorf("global administrator %q not found", username)
		}
		return errors.WrapIf(err, "failed to find user")
	}

	permissions, err := roleService.ResolvePermissions(ctx, target)
	if err != nil {
		return errors.WrapIf(err, "failed to resolve user permissions")
	}
	if permissions == nil || !permissions.IsGlobalAdmin() {
		return errors.Errorf("user %q does not have effective global administrator permissions", username)
	}

	if _, err := userService.SetPasswordAndRevokeSessionsExcept(ctx, target, password, ""); err != nil {
		return errors.WrapIf(err, "failed to reset password")
	}

	return nil
}
