package backups

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for Arcane system backup operations (admin-only).
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "backups",
		Aliases: []string{"backup"},
		Short:   "Manage Arcane system backups (admin only)",
		Long: `System backups snapshot Arcane's database and configuration, encrypted with the recovery key.
Backups are stored in the local repository, a saved S3 destination, or both; policies schedule automatic runs.`,
	}
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newRestoreCmd())
	cmd.AddCommand(newUploadCmd())
	cmd.AddCommand(newDiscoverCmd())
	cmd.AddCommand(newPoliciesCmd())
	cmd.AddCommand(newRecoveryKeyCmd())
	cmd.AddCommand(newS3Cmd())
	return cmd
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List Arcane system backups",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.SystemBackupRun]{
				Resource: "backups",
				Endpoint: clitypes.Backups(),
				Params:   cmdutil.ListParams{Resource: "backups", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "STATUS", "TRIGGER", "DESTINATION", "SIZE", "S3 DESTINATION", "CREATED"},
				Row: func(run types.SystemBackupRun) []string {
					return []string{
						run.ID,
						run.Status,
						run.Trigger,
						string(run.Destination),
						kit.Ternary(run.Size > 0, output.Bytes(run.Size), ""),
						run.S3DestinationName,
						run.CreatedAt.Format("2006-01-02 15:04"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of backups to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return cmd
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		destination   types.SystemBackupDestination
		s3Destination string
		local         bool
		recoveryKey   string
		policyID      string
	}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Arcane system backup",
		Long: `Create a system backup. With no flags the backup is stored locally; --s3-destination
alone stores it in S3, and --local together with --s3-destination stores it in both.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			s3DestinationID := ""
			if opts.s3Destination != "" {
				resolved, _, resolveErr := s3DestinationRef.Resolve(cmd.Context(), c, opts.s3Destination, cmdutil.AllowPrompt(cmd))
				if resolveErr != nil {
					return resolveErr
				}
				s3DestinationID = resolved.ID
			}

			destination := opts.destination
			if cmd.Flags().Changed("destination") && destination == "" {
				return fmt.Errorf("invalid --destination %q: must be local, s3, or local_s3", opts.destination)
			}
			if !cmd.Flags().Changed("destination") {
				switch {
				case s3DestinationID != "" && opts.local:
					destination = types.SystemBackupDestinationLocalS3
				case s3DestinationID != "":
					destination = types.SystemBackupDestinationS3
				default:
					destination = types.SystemBackupDestinationLocal
				}
			}
			if (destination == types.SystemBackupDestinationS3 || destination == types.SystemBackupDestinationLocalS3) && s3DestinationID == "" {
				return fmt.Errorf("--destination %s requires --s3-destination", destination)
			}

			req := types.CreateSystemBackupRequest{
				Destination:     destination,
				S3DestinationID: s3DestinationID,
				RecoveryKey:     opts.recoveryKey,
				PolicyID:        opts.policyID,
			}

			run, err := c.DoJSON[types.SystemBackupRun](cmd.Context(), http.MethodPost, clitypes.Backups(), req)
			if err != nil {
				return fmt.Errorf("failed to create backup: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(run)
			}

			output.Success("Backup %s created successfully", run.ID)
			output.KeyValue("ID", run.ID)
			output.KeyValue("Status", run.Status)
			output.KeyValue("Destination", string(run.Destination))
			if run.Size > 0 {
				output.KeyValue("Size", output.Bytes(run.Size))
			}
			if run.S3DestinationName != "" {
				output.KeyValue("S3 Destination", run.S3DestinationName)
			}
			return nil
		},
	}
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.destination, "destination", "", "",
		"Backup destination (inferred from --local/--s3-destination when omitted)",
		types.SystemBackupDestinationLocal, types.SystemBackupDestinationS3, types.SystemBackupDestinationLocalS3)
	cmd.Flags().StringVar(&opts.s3Destination, "s3-destination", "", "S3 destination name or ID")
	cmd.Flags().BoolVar(&opts.local, "local", false, "Also keep a local copy when backing up to S3")
	cmd.Flags().StringVar(&opts.recoveryKey, "recovery-key", "", "Recovery key used to encrypt the backup")
	cmd.Flags().StringVar(&opts.policyID, "policy", "", "Backup policy ID to associate with the run")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force       bool
		recoveryKey string
	}
	cmd := &cobra.Command{
		Use:          "delete <backup-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete an Arcane system backup",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete backup %s?", args[0]),
				Path:           clitypes.Backup(args[0]),
				Body:           types.DeleteSystemBackupRequest{RecoveryKey: opts.recoveryKey},
				FailureMessage: "failed to delete backup",
				SuccessMessage: fmt.Sprintf("Backup %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().StringVar(&opts.recoveryKey, "recovery-key", "", "Recovery key (required to delete remote snapshots)")
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Delete without confirmation")
	return cmd
}

func newRestoreCmd() *cobra.Command {
	var opts struct {
		force       bool
		recoveryKey string
	}
	cmd := &cobra.Command{
		Use:          "restore <backup-id>",
		Short:        "Restore Arcane from a system backup",
		Long:         `Restore overwrites the current Arcane database and configuration with the backup contents, then restarts Arcane.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.force {
				output.Warning("Restoring backup %s will OVERWRITE the current Arcane database and configuration, then restart Arcane. This cannot be undone.", args[0])
			}
			confirmed, confirmErr := cmdutil.ConfirmOrCancel(cmd, opts.force, "Are you absolutely sure you want to restore this backup?")
			if confirmErr != nil || !confirmed {
				return confirmErr
			}

			key := opts.recoveryKey
			if key == "" {
				var err error
				if key, err = output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "Recovery key", "recovery-key"); err != nil {
					return fmt.Errorf("failed to read recovery key: %w", err)
				}
				if key == "" {
					return errors.New("recovery key is required")
				}
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.BackupAction(args[0], "restore"),
				Body:           types.RestoreSystemBackupFilesRequest{RecoveryKey: key},
				FailureMessage: "failed to restore backup",
				Render: func(data types.MessageResponse) error {
					output.Success("%s", data.Message)
					output.Warning("Arcane will restart while the restore is applied; the server may be unavailable for a short time.")
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.recoveryKey, "recovery-key", "", "Recovery key the backup was encrypted with (prompted for when omitted)")
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Restore without confirmation")
	return cmd
}

func newUploadCmd() *cobra.Command {
	var opts struct {
		s3Destination string
		recoveryKey   string
	}
	cmd := &cobra.Command{
		Use:          "upload <backup-id>",
		Short:        "Upload an Arcane system backup to an S3 destination",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, opts.s3Destination, cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}

			req := types.UploadSystemBackupRequest{
				S3DestinationID: resolved.ID,
				RecoveryKey:     opts.recoveryKey,
			}

			run, err := c.DoJSON[types.SystemBackupRun](cmd.Context(), http.MethodPost, clitypes.BackupUpload(args[0]), req)
			if err != nil {
				return fmt.Errorf("failed to upload backup: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(run)
			}

			output.Success("Backup %s uploaded successfully", run.ID)
			output.KeyValue("Destination", string(run.Destination))
			if run.S3DestinationName != "" {
				output.KeyValue("S3 Destination", run.S3DestinationName)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.s3Destination, "s3-destination", "", "S3 destination name or ID to upload to")
	cmd.Flags().StringVar(&opts.recoveryKey, "recovery-key", "", "Recovery key the backup was encrypted with")
	_ = cmd.MarkFlagRequired("s3-destination")
	return cmd
}

func newDiscoverCmd() *cobra.Command {
	var opts struct {
		s3Destination string
		recoveryKey   string
	}
	cmd := &cobra.Command{
		Use:          "discover",
		Short:        "Discover Arcane system backups stored in an S3 destination",
		Long:         `Discover scans an S3 destination for backups and imports records for any not already known to this Arcane instance.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, opts.s3Destination, cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}

			req := types.DiscoverSystemBackupsRequest{
				S3DestinationID: resolved.ID,
				RecoveryKey:     opts.recoveryKey,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[int]{
				Method:         http.MethodPost,
				Path:           clitypes.BackupsDiscover(),
				Body:           req,
				FailureMessage: "failed to discover backups",
				Render: func(data int) error {
					output.Success("Discovered %d backup(s)", data)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.s3Destination, "s3-destination", "", "S3 destination name or ID to search")
	cmd.Flags().StringVar(&opts.recoveryKey, "recovery-key", "", "Recovery key the backups were encrypted with")
	_ = cmd.MarkFlagRequired("s3-destination")
	return cmd
}

func newPoliciesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "policies",
		Short:        "Show Arcane system backup policies",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			result, err := c.DoJSON[types.SystemBackupPolicyCollection](cmd.Context(), http.MethodGet, clitypes.BackupsPolicies(), nil)
			if err != nil {
				return fmt.Errorf("failed to get backup policies: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result)
			}

			return renderSystemPolicies(result)
		},
	}
	cmd.AddCommand(newPoliciesUpdateCmd())
	return cmd
}

func newPoliciesUpdateCmd() *cobra.Command {
	var opts policiesUpdateOptions
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Arcane system backup policies",
		Long: `Update a system backup policy with flags: the current policies are fetched, the targeted
policy is modified, and the whole set is saved back. A new policy is created when none exist.
Alternatively, --file replaces all policies from a raw JSON payload.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			var req types.UpdateSystemBackupPolicies
			if opts.file != "" {
				req, err = opts.requestFromFile()
			} else {
				req, err = opts.requestFromFlags(cmd, c)
			}
			if err != nil {
				return err
			}

			result, err := c.DoJSON[types.SystemBackupPolicyCollection](cmd.Context(), http.MethodPut, clitypes.BackupsPolicies(), req)
			if err != nil {
				return fmt.Errorf("failed to update backup policies: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result)
			}

			output.Success("Backup policies updated successfully")
			return renderSystemPolicies(result)
		},
	}
	cmd.Flags().StringVar(&opts.file, "file", "", "JSON file with the full policies payload (use - for stdin); replaces all policies")
	cmd.Flags().StringVar(&opts.id, "policy-id", "", "Policy ID to update (defaults to the sole policy)")
	cmd.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable the policy")
	cmd.Flags().StringVar(&opts.schedule, "schedule", "", "Cron schedule for automatic backups")
	cmd.Flags().IntVar(&opts.retention, "retention", 0, "Number of backups to retain")
	cmd.Flags().BoolVar(&opts.local, "local", false, "Store backups in the local repository")
	cmd.Flags().BoolVar(&opts.s3, "s3", false, "Store backups in the S3 destination")
	cmd.Flags().StringVar(&opts.s3Destination, "s3-destination", "", "S3 destination name or ID (implies --s3)")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("file") {
			return nil
		}
		for _, name := range []string{"policy-id", "enabled", "schedule", "retention", "local", "s3", "s3-destination"} {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--file cannot be combined with --%s", name)
			}
		}
		return nil
	}
	return cmd
}

func newRecoveryKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recovery",
		Short: "Manage the Arcane system backup recovery key",
	}
	cmd.AddCommand(newRecoveryKeyGenerateCmd())
	cmd.AddCommand(newRecoveryKeySetCmd())
	return cmd
}

func newRecoveryKeyGenerateCmd() *cobra.Command {
	var opts struct {
		save bool
	}
	cmd := &cobra.Command{
		Use:          "generate",
		Short:        "Generate a new Arcane system backup recovery key",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			result, err := c.DoJSON[types.SystemBackupRecoveryKey](cmd.Context(), http.MethodPost, clitypes.BackupsRecoveryKeyGenerate(), nil)
			if err != nil {
				return fmt.Errorf("failed to generate recovery key: %w", err)
			}

			jsonMode := cmdutil.JSONOutputEnabled(cmd)
			if !jsonMode {
				output.Header("Generated Recovery Key")
				_, _ = fmt.Fprintln(output.Stdout(), result.RecoveryKey)
				output.Warning("Store this recovery key somewhere safe. Backups encrypted with it cannot be restored without it.")
			}

			save := opts.save
			if !save && cmdutil.AllowPrompt(cmd) {
				save, err = cmdutil.Confirm(cmd, "Store this recovery key on the server now?")
				if err != nil {
					return err
				}
			}
			if !save {
				if jsonMode {
					return cmdutil.PrintJSON(result)
				}
				_, _ = fmt.Fprintln(output.Stdout(), "Run `arcane admin backups recovery set` to store it on the server later.")
				return nil
			}
			storeRequest := types.SystemBackupRecoveryKey{RecoveryKey: result.RecoveryKey}
			if _, storeErr := c.DoJSON[types.SystemBackupRecoveryKeyStatus](cmd.Context(), http.MethodPut, clitypes.BackupsRecoveryKey(), storeRequest); storeErr != nil {
				return fmt.Errorf("failed to store recovery key: %w", storeErr)
			}
			if jsonMode {
				return cmdutil.PrintJSON(result)
			}
			output.Success("Recovery key stored on the server")
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.save, "save", false, "Store the generated recovery key on the server")
	return cmd
}

