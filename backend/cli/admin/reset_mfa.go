package admin

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"emperror.dev/errors"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
)

var resetMFAUsername string

var resetMFACmd = &cobra.Command{
	Use:          "reset-mfa",
	Short:        "Reset passkey MFA for a user",
	Long:         "Disable passkey MFA, remove recovery codes, cancel pending MFA transactions, and revoke the user's sessions. This command must be explicitly enabled with ALLOW_CLI_MFA_RESET=true.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runResetMFACommandInternal,
}

func init() {
	resetMFACmd.Flags().StringVar(&resetMFAUsername, "username", defaultAdminUsername, "Username whose passkey MFA should be reset")
	AdminCmd.AddCommand(resetMFACmd)
}

func runResetMFACommandInternal(cmd *cobra.Command, _ []string) error {
	cfg := config.Load()
	if err := ensureMFAResetEnabledInternal(cfg); err != nil {
		return err
	}

	username := strings.TrimSpace(resetMFAUsername)
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if err := confirmMFAResetInternal(cmd.InOrStdin(), cmd.OutOrStdout(), username); err != nil {
		return err
	}

	db, err := database.Initialize(cmd.Context(), cfg.DatabaseURL, database.MigrationOptions{AllowDowngrade: cfg.AllowDowngrade})
	if err != nil {
		return errors.WrapIf(err, "failed to initialize database")
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.WarnContext(cmd.Context(), "Failed to close database after MFA reset", "error", closeErr)
		}
	}()

	userService := user.NewUserService(db)
	user, err := userService.GetUserByUsername(cmd.Context(), username)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			return errors.Errorf("user %q not found", username)
		}
		return errors.WrapIf(err, "failed to find user")
	}

	passkeyService := passkey.NewPasskeyService(db, cfg)
	if err := passkeyService.ResetMFAForUser(cmd.Context(), user.ID); err != nil {
		return errors.WrapIf(err, "failed to reset passkey MFA")
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Passkey MFA reset successfully for %q\n", username); err != nil {
		return errors.WrapIf(err, "failed to write MFA reset result")
	}
	return nil
}

func ensureMFAResetEnabledInternal(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("MFA reset config is nil")
	}
	if !cfg.AllowCLIMFAReset {
		return errors.New("CLI MFA reset is disabled; set ALLOW_CLI_MFA_RESET=true to enable it")
	}
	return nil
}

func confirmMFAResetInternal(in io.Reader, out io.Writer, username string) error {
	if in == nil || out == nil {
		return errors.New("confirmation input and output are required")
	}
	if _, err := fmt.Fprintf(out, "This will disable passkey MFA and revoke all sessions for %q. Type RESET to continue: ", username); err != nil {
		return errors.WrapIf(err, "failed to write MFA reset confirmation")
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return errors.WrapIf(err, "failed to read MFA reset confirmation")
	}
	if strings.TrimSpace(answer) != "RESET" {
		return errors.New("MFA reset cancelled")
	}
	return nil
}
