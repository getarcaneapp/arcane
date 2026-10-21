package volumes

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func newRenameCmd() *cobra.Command {
	var opts struct {
		forceFlag bool
	}
	cmd := &cobra.Command{
		Use:          "rename <volume-name> <new-name>",
		Short:        "Rename an unused volume",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := !opts.forceFlag && cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.forceFlag, fmt.Sprintf("Rename volume %s to %s? The volume is copied and the source is removed.", resolved.Name, args[1]))
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Volume]{
				Method:         http.MethodPost,
				Path:           clitypes.VolumeRename(c.EnvID(), resolved.Name),
				Body:           types.VolumeRename{Name: args[1]},
				FailureMessage: "failed to rename volume",
				Render: func(data types.Volume) error {
					output.Success("Volume %s renamed to %s", resolved.Name, data.Name)
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Rename without confirmation")
	return cmd
}

func newBackupsPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "policy <volume-name>",
		Short:        "Show volume backup policies",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.VolumeBackupPolicyCollection]{
				Method:         http.MethodGet,
				Path:           clitypes.VolumeBackupPolicy(c.EnvID(), resolved.Name),
				FailureMessage: "failed to get backup policies",
				Render: func(data types.VolumeBackupPolicyCollection) error {
					return renderVolumePolicies(resolved.Name, data)
				},
			})
		},
	}
	cmd.AddCommand(newBackupsPolicyUpdateCmd())
	return cmd
}

func newBackupsPolicyUpdateCmd() *cobra.Command {
	var opts backupPolicyOptions
	cmd := &cobra.Command{
		Use:          "update <volume-name>",
		Short:        "Update volume backup policies",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			payload, policyErr := volumeBackupPolicies(cmd, c, resolved.Name, opts)
			if policyErr != nil {
				return policyErr
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.VolumeBackupPolicyCollection]{
				Method:         http.MethodPut,
				Path:           clitypes.VolumeBackupPolicy(c.EnvID(), resolved.Name),
				Body:           payload,
				FailureMessage: "failed to update backup policies",
				Render: func(data types.VolumeBackupPolicyCollection) error {
					output.Success("Backup policies for volume %s saved successfully", resolved.Name)
					return renderVolumePolicies(resolved.Name, data)
				},
			})
		},
	}
	cmd.Flags().BoolVar(&opts.policyEnabled, "enabled", false, "Enable or disable the policy")
	cmd.Flags().StringVar(&opts.policySchedule, "schedule", "", "Backup schedule (cron expression)")
	cmd.Flags().IntVar(&opts.policyRetention, "retention", 0, "Number of backups to retain")
	cmd.Flags().BoolVar(&opts.policyStopContainers, "stop-containers", false, "Stop containers using the volume during backup")
	cmd.Flags().BoolVar(&opts.policyLocal, "local", false, "Store backups locally")
	cmd.Flags().BoolVar(&opts.policyS3, "s3", false, "Store backups in S3")
	cmd.Flags().StringVar(&opts.policyS3Destination, "s3-destination", "", "Saved S3 destination ID")
	cmd.Flags().StringVar(&opts.policyID, "policy-id", "", "Policy to update when the volume has multiple policies")
	cmd.Flags().StringVar(&opts.policyFile, "file", "", "JSON file with the full policy collection to PUT")
	return cmd
}

func newBackupsCmd() *cobra.Command {
	var opts backupListOptions
	cmd := &cobra.Command{
		Use:          "backups <volume-name>",
		Short:        "Manage volume backups",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, args []string) error { return runBackupList(cmd, args, opts) },
	}
	cmd.Flags().IntVarP(&opts.limitFlag, "limit", "n", 20, "Number of backups to show")
	cmd.Flags().IntVar(&opts.startFlag, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	cmd.AddCommand(
		newBackupsListCmd(),
		newBackupsPolicyCmd(),
		newBackupsCreateCmd(),
		newBackupsRestoreCmd(),
		newBackupsDeleteCmd(),
		newBackupsUploadCmd(),
		newBackupsDownloadCmd(),
		newBackupsFilesCmd(),
	)
	return cmd
}

