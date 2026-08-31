package backups

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"emperror.dev/errors"

	"github.com/charmbracelet/x/term"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	startFlag  int
	allFlag    bool
	forceFlag  bool
	jsonOutput bool

	createDestination   string
	createS3Destination string
	createLocal         bool
	createRecoveryKey   string
	createPolicyID      string

	deleteRecoveryKey string

	restoreRecoveryKey string

	uploadS3Destination string
	uploadRecoveryKey   string

	discoverS3Destination string
	discoverRecoveryKey   string

	policiesUpdateFile          string
	policiesUpdateID            string
	policiesUpdateEnabled       bool
	policiesUpdateDisabled      bool
	policiesUpdateSchedule      string
	policiesUpdateRetention     int
	policiesUpdateLocal         bool
	policiesUpdateS3            bool
	policiesUpdateS3Destination string

	generateSave   bool
	setRecoveryKey string
)

// BackupsCmd is the parent command for Arcane system backup operations (admin-only).
var BackupsCmd = &cobra.Command{
	Use:     "backups",
	Aliases: []string{"backup"},
	Short:   "Manage Arcane system backups (admin only)",
	Long: `System backups snapshot Arcane's database and configuration, encrypted with the recovery key.
Backups are stored in the local repository, a saved S3 destination, or both; policies schedule automatic runs.`,
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List Arcane system backups",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[backup.SystemBackupRun]{
			Resource: "backups",
			Endpoint: types.Backups(),
			Params:   cmdutil.ListParams{Resource: "backups", Limit: limitFlag, FallbackDefault: 20, Start: startFlag, All: allFlag},
			JSON:     cmdutil.JSONOutputEnabled(cmd),
			Headers:  []string{"ID", "STATUS", "TRIGGER", "DESTINATION", "SIZE", "S3 DESTINATION", "CREATED"},
			Row: func(run backup.SystemBackupRun) []string {
				size := ""
				if run.Size > 0 {
					size = output.Bytes(run.Size)
				}
				return []string{
					run.ID,
					run.Status,
					run.Trigger,
					string(run.Destination),
					size,
					run.S3DestinationName,
					run.CreatedAt.Format("2006-01-02 15:04"),
				}
			},
		})
	},
}

