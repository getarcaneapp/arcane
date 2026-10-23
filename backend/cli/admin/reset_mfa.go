package admin

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config/schema"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/session"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
)

var resetMFAUsername string

var resetMFACmd = &cobra.Command{
	Use:   "reset-mfa",
	Short: "Reset passkey MFA for a user",
	Long: "Disable passkey MFA, remove recovery codes, cancel pending MFA transactions, and revoke the user's sessions. " +
		"This command must be explicitly enabled with ARCANE_ALLOW_CLI_MFA_RESET=true.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runResetMFACommand,
}

func init() {
	resetMFACmd.Flags().StringVar(&resetMFAUsername, "username", defaultAdminUsername, "Username whose passkey MFA should be reset")
	AdminCmd.AddCommand(resetMFACmd)
}

func runResetMFACommand(cmd *cobra.Command, _ []string) error {
	if err := schema.CheckLegacyEnv(cmd.Context()); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if enabledErr := ensureMFAResetEnabledInternal(cfg); enabledErr != nil {
		return enabledErr
	}

	username := strings.TrimSpace(resetMFAUsername)
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if confirmErr := confirmMFAResetInternal(cmd.InOrStdin(), cmd.OutOrStdout(), username); confirmErr != nil {
		return confirmErr
	}

	db, err := database.Initialize(cmd.Context(), cfg.DatabaseURL, database.MigrationOptions{AllowDowngrade: cfg.AllowDowngrade})
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.WarnContext(cmd.Context(), "Failed to close database after MFA reset", "error", closeErr)
		}
	}()

	userService := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	localUser, err := userService.GetUserByUsername(cmd.Context(), username)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			return fmt.Errorf("user %q not found", username)
		}
		return fmt.Errorf("failed to find user: %w", err)
	}

	passkeyService := passkey.NewPasskeyService(db, cfg)
	if resetMFAForUserErr := passkeyService.ResetMFAForUser(cmd.Context(), localUser.ID); resetMFAForUserErr != nil {
		return fmt.Errorf("failed to reset passkey MFA: %w", resetMFAForUserErr)
	}

	if _, fprintfErr := fmt.Fprintf(cmd.OutOrStdout(), "Passkey MFA reset successfully for %q\n", username); fprintfErr != nil {
		return fmt.Errorf("failed to write MFA reset result: %w", fprintfErr)
	}
	return nil
}

func ensureMFAResetEnabledInternal(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("MFA reset config is nil")
	}
	if !cfg.AllowCLIMFAReset {
		return errors.New("CLI MFA reset is disabled; set ARCANE_ALLOW_CLI_MFA_RESET=true to enable it")
	}
	return nil
}

func confirmMFAResetInternal(in io.Reader, out io.Writer, username string) error {
	if in == nil || out == nil {
		return errors.New("confirmation input and output are required")
	}
	if _, err := fmt.Fprintf(out, "This will disable passkey MFA and revoke all sessions for %q. Type RESET to continue: ", username); err != nil {
		return fmt.Errorf("failed to write MFA reset confirmation: %w", err)
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read MFA reset confirmation: %w", err)
	}
	if strings.TrimSpace(answer) != "RESET" {
		return errors.New("MFA reset cancelled")
	}
	return nil
}