func newBackupsListCmd() *cobra.Command {
	var opts backupListOptions
	cmd := &cobra.Command{
		Use:          "list <volume-name>",
		Aliases:      []string{"ls"},
		Short:        "List volume backups",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, args []string) error { return runBackupList(cmd, args, opts) },
	}
	cmd.Flags().IntVarP(&opts.limitFlag, "limit", "n", 20, "Number of backups to show")
	cmd.Flags().IntVar(&opts.startFlag, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	return cmd
}

func newBackupsCreateCmd() *cobra.Command {
	var opts struct {
		backupCreateDestination   string
		backupCreateS3Destination string
		backupCreatePolicyID      string
	}
	cmd := &cobra.Command{
		Use:          "create <volume-name>",
		Short:        "Create a volume backup",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			req := types.CreateVolumeBackupRequest{
				Destination:     types.VolumeBackupDestination(opts.backupCreateDestination),
				PolicyID:        opts.backupCreatePolicyID,
				S3DestinationID: opts.backupCreateS3Destination,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.VolumeBackupEntry]{
				Method:         http.MethodPost,
				Path:           clitypes.VolumeBackups(c.EnvID(), resolved.Name),
				Body:           req,
				FailureMessage: "failed to create backup",
				Render: func(data types.VolumeBackupEntry) error {
					output.Success("Backup started for volume %s", resolved.Name)
					output.KeyValue("ID", data.ID)
					output.KeyValue("Status", data.Status)
					output.KeyValue("Destination", string(data.Destination))
					if data.Size > 0 {
						output.KeyValue("Size", output.Bytes(data.Size))
					}
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.backupCreateDestination, "destination", "", "Backup destination (local, s3, local_s3)")
	cmd.Flags().StringVar(&opts.backupCreateS3Destination, "s3-destination", "", "Saved S3 destination ID for a remote backup")
	cmd.Flags().StringVar(&opts.backupCreatePolicyID, "policy-id", "", "Backup policy whose settings should be used")
	return cmd
}

func newBackupsRestoreCmd() *cobra.Command {
	var opts struct {
		restorePaths       []string
		restoreArchiveFile string
		forceFlag          bool
	}
	cmd := &cobra.Command{
		Use:   "restore <volume-name> [backup-id]",
		Short: "Restore a volume from a backup",
		Long: `Restore a volume from an existing backup, or from a local tar.gz archive.

With a backup ID the whole backup is restored; add one or more --path flags to
restore only those paths. With --file a local tar.gz backup archive is uploaded
and restored instead (no backup ID).`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.restoreArchiveFile != "" && len(args) == 2 {
				return errors.New("--file and a backup ID are mutually exclusive")
			}
			if opts.restoreArchiveFile == "" && len(args) < 2 {
				return errors.New("a backup ID or --file is required")
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := !opts.forceFlag && cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			backupID := ""
			confirmation := fmt.Sprintf("Restore volume %s from %s? Current volume contents will be replaced.", resolved.Name, opts.restoreArchiveFile)
			if opts.restoreArchiveFile == "" {
				backupID = args[1]
				confirmation = fmt.Sprintf("Restore volume %s from backup %s? Current volume contents will be replaced.", resolved.Name, backupID)
				if len(opts.restorePaths) > 0 {
					confirmation = fmt.Sprintf("Restore %d path(s) into volume %s from backup %s?", len(opts.restorePaths), resolved.Name, backupID)
				}
			}
			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.forceFlag, confirmation)
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}
			if opts.restoreArchiveFile != "" {
				c.SetTimeout(30 * time.Minute)
				sessionID, uploadErr := cmdutil.UploadFileInChunks(cmd.Context(), c, types.UploadKindVolumeBackup, opts.restoreArchiveFile, !cmdutil.JSONOutputEnabled(cmd))
				if uploadErr != nil {
					return uploadErr
				}
				restoreErr := cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
					Method:         http.MethodPost,
					Path:           clitypes.VolumeBackupUploadRestore(c.EnvID(), resolved.Name),
					Body:           types.UploadConsumeRequest{UploadID: sessionID},
					FailureMessage: "failed to upload and restore backup",
					Render: func(types.MessageResponse) error {
						output.Success("Backup uploaded and restored into volume %s successfully", resolved.Name)
						return nil
					},
				})
				if restoreErr != nil {
					cmdutil.AbortUploadSession(cmd.Context(), c, types.UploadKindVolumeBackup, sessionID)
				}
				return restoreErr
			}
			spec := cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.VolumeBackupRestore(c.EnvID(), resolved.Name, backupID),
				FailureMessage: "failed to restore backup",
				Render: func(types.MessageResponse) error {
					output.Success("%s", fmt.Sprintf("Restore of volume %s from backup %s initiated", resolved.Name, backupID))
					return nil
				},
			}
			if len(opts.restorePaths) > 0 {
				spec.Path = clitypes.VolumeBackupRestoreFiles(c.EnvID(), resolved.Name, backupID)
				spec.Body = types.BackupRestoreSelection{Paths: opts.restorePaths}
				spec.FailureMessage = "failed to restore backup files"
				spec.Render = func(types.MessageResponse) error {
					output.Success("Restore of %d path(s) into volume %s initiated", len(opts.restorePaths), resolved.Name)
					return nil
				}
			}
			return cmdutil.Run(cmd, c, spec)
		},
	}
	cmd.Flags().StringArrayVar(&opts.restorePaths, "path", nil, "Restore only this path from the backup (repeatable)")
	cmd.Flags().StringVar(&opts.restoreArchiveFile, "file", "", "Local tar.gz backup archive to upload and restore")
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Restore without confirmation")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error { return cmdutil.ExclusiveFlags(cmd, "file", "path") }
	return cmd
}