func newRecoveryKeySetCmd() *cobra.Command {
	var opts struct {
		recoveryKey string
	}
	cmd := &cobra.Command{
		Use:          "set",
		Short:        "Configure the Arcane system backup recovery key",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := opts.recoveryKey
			if key == "" {
				var err error
				if key, err = output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "Recovery key", "recovery-key"); err != nil {
					return fmt.Errorf("failed to read recovery key: %w", err)
				}
			}
			if key == "" {
				return errors.New("recovery key is required; use --recovery-key")
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			status, err := c.DoJSON[types.SystemBackupRecoveryKeyStatus](cmd.Context(), http.MethodPut, clitypes.BackupsRecoveryKey(), types.SystemBackupRecoveryKey{RecoveryKey: key})
			if err != nil {
				return fmt.Errorf("failed to set recovery key: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(status)
			}

			output.Success("Recovery key configured successfully")
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.recoveryKey, "recovery-key", "", "Recovery key to store (prompted for when omitted)")
	return cmd
}

func renderSystemPolicies(policies types.SystemBackupPolicyCollection) error {
	output.Header("System Backup Policies")
	output.KeyValue("Recovery Key", kit.Ternary(policies.RecoveryKeyStored, "Configured", "Not configured"))

	headers := []string{"ID", "ENABLED", "SCHEDULE", "RETENTION", "LOCAL", "S3", "S3 DESTINATION", "LAST RUN"}
	rows := make([][]string, len(policies.Policies))
	for i, policy := range policies.Policies {
		lastRun := ""
		if policy.LastRun != nil {
			lastRun = fmt.Sprintf("%s (%s)", policy.LastRun.CreatedAt.Format("2006-01-02 15:04"), policy.LastRun.Status)
		}
		rows[i] = []string{
			policy.ID,
			strconv.FormatBool(policy.Enabled),
			policy.Schedule,
			strconv.Itoa(policy.RetentionCount),
			strconv.FormatBool(policy.LocalEnabled),
			strconv.FormatBool(policy.S3Enabled),
			policy.S3DestinationName,
			lastRun,
		}
	}
	return output.Table(headers, rows)
}

type policiesUpdateOptions struct {
	file          string
	id            string
	enabled       bool
	schedule      string
	retention     int
	local         bool
	s3            bool
	s3Destination string
}

func (opts policiesUpdateOptions) requestFromFile() (types.UpdateSystemBackupPolicies, error) {
	var req types.UpdateSystemBackupPolicies
	var data []byte
	var err error
	if opts.file == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return req, fmt.Errorf("failed to read policies from stdin: %w", err)
		}
	} else {
		data, err = os.ReadFile(opts.file)
		if err != nil {
			return req, fmt.Errorf("failed to read file %s: %w", opts.file, err)
		}
	}
	if err = json.Unmarshal(data, &req); err != nil {
		return req, fmt.Errorf("failed to parse policies file: %w", err)
	}
	return req, nil
}