var createCmd = &cobra.Command{
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
		if createS3Destination != "" {
			resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, createS3Destination, !cmdutil.JSONOutputEnabled(cmd) && prompt.IsInteractive())
			if err != nil {
				return err
			}
			s3DestinationID = resolved.ID
		}

		destination := backup.SystemBackupDestination(createDestination)
		if cmd.Flags().Changed("destination") {
			switch destination {
			case backup.SystemBackupDestinationLocal, backup.SystemBackupDestinationS3, backup.SystemBackupDestinationLocalS3:
			default:
				return errors.Errorf("invalid --destination %q: must be local, s3, or local_s3", createDestination)
			}
		} else {
			switch {
			case s3DestinationID != "" && createLocal:
				destination = backup.SystemBackupDestinationLocalS3
			case s3DestinationID != "":
				destination = backup.SystemBackupDestinationS3
			default:
				destination = backup.SystemBackupDestinationLocal
			}
		}
		if (destination == backup.SystemBackupDestinationS3 || destination == backup.SystemBackupDestinationLocalS3) && s3DestinationID == "" {
			return errors.Errorf("--destination %s requires --s3-destination", destination)
		}

		req := backup.CreateSystemBackupRequest{
			Destination:     destination,
			S3DestinationID: s3DestinationID,
			RecoveryKey:     createRecoveryKey,
			PolicyID:        createPolicyID,
		}

		run, err := c.DoJSON[backup.SystemBackupRun](cmd.Context(), http.MethodPost, types.Backups(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create backup")
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

var deleteCmd = &cobra.Command{
	Use:          "delete <backup-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete an Arcane system backup",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete backup %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.DoJSON[base.ApiResponse[base.MessageResponse]](cmd.Context(), http.MethodDelete, types.Backup(args[0]), backup.DeleteSystemBackupRequest{RecoveryKey: deleteRecoveryKey})
		if err != nil {
			return errors.WrapIf(err, "failed to delete backup")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Backup %s deleted successfully", args[0])
		return nil
	},
}

var restoreCmd = &cobra.Command{
	Use:          "restore <backup-id>",
	Short:        "Restore Arcane from a system backup",
	Long:         `Restore overwrites the current Arcane database and configuration with the backup contents, then restarts Arcane.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			output.Warning("Restoring backup %s will OVERWRITE the current Arcane database and configuration, then restart Arcane. This cannot be undone.", args[0])
			confirmed, err := cmdutil.Confirm(cmd, "Are you absolutely sure you want to restore this backup?")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		key := restoreRecoveryKey
		if key == "" {
			if !prompt.IsInteractive() {
				return errors.New("recovery key is required; pass --recovery-key")
			}
			fmt.Print("Recovery key: ")
			byteKey, err := term.ReadPassword(os.Stdin.Fd())
			if err != nil {
				return errors.WrapIf(err, "failed to read recovery key")
			}
			key = string(byteKey)
			fmt.Println()
			if key == "" {
				return errors.New("recovery key is required")
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.PostJSON[base.MessageResponse](cmd.Context(), types.BackupRestore(args[0]), backup.RestoreSystemBackupRequest{RecoveryKey: key})
		if err != nil {
			return errors.WrapIf(err, "failed to restore backup")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("%s", result.Data.Message)
		output.Warning("Arcane will restart while the restore is applied; the server may be unavailable for a short time.")
		return nil
	},
}

var uploadCmd = &cobra.Command{
	Use:          "upload <backup-id>",
	Short:        "Upload an Arcane system backup to an S3 destination",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, uploadS3Destination, !cmdutil.JSONOutputEnabled(cmd) && prompt.IsInteractive())
		if err != nil {
			return err
		}

		req := backup.UploadSystemBackupRequest{
			S3DestinationID: resolved.ID,
			RecoveryKey:     uploadRecoveryKey,
		}

		run, err := c.DoJSON[backup.SystemBackupRun](cmd.Context(), http.MethodPost, types.BackupUpload(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to upload backup")
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

var discoverCmd = &cobra.Command{
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

		resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, discoverS3Destination, !cmdutil.JSONOutputEnabled(cmd) && prompt.IsInteractive())
		if err != nil {
			return err
		}

		req := backup.DiscoverSystemBackupsRequest{
			S3DestinationID: resolved.ID,
			RecoveryKey:     discoverRecoveryKey,
		}

		result, err := c.PostJSON[int](cmd.Context(), types.BackupsDiscover(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to discover backups")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Discovered %d backup(s)", result.Data)
		return nil
	},
}

var policiesCmd = &cobra.Command{
	Use:          "policies",
	Short:        "Show Arcane system backup policies",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.DoJSON[backup.SystemBackupPolicyCollection](cmd.Context(), http.MethodGet, types.BackupsPolicies(), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to get backup policies")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result)
		}

		printPolicies(result)
		return nil
	},
}

func printPolicies(result backup.SystemBackupPolicyCollection) {
	output.Header("System Backup Policies")
	recoveryKey := "Not configured"
	if result.RecoveryKeyStored {
		recoveryKey = "Configured"
	}
	output.KeyValue("Recovery Key", recoveryKey)

	headers := []string{"ID", "ENABLED", "SCHEDULE", "RETENTION", "LOCAL", "S3", "S3 DESTINATION", "LAST RUN"}
	rows := make([][]string, len(result.Policies))
	for i, policy := range result.Policies {
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
	output.Table(headers, rows)
}

var policiesUpdateCmd = &cobra.Command{
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

		var req backup.UpdateSystemBackupPolicies
		if policiesUpdateFile != "" {
			for _, name := range []string{"policy-id", "enabled", "disabled", "schedule", "retention", "local", "s3", "s3-destination"} {
				if cmd.Flags().Changed(name) {
					return errors.Errorf("--file cannot be combined with --%s", name)
				}
			}

			var data []byte
			if policiesUpdateFile == "-" {
				data, err = io.ReadAll(os.Stdin)
				if err != nil {
					return errors.WrapIf(err, "failed to read policies from stdin")
				}
			} else {
				data, err = os.ReadFile(policiesUpdateFile)
				if err != nil {
					return errors.WrapIff(err, "failed to read file %s", policiesUpdateFile)
				}
			}
			if err := json.Unmarshal(data, &req); err != nil {
				return errors.WrapIf(err, "failed to parse policies file")
			}
		} else {
			current, err := c.DoJSON[backup.SystemBackupPolicyCollection](cmd.Context(), http.MethodGet, types.BackupsPolicies(), nil)
			if err != nil {
				return errors.WrapIf(err, "failed to get current backup policies")
			}

			req.Policies = make([]backup.UpdateSystemBackupPolicy, len(current.Policies))
			targetIndex := -1
			for i, p := range current.Policies {
				req.Policies[i] = backup.UpdateSystemBackupPolicy{
					ID:              p.ID,
					Enabled:         p.Enabled,
					Schedule:        p.Schedule,
					RetentionCount:  p.RetentionCount,
					LocalEnabled:    p.LocalEnabled,
					S3Enabled:       p.S3Enabled,
					S3DestinationID: p.S3DestinationID,
				}
				if policiesUpdateID != "" && p.ID == policiesUpdateID {
					targetIndex = i
				}
			}

			switch {
			case policiesUpdateID != "" && targetIndex == -1:
				return errors.Errorf("policy %q not found; run `arcane backups policies`", policiesUpdateID)
			case policiesUpdateID == "" && len(req.Policies) > 1:
				return errors.Errorf("%d backup policies exist; select one with --policy-id", len(req.Policies))
			case policiesUpdateID == "" && len(req.Policies) == 1:
				targetIndex = 0
			case targetIndex == -1:
				if !cmd.Flags().Changed("schedule") {
					return errors.New("--schedule is required when creating a new backup policy")
				}
				// New policies default to local storage unless --local was set explicitly.
				req.Policies = append(req.Policies, backup.UpdateSystemBackupPolicy{LocalEnabled: !cmd.Flags().Changed("local") || policiesUpdateLocal})
				targetIndex = len(req.Policies) - 1
			}

			target := &req.Policies[targetIndex]
			if cmd.Flags().Changed("enabled") {
				target.Enabled = true
			}
			if cmd.Flags().Changed("disabled") {
				target.Enabled = false
			}
			if cmd.Flags().Changed("schedule") {
				target.Schedule = policiesUpdateSchedule
			}
			if cmd.Flags().Changed("retention") {
				target.RetentionCount = policiesUpdateRetention
			}
			if cmd.Flags().Changed("local") {
				target.LocalEnabled = policiesUpdateLocal
			}
			if cmd.Flags().Changed("s3") {
				target.S3Enabled = policiesUpdateS3
			}
			if cmd.Flags().Changed("s3-destination") {
				resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, policiesUpdateS3Destination, !cmdutil.JSONOutputEnabled(cmd) && prompt.IsInteractive())
				if err != nil {
					return err
				}
				target.S3DestinationID = resolved.ID
				if !cmd.Flags().Changed("s3") {
					target.S3Enabled = true
				}
			}
		}

		result, err := c.DoJSON[backup.SystemBackupPolicyCollection](cmd.Context(), http.MethodPut, types.BackupsPolicies(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update backup policies")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result)
		}

		output.Success("Backup policies updated successfully")
		printPolicies(result)
		return nil
	},
}

var recoveryKeyCmd = &cobra.Command{
	Use:   "recovery",
	Short: "Manage the Arcane system backup recovery key",
}

var recoveryKeyGenerateCmd = &cobra.Command{
	Use:          "generate",
	Short:        "Generate a new Arcane system backup recovery key",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.DoJSON[backup.SystemBackupRecoveryKey](cmd.Context(), http.MethodPost, types.BackupsRecoveryKeyGenerate(), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to generate recovery key")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			if generateSave {
				if err := storeRecoveryKey(cmd, c, result.RecoveryKey); err != nil {
					return err
				}
			}
			return cmdutil.PrintJSON(result)
		}

		output.Header("Generated Recovery Key")
		fmt.Println(result.RecoveryKey)
		output.Warning("Store this recovery key somewhere safe. Backups encrypted with it cannot be restored without it.")

		save := generateSave
		if !save && prompt.IsInteractive() {
			save, err = cmdutil.Confirm(cmd, "Store this recovery key on the server now?")
			if err != nil {
				return err
			}
		}
		if !save {
			fmt.Println("Run `arcane backups recovery set` to store it on the server later.")
			return nil
		}
		if err := storeRecoveryKey(cmd, c, result.RecoveryKey); err != nil {
			return err
		}
		output.Success("Recovery key stored on the server")
		return nil
	},
}

var recoveryKeySetCmd = &cobra.Command{
	Use:          "set",
	Short:        "Configure the Arcane system backup recovery key",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		key := setRecoveryKey
		if key == "" {
			if !prompt.IsInteractive() {
				return errors.New("recovery key is required; pass --recovery-key")
			}
			fmt.Print("Recovery key: ")
			byteKey, err := term.ReadPassword(os.Stdin.Fd())
			if err != nil {
				return errors.WrapIf(err, "failed to read recovery key")
			}
			key = string(byteKey)
			fmt.Println()
		}
		if key == "" {
			return errors.New("recovery key is required; use --recovery-key")
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		status, err := c.DoJSON[backup.SystemBackupRecoveryKeyStatus](cmd.Context(), http.MethodPut, types.BackupsRecoveryKey(), backup.SystemBackupRecoveryKey{RecoveryKey: key})
		if err != nil {
			return errors.WrapIf(err, "failed to set recovery key")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(status)
		}

		output.Success("Recovery key configured successfully")
		return nil
	},
}

func storeRecoveryKey(cmd *cobra.Command, c *client.Client, key string) error {
	_, err := c.DoJSON[backup.SystemBackupRecoveryKeyStatus](cmd.Context(), http.MethodPut, types.BackupsRecoveryKey(), backup.SystemBackupRecoveryKey{RecoveryKey: key})
	return errors.WrapIf(err, "failed to store recovery key")
}

func init() {
	BackupsCmd.AddCommand(listCmd)
	BackupsCmd.AddCommand(createCmd)
	BackupsCmd.AddCommand(deleteCmd)
	BackupsCmd.AddCommand(restoreCmd)
	BackupsCmd.AddCommand(uploadCmd)
	BackupsCmd.AddCommand(discoverCmd)
	BackupsCmd.AddCommand(policiesCmd)
	BackupsCmd.AddCommand(recoveryKeyCmd)

	policiesCmd.AddCommand(policiesUpdateCmd)
	recoveryKeyCmd.AddCommand(recoveryKeyGenerateCmd)
	recoveryKeyCmd.AddCommand(recoveryKeySetCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of backups to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&createDestination, "destination", "", "Backup destination: local, s3, or local_s3 (inferred from --local/--s3-destination when omitted)")
	createCmd.Flags().StringVar(&createS3Destination, "s3-destination", "", "S3 destination name or ID")
	createCmd.Flags().BoolVar(&createLocal, "local", false, "Also keep a local copy when backing up to S3")
	createCmd.Flags().StringVar(&createRecoveryKey, "recovery-key", "", "Recovery key used to encrypt the backup")
	createCmd.Flags().StringVar(&createPolicyID, "policy", "", "Backup policy ID to associate with the run")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().StringVar(&deleteRecoveryKey, "recovery-key", "", "Recovery key (required to delete remote snapshots)")
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Delete without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	restoreCmd.Flags().StringVar(&restoreRecoveryKey, "recovery-key", "", "Recovery key the backup was encrypted with (prompted for when omitted)")
	restoreCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Restore without confirmation")
	restoreCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	uploadCmd.Flags().StringVar(&uploadS3Destination, "s3-destination", "", "S3 destination name or ID to upload to")
	uploadCmd.Flags().StringVar(&uploadRecoveryKey, "recovery-key", "", "Recovery key the backup was encrypted with")
	uploadCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = uploadCmd.MarkFlagRequired("s3-destination")

	discoverCmd.Flags().StringVar(&discoverS3Destination, "s3-destination", "", "S3 destination name or ID to search")
	discoverCmd.Flags().StringVar(&discoverRecoveryKey, "recovery-key", "", "Recovery key the backups were encrypted with")
	discoverCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = discoverCmd.MarkFlagRequired("s3-destination")

	policiesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	policiesUpdateCmd.Flags().StringVar(&policiesUpdateFile, "file", "", "JSON file with the full policies payload (use - for stdin); replaces all policies")
	policiesUpdateCmd.Flags().StringVar(&policiesUpdateID, "policy-id", "", "Policy ID to update (defaults to the sole policy)")
	policiesUpdateCmd.Flags().BoolVar(&policiesUpdateEnabled, "enabled", false, "Enable the policy")
	policiesUpdateCmd.Flags().BoolVar(&policiesUpdateDisabled, "disabled", false, "Disable the policy")
	policiesUpdateCmd.Flags().StringVar(&policiesUpdateSchedule, "schedule", "", "Cron schedule for automatic backups")
	policiesUpdateCmd.Flags().IntVar(&policiesUpdateRetention, "retention", 0, "Number of backups to retain")
	policiesUpdateCmd.Flags().BoolVar(&policiesUpdateLocal, "local", false, "Store backups in the local repository")
	policiesUpdateCmd.Flags().BoolVar(&policiesUpdateS3, "s3", false, "Store backups in the S3 destination")
	policiesUpdateCmd.Flags().StringVar(&policiesUpdateS3Destination, "s3-destination", "", "S3 destination name or ID (implies --s3)")
	policiesUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	policiesUpdateCmd.MarkFlagsMutuallyExclusive("enabled", "disabled")

	recoveryKeyGenerateCmd.Flags().BoolVar(&generateSave, "save", false, "Store the generated recovery key on the server")
	recoveryKeyGenerateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	recoveryKeySetCmd.Flags().StringVar(&setRecoveryKey, "recovery-key", "", "Recovery key to store (prompted for when omitted)")
	recoveryKeySetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
