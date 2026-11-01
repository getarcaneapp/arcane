package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/config/schema"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/validation"
)

const (
	defaultAdminUsername = "arcane"
)

var (
	resetPasswordUsername string

	// AdminCmd contains administration commands that run inside the Arcane
	// container without starting the HTTP server.
	AdminCmd = &cobra.Command{
		Use:   "admin",
		Short: "Manage Arcane administration",
	}

	resetPasswordCmd = &cobra.Command{
		Use:          "reset-password",
		Short:        "Reset the password for a global administrator",
		Long:         "Reset the password for a global administrator. This command must be explicitly enabled with ARCANE_ALLOW_CLI_PASSWORD_RESET=true.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runResetPasswordCommand,
	}
)

func init() {
	resetPasswordCmd.Flags().StringVar(&resetPasswordUsername, "username", defaultAdminUsername, "Username of the global administrator")
	AdminCmd.AddCommand(resetPasswordCmd)
}

func runResetPasswordCommand(cmd *cobra.Command, _ []string) error {
	if err := schema.CheckLegacyEnv(cmd.Context()); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if enabledErr := ensurePasswordResetEnabled(cfg); enabledErr != nil {
		return enabledErr
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

	policy := passwordPolicyFromDB(cmd.Context(), db)

	password, err := readNewPassword(cmd.OutOrStdout(), policy)
	if err != nil {
		return err
	}

	if resetPasswordErr := resetPassword(cmd.Context(), db, username, password, policy); resetPasswordErr != nil {
		return resetPasswordErr
	}

	if _, fprintfErr := fmt.Fprintf(cmd.OutOrStdout(), "Password reset successfully for global administrator %q\n", username); fprintfErr != nil {
		return fmt.Errorf("failed to write password reset result: %w", fprintfErr)
	}
	return nil
}

func ensurePasswordResetEnabled(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("password reset config is nil")
	}
	if !cfg.AllowCLIPasswordReset {
		return errors.New("CLI password reset is disabled; set ARCANE_ALLOW_CLI_PASSWORD_RESET=true to enable it")
	}
	return nil
}

func readNewPassword(out io.Writer, policy string) (string, error) {
	password, err := readPassword(out, "New password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read new password: %w", err)
	}

	confirmation, err := readPassword(out, "Confirm new password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read password confirmation: %w", err)
	}

	if validatePasswordPairErr := validatePasswordPair(password, confirmation, policy); validatePasswordPairErr != nil {
		return "", validatePasswordPairErr
	}
	return password, nil
}

func readPassword(out io.Writer, prompt string) (string, error) {
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

func validatePasswordPair(password, confirmation, policy string) error {
	if err := validation.ValidatePasswordPolicy(password, policy); err != nil {
		return err
	}
	if password != confirmation {
		return errors.New("passwords do not match")
	}
	return nil
}

func passwordPolicyFromDB(ctx context.Context, db *database.DB) string {
	var setting settings.SettingVariable
	err := db.WithContext(ctx).First(&setting, "key = ?", "authPasswordPolicy").Error
	return kit.Ternary(err != nil || strings.TrimSpace(setting.Value) == "", validation.PasswordPolicyStrong, setting.Value)
}

func resetPassword(ctx context.Context, db *database.DB, username, password, policy string) error {
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
	userService := user.NewUserService(db, roleService, session.RevokeAllUserSessionsExceptInDB)
	target, err := userService.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			return fmt.Errorf("global administrator %q not found", username)
		}
		return fmt.Errorf("failed to find user: %w", err)
	}

	permissions, err := roleService.ResolvePermissions(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("failed to resolve user permissions: %w", err)
	}
	if permissions == nil || !permissions.IsGlobalAdmin() {
		return fmt.Errorf("user %q does not have effective global administrator permissions", username)
	}

	if _, setPasswordAndRevokeSessionsExceptErr := userService.SetPasswordAndRevokeSessionsExcept(ctx, target, password, "", nil); setPasswordAndRevokeSessionsExceptErr != nil {
		return fmt.Errorf("failed to reset password: %w", setPasswordAndRevokeSessionsExceptErr)
	}

	return nil
}