func newBackupsDeleteCmd() *cobra.Command {
	var opts struct {
		forceFlag bool
	}
	cmd := &cobra.Command{
		Use:          "delete <backup-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a volume backup",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.forceFlag,
				Prompt:         fmt.Sprintf("Are you sure you want to delete backup %s?", args[0]),
				Path:           clitypes.VolumeBackup(c.EnvID(), args[0]),
				FailureMessage: "failed to delete backup",
				SuccessMessage: fmt.Sprintf("Backup %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Delete without confirmation")
	cmdutil.SetResource(backupRef, cmd)
	return cmd
}

func newBackupsUploadCmd() *cobra.Command {
	var opts struct {
		backupUploadS3Destination string
	}
	cmd := &cobra.Command{
		Use:          "upload <backup-id>",
		Short:        "Upload an existing local backup to an S3 destination",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			// Uploading large backups can take a long time
			c.SetTimeout(30 * time.Minute)

			req := types.UploadVolumeBackupRequest{S3DestinationID: opts.backupUploadS3Destination}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.VolumeBackupEntry]{
				Method:         http.MethodPost,
				Path:           clitypes.VolumeBackupUpload(c.EnvID(), args[0]),
				Body:           req,
				FailureMessage: "failed to upload backup",
				Render: func(types.VolumeBackupEntry) error {
					output.Success("Backup %s uploaded to S3 successfully", args[0])
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.backupUploadS3Destination, "s3-destination", "", "S3 destination for the uploaded backup")
	_ = cmd.MarkFlagRequired("s3-destination")
	cmdutil.SetResource(backupRef, cmd)
	return cmd
}

func newBackupsDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "download <backup-id> [output-file]",
		Short:        "Download a volume backup archive",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			// Downloading large backups can take a long time
			c.SetTimeout(30 * time.Minute)

			resp, err := c.Get(cmd.Context(), clitypes.VolumeBackupDownload(c.EnvID(), args[0]))
			if err != nil {
				return fmt.Errorf("failed to download backup: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if ensureSuccessStatusErr := cmdutil.EnsureSuccessStatus(resp); ensureSuccessStatusErr != nil {
				return fmt.Errorf("failed to download backup: %w", ensureSuccessStatusErr)
			}

			outputFile := ""
			if len(args) == 2 {
				outputFile = args[1]
			}
			if outputFile == "" {
				outputFile = args[0] + ".tar.gz"
				if _, params, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); parseErr == nil {
					outputFile = cmp.Or(params["filename"], outputFile)
				}
			}

			file, openErr := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if openErr != nil {
				return fmt.Errorf("failed to create file %s: %w", outputFile, openErr)
			}
			written, copyErr := output.Copy(cmd.Context(), file, resp.Body, resp.ContentLength, "Downloading backup", !cmdutil.JSONOutputEnabled(cmd))
			if copyErr != nil {
				_ = file.Close()
				return fmt.Errorf("failed to write file %s: %w", outputFile, copyErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				return fmt.Errorf("failed to write file %s: %w", outputFile, closeErr)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path  string `json:"path"`
					Bytes int64  `json:"bytes"`
				}{outputFile, written})
			}
			output.Success("Backup downloaded to %s", outputFile)
			return nil
		},
	}
	cmdutil.SetResource(backupRef, cmd)
	return cmd
}

func newBackupsFilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "files <backup-id>",
		Short:        "List files in a volume backup",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]string]{
				Method:         http.MethodGet,
				Path:           clitypes.VolumeBackupFiles(c.EnvID(), args[0]),
				FailureMessage: "failed to list backup files",
				Render: func(data []string) error {
					for _, file := range data {
						_, _ = fmt.Fprintln(output.Stdout(), file)
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetResource(backupRef, cmd)
	return cmd
}

var backupRef = cmdutil.ResourceRef[types.VolumeBackupEntry, types.VolumeBackupEntry]{
	EnvironmentScoped: true, Singular: "backup", Plural: "backups", IDHint: "the backup ID", ListCmd: "arcane-cli volumes backups list <volume-name>",
	SearchCandidates: func(ctx context.Context, c *client.Client, identifier string) ([]types.VolumeBackupEntry, error) {
		volumes, err := c.GetJSON[[]types.Volume](ctx, clitypes.Volumes(c.EnvID())+"?limit=-1")
		if err != nil {
			return nil, err
		}
		var matches []types.VolumeBackupEntry
		var failures []error
		for _, item := range volumes.Data {
			backups, listErr := c.GetJSON[[]types.VolumeBackupEntry](ctx, clitypes.VolumeBackups(c.EnvID(), item.Name)+"?limit=-1")
			if listErr != nil {
				failures = append(failures, fmt.Errorf("%s: %w", item.Name, listErr))
				continue
			}
			matches = append(matches, slices.DeleteFunc(backups.Data, func(entry types.VolumeBackupEntry) bool { return entry.ID != identifier })...)
		}
		return matches, errors.Join(failures...)
	},
	Label:   func(item types.VolumeBackupEntry) string { return item.VolumeName + " (" + item.ID + ")" },
	Promote: func(item types.VolumeBackupEntry) *types.VolumeBackupEntry { return &item },
}

type backupListOptions struct {
	limitFlag int
	startFlag int
	allFlag   bool
}

func runBackupList(cmd *cobra.Command, args []string, opts backupListOptions) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	allowPrompt := cmdutil.AllowPrompt(cmd)
	resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
	if err != nil {
		return err
	}

	path, err := cmdutil.ApplyPaginationParams(cmd, clitypes.VolumeBackups(c.EnvID(), resolved.Name), cmdutil.ListParams{
		Resource:        "backups",
		Limit:           opts.limitFlag,
		FallbackDefault: 20,
		Start:           opts.startFlag,
		All:             opts.allFlag,
	})
	if err != nil {
		return fmt.Errorf("failed to build pagination query: %w", err)
	}

	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := cmdutil.ReadJSONBody(resp)
	if readErr != nil {
		return fmt.Errorf("failed to list backups: %w", readErr)
	}
	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintRawJSON(body)
	}
	var result types.PaginatedWithWarnings[types.VolumeBackup]
	if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
		return fmt.Errorf("failed to list backups: failed to decode response: %w", decodeErr)
	}

	headers := []string{"ID", "SIZE", "CREATED", "STATUS", "TRIGGER", "DESTINATION", "FORMAT"}
	rows := make([][]string, len(result.Data))
	for i, backup := range result.Data {
		rows[i] = []string{
			backup.ID,
			output.Bytes(backup.Size),
			backup.CreatedAt.Format(time.RFC3339Nano),
			output.TintStatus(backup.Status),
			backup.Trigger,
			string(backup.Destination),
			string(backup.Format),
		}
	}

	if tableErr := output.Table(headers, rows); tableErr != nil {
		return tableErr
	}
	output.Showing(len(result.Data), result.Pagination.TotalItems, "backups")
	for _, warning := range result.Warnings {
		output.Warning("%s", warning)
	}
	return nil
}