func (opts policiesUpdateOptions) requestFromFlags(cmd *cobra.Command, c *client.Client) (types.UpdateSystemBackupPolicies, error) {
	var req types.UpdateSystemBackupPolicies
	current, err := c.DoJSON[types.SystemBackupPolicyCollection](cmd.Context(), http.MethodGet, clitypes.BackupsPolicies(), nil)
	if err != nil {
		return req, fmt.Errorf("failed to get current backup policies: %w", err)
	}

	req.Policies = make([]types.UpdateBackupPolicy, len(current.Policies))
	targetIndex := -1
	for i, p := range current.Policies {
		req.Policies[i] = types.UpdateBackupPolicy{
			ID:              p.ID,
			Enabled:         p.Enabled,
			Schedule:        p.Schedule,
			RetentionCount:  p.RetentionCount,
			LocalEnabled:    p.LocalEnabled,
			S3Enabled:       p.S3Enabled,
			S3DestinationID: p.S3DestinationID,
		}
		if opts.id != "" && p.ID == opts.id {
			targetIndex = i
		}
	}

	switch {
	case opts.id != "" && targetIndex == -1:
		return req, fmt.Errorf("policy %q not found; run `arcane admin backups policies`", opts.id)
	case opts.id == "" && len(req.Policies) > 1:
		return req, fmt.Errorf("%d backup policies exist; select one with --policy-id", len(req.Policies))
	case opts.id == "" && len(req.Policies) == 1:
		targetIndex = 0
	case targetIndex == -1:
		if !cmd.Flags().Changed("schedule") {
			return req, errors.New("--schedule is required when creating a new backup policy")
		}
		// New policies default to local storage unless --local was set explicitly.
		req.Policies = append(req.Policies, types.UpdateBackupPolicy{LocalEnabled: !cmd.Flags().Changed("local") || opts.local})
		targetIndex = len(req.Policies) - 1
	}

	return req, opts.applyFlags(cmd, c, &req.Policies[targetIndex])
}

func (opts policiesUpdateOptions) applyFlags(cmd *cobra.Command, c *client.Client, target *types.UpdateBackupPolicy) error {
	if cmd.Flags().Changed("enabled") {
		target.Enabled = opts.enabled
	}
	if cmd.Flags().Changed("schedule") {
		target.Schedule = opts.schedule
	}
	if cmd.Flags().Changed("retention") {
		target.RetentionCount = opts.retention
	}
	if cmd.Flags().Changed("local") {
		target.LocalEnabled = opts.local
	}
	if cmd.Flags().Changed("s3") {
		target.S3Enabled = opts.s3
	}
	if cmd.Flags().Changed("s3-destination") {
		resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, opts.s3Destination, cmdutil.AllowPrompt(cmd))
		if err != nil {
			return err
		}
		target.S3DestinationID = resolved.ID
		if !cmd.Flags().Changed("s3") {
			target.S3Enabled = true
		}
	}
	return nil
}