func renderVolumePolicies(name string, data types.VolumeBackupPolicyCollection) error {
	output.Header("Backup Policies: %s", name)
	output.KeyValue("S3 Available", data.S3Available)
	if len(data.Policies) == 0 {
		_, writeErr := fmt.Fprintln(output.Stdout(), "No backup policies configured")
		return writeErr
	}

	headers := []string{"ID", "ENABLED", "SCHEDULE", "RETENTION", "STOP CONTAINERS", "LOCAL", "S3", "S3 DESTINATION"}
	rows := make([][]string, len(data.Policies))
	for i, p := range data.Policies {
		rows[i] = []string{
			p.ID,
			output.TintEnabled(kit.Ternary(p.Enabled, "Yes", "No")),
			p.Schedule,
			strconv.Itoa(p.RetentionCount),
			kit.Ternary(p.StopContainers, "Yes", "No"),
			kit.Ternary(p.LocalEnabled, "Yes", "No"),
			kit.Ternary(p.S3Enabled, "Yes", "No"),
			cmp.Or(p.S3DestinationName, p.S3DestinationID),
		}
	}
	return output.Table(headers, rows)
}

type backupPolicyOptions struct {
	policyEnabled        bool
	policySchedule       string
	policyRetention      int
	policyStopContainers bool
	policyLocal          bool
	policyS3             bool
	policyS3Destination  string
	policyID             string
	policyFile           string
}

func volumeBackupPolicies(cmd *cobra.Command, c *client.Client, name string, opts backupPolicyOptions) (types.UpdateVolumeBackupPolicies, error) {
	var payload types.UpdateVolumeBackupPolicies
	if opts.policyFile != "" {
		raw, readFileErr := os.ReadFile(opts.policyFile)
		if readFileErr != nil {
			return payload, fmt.Errorf("failed to read policy file: %w", readFileErr)
		}
		if unmarshalErr := json.Unmarshal(raw, &payload); unmarshalErr != nil {
			return payload, fmt.Errorf("failed to parse policy file: %w", unmarshalErr)
		}
	} else {
		current, policyErr := c.GetJSON[types.VolumeBackupPolicyCollection](cmd.Context(), clitypes.VolumeBackupPolicy(c.EnvID(), name))
		if policyErr != nil {
			return payload, fmt.Errorf("failed to get current backup policies: %w", policyErr)
		}

		payload.Policies = make([]types.UpdateBackupPolicy, len(current.Data.Policies))
		for i, p := range current.Data.Policies {
			payload.Policies[i] = types.UpdateBackupPolicy{
				ID:              p.ID,
				Enabled:         p.Enabled,
				Schedule:        p.Schedule,
				RetentionCount:  p.RetentionCount,
				StopContainers:  p.StopContainers,
				LocalEnabled:    p.LocalEnabled,
				S3Enabled:       p.S3Enabled,
				S3DestinationID: p.S3DestinationID,
			}
		}

		targetIndex := slices.IndexFunc(payload.Policies, func(policy types.UpdateBackupPolicy) bool { return opts.policyID != "" && policy.ID == opts.policyID })
		switch {
		case opts.policyID != "" && targetIndex == -1:
			return payload, fmt.Errorf("policy %q not found for volume %q", opts.policyID, name)
		case opts.policyID == "" && len(payload.Policies) > 1:
			return payload, fmt.Errorf("volume %q has %d backup policies; select one with --policy-id", name, len(payload.Policies))
		case opts.policyID == "" && len(payload.Policies) == 1:
			targetIndex = 0
		case targetIndex == -1:
			if opts.policySchedule == "" {
				return payload, errors.New("--schedule is required when creating a new backup policy")
			}
			// New policies default to local storage unless --local was set explicitly.
			payload.Policies = append(payload.Policies, types.UpdateBackupPolicy{LocalEnabled: !cmd.Flags().Changed("local") || opts.policyLocal})
			targetIndex = len(payload.Policies) - 1
		}

		target := &payload.Policies[targetIndex]
		if cmd.Flags().Changed("enabled") {
			target.Enabled = opts.policyEnabled
		}
		if cmd.Flags().Changed("schedule") {
			target.Schedule = opts.policySchedule
		}
		if cmd.Flags().Changed("retention") {
			target.RetentionCount = opts.policyRetention
		}
		if cmd.Flags().Changed("stop-containers") {
			target.StopContainers = opts.policyStopContainers
		}
		if cmd.Flags().Changed("local") {
			target.LocalEnabled = opts.policyLocal
		}
		if cmd.Flags().Changed("s3") {
			target.S3Enabled = opts.policyS3
		}
		if cmd.Flags().Changed("s3-destination") {
			target.S3DestinationID = opts.policyS3Destination
		}
	}

	return payload, nil
}
