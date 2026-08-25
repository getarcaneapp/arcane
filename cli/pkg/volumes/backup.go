package volumes

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/spf13/cobra"
)

var (
	backupCreateDestination   string
	backupCreateS3Destination string
	backupCreatePolicyID      string
	backupUploadS3Destination string
	restorePaths              []string
	restoreArchiveFile        string
	policyEnabled             bool
	policySchedule            string
	policyRetention           int
	policyStopContainers      bool
	policyLocal               bool
	policyS3                  bool
	policyS3Destination       string
	policyID                  string
	policyFile                string
)

// backupsListResponse mirrors the backend's paginated volume backup response,
// which carries warnings alongside the standard pagination envelope.
type backupsListResponse struct {
	Success    bool                    `json:"success"`
	Data       []volume.BackupEntry    `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
	Warnings   []string                `json:"warnings,omitempty"`
}

var renameCmd = &cobra.Command{
	Use:          "rename <volume-name> <new-name>",
	Short:        "Rename an unused volume",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !forceFlag && !jsonOutput && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Rename volume %s to %s? The volume is copied and the source is removed.", resolved.Name, args[1]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.Post(cmd.Context(), types.VolumeRename(c.EnvID(), resolved.Name), volume.Rename{Name: args[1]})
		if err != nil {
			return errors.WrapIf(err, "failed to rename volume")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to rename volume")
		}

		var result base.ApiResponse[volume.Volume]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Volume %s renamed to %s", resolved.Name, result.Data.Name)
		return nil
	},
}

var backupsPolicyCmd = &cobra.Command{
	Use:          "policy <volume-name>",
	Short:        "Show volume backup policies",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.VolumeBackupPolicy(c.EnvID(), resolved.Name))
		if err != nil {
			return errors.WrapIf(err, "failed to get backup policies")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to get backup policies")
		}

		var result base.ApiResponse[volume.BackupPolicyCollection]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		printBackupPolicies(resolved.Name, result.Data)
		return nil
	},
}

var backupsPolicyUpdateCmd = &cobra.Command{
	Use:          "update <volume-name>",
	Short:        "Update volume backup policies",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		var payload volume.UpdateBackupPolicies
		if policyFile != "" {
			raw, err := os.ReadFile(policyFile)
			if err != nil {
				return errors.WrapIf(err, "failed to read policy file")
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				return errors.WrapIf(err, "failed to parse policy file")
			}
		} else {
			payload, err = buildPolicyUpdate(cmd, c, resolved.Name)
			if err != nil {
				return err
			}
		}

		resp, err := c.Put(cmd.Context(), types.VolumeBackupPolicy(c.EnvID(), resolved.Name), payload)
		if err != nil {
			return errors.WrapIf(err, "failed to update backup policies")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update backup policies")
		}

		var result base.ApiResponse[volume.BackupPolicyCollection]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Backup policies for volume %s saved successfully", resolved.Name)
		printBackupPolicies(resolved.Name, result.Data)
		return nil
	},
}

// buildPolicyUpdate merges the flag values into the volume's current policy
// collection: the whole collection is PUT back, with only the targeted policy
// changed. A new policy is appended when the volume has none yet.
func buildPolicyUpdate(cmd *cobra.Command, c *client.Client, volumeName string) (volume.UpdateBackupPolicies, error) {
	var payload volume.UpdateBackupPolicies

	resp, err := c.Get(cmd.Context(), types.VolumeBackupPolicy(c.EnvID(), volumeName))
	if err != nil {
		return payload, errors.WrapIf(err, "failed to get current backup policies")
	}
	defer func() { _ = resp.Body.Close() }()
	if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
		return payload, errors.WrapIf(err, "failed to get current backup policies")
	}

	var current base.ApiResponse[volume.BackupPolicyCollection]
	if err := cmdutil.DecodeJSON(resp, &current); err != nil {
		return payload, err
	}

	payload.Policies = make([]volume.UpdateBackupPolicy, len(current.Data.Policies))
	targetIndex := -1
	for i, p := range current.Data.Policies {
		payload.Policies[i] = volume.UpdateBackupPolicy{
			ID:              p.ID,
			Enabled:         p.Enabled,
			Schedule:        p.Schedule,
			RetentionCount:  p.RetentionCount,
			StopContainers:  p.StopContainers,
			LocalEnabled:    p.LocalEnabled,
			S3Enabled:       p.S3Enabled,
			S3DestinationID: p.S3DestinationID,
		}
		if policyID != "" && p.ID == policyID {
			targetIndex = i
		}
	}

	switch {
	case policyID != "" && targetIndex == -1:
		return payload, errors.Errorf("policy %q not found for volume %q", policyID, volumeName)
	case policyID == "" && len(payload.Policies) > 1:
		return payload, errors.Errorf("volume %q has %d backup policies; select one with --policy-id", volumeName, len(payload.Policies))
	case policyID == "" && len(payload.Policies) == 1:
		targetIndex = 0
	case targetIndex == -1:
		if policySchedule == "" {
			return payload, errors.New("--schedule is required when creating a new backup policy")
		}
		// New policies default to local storage unless --local was set explicitly.
		payload.Policies = append(payload.Policies, volume.UpdateBackupPolicy{LocalEnabled: !cmd.Flags().Changed("local") || policyLocal})
		targetIndex = len(payload.Policies) - 1
	}

	target := &payload.Policies[targetIndex]
	if cmd.Flags().Changed("enabled") {
		target.Enabled = policyEnabled
	}
	if cmd.Flags().Changed("schedule") {
		target.Schedule = policySchedule
	}
	if cmd.Flags().Changed("retention") {
		target.RetentionCount = policyRetention
	}
	if cmd.Flags().Changed("stop-containers") {
		target.StopContainers = policyStopContainers
	}
	if cmd.Flags().Changed("local") {
		target.LocalEnabled = policyLocal
	}
	if cmd.Flags().Changed("s3") {
		target.S3Enabled = policyS3
	}
	if cmd.Flags().Changed("s3-destination") {
		target.S3DestinationID = policyS3Destination
	}
	return payload, nil
}

func printBackupPolicies(volumeName string, collection volume.BackupPolicyCollection) {
	output.Header("Backup Policies: %s", volumeName)
	output.KeyValue("S3 Available", collection.S3Available)
	if len(collection.Policies) == 0 {
		fmt.Println("No backup policies configured")
		return
	}

	headers := []string{"ID", "ENABLED", "SCHEDULE", "RETENTION", "STOP CONTAINERS", "LOCAL", "S3", "S3 DESTINATION"}
	rows := make([][]string, len(collection.Policies))
	for i, p := range collection.Policies {
		s3Destination := p.S3DestinationName
		if s3Destination == "" {
			s3Destination = p.S3DestinationID
		}
		rows[i] = []string{
			p.ID,
			output.TintEnabled(yesNo(p.Enabled)),
			p.Schedule,
			strconv.Itoa(p.RetentionCount),
			yesNo(p.StopContainers),
			yesNo(p.LocalEnabled),
			yesNo(p.S3Enabled),
			s3Destination,
		}
	}
	output.Table(headers, rows)
}

func yesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

var backupsCmd = &cobra.Command{
	Use:          "backups <volume-name>",
	Short:        "Manage volume backups",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runListBackups,
}

var backupsListCmd = &cobra.Command{
	Use:          "list <volume-name>",
	Aliases:      []string{"ls"},
	Short:        "List volume backups",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runListBackups,
}

func runListBackups(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	allowPrompt := !jsonOutput && prompt.IsInteractive()
	resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
	if err != nil {
		return err
	}

	path, err := cmdutil.ApplyPaginationParams(cmd, types.VolumeBackups(c.EnvID(), resolved.Name), cmdutil.ListParams{
		Resource:        "backups",
		Limit:           limitFlag,
		FallbackDefault: 20,
		Start:           startFlag,
		All:             allFlag,
	})
	if err != nil {
		return errors.WrapIf(err, "failed to build pagination query")
	}

	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return errors.WrapIf(err, "failed to list backups")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := cmdutil.ReadJSONBody(resp)
	if err != nil {
		return errors.WrapIf(err, "failed to list backups")
	}

	if jsonOutput {
		return cmdutil.PrintRawJSON(body)
	}

	var result backupsListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return errors.WrapIf(err, "failed to parse response")
	}

	headers := []string{"ID", "SIZE", "CREATED", "STATUS", "TRIGGER", "DESTINATION", "FORMAT"}
	rows := make([][]string, len(result.Data))
	for i, backup := range result.Data {
		rows[i] = []string{
			backup.ID,
			output.Bytes(backup.Size),
			backup.CreatedAt,
			output.TintStatus(backup.Status),
			backup.Trigger,
			string(backup.Destination),
			string(backup.Format),
		}
	}

	output.Table(headers, rows)
	output.Showing(len(result.Data), result.Pagination.TotalItems, "backups")
	for _, warning := range result.Warnings {
		output.Warning("%s", warning)
	}
	return nil
}

var backupsCreateCmd = &cobra.Command{
	Use:          "create <volume-name>",
	Short:        "Create a volume backup",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		req := volume.CreateBackupRequest{
			Destination:     volume.BackupDestination(backupCreateDestination),
			PolicyID:        backupCreatePolicyID,
			S3DestinationID: backupCreateS3Destination,
		}

		resp, err := c.Post(cmd.Context(), types.VolumeBackups(c.EnvID(), resolved.Name), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create backup")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to create backup")
		}

		var result base.ApiResponse[volume.BackupEntry]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Backup created for volume %s", resolved.Name)
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Status", result.Data.Status)
		output.KeyValue("Destination", string(result.Data.Destination))
		if result.Data.Size > 0 {
			output.KeyValue("Size", output.Bytes(result.Data.Size))
		}
		return nil
	},
}

var backupsRestoreCmd = &cobra.Command{
	Use:   "restore <volume-name> [backup-id]",
	Short: "Restore a volume from a backup",
	Long: `Restore a volume from an existing backup, or from a local tar.gz archive.

With a backup ID the whole backup is restored; add one or more --path flags to
restore only those paths. With --file a local tar.gz backup archive is uploaded
and restored instead (no backup ID).`,
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if restoreArchiveFile != "" && len(args) == 2 {
			return errors.New("--file and a backup ID are mutually exclusive")
		}
		if restoreArchiveFile == "" && len(args) < 2 {
			return errors.New("a backup ID or --file is required")
		}
		if restoreArchiveFile != "" && len(restorePaths) > 0 {
			return errors.New("--path cannot be used with --file")
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !forceFlag && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if restoreArchiveFile != "" {
			return runUploadRestore(cmd, c, resolved.Name)
		}

		backupID := args[1]
		if len(restorePaths) > 0 {
			return runRestorePaths(cmd, c, resolved.Name, backupID)
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Restore volume %s from backup %s? Current volume contents will be replaced.", resolved.Name, backupID))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.Post(cmd.Context(), types.VolumeBackupRestore(c.EnvID(), resolved.Name, backupID), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to restore backup")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to restore backup")
		}

		if jsonOutput {
			body, err := cmdutil.ReadJSONBody(resp)
			if err != nil {
				return err
			}
			return cmdutil.PrintRawJSON(body)
		}

		output.Success("Restore of volume %s from backup %s initiated", resolved.Name, backupID)
		return nil
	},
}

func runRestorePaths(cmd *cobra.Command, c *client.Client, volumeName, backupID string) error {
	if !forceFlag {
		confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Restore %d path(s) into volume %s from backup %s?", len(restorePaths), volumeName, backupID))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Cancelled")
			return nil
		}
	}

	body := struct {
		Paths []string `json:"paths"`
	}{Paths: restorePaths}

	resp, err := c.Post(cmd.Context(), types.VolumeBackupRestoreFiles(c.EnvID(), volumeName, backupID), body)
	if err != nil {
		return errors.WrapIf(err, "failed to restore backup files")
	}
	defer func() { _ = resp.Body.Close() }()
	if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
		return errors.WrapIf(err, "failed to restore backup files")
	}

	if jsonOutput {
		respBody, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return err
		}
		return cmdutil.PrintRawJSON(respBody)
	}

	output.Success("Restore of %d path(s) into volume %s initiated", len(restorePaths), volumeName)
	return nil
}

func runUploadRestore(cmd *cobra.Command, c *client.Client, volumeName string) error {
	if !forceFlag {
		confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Restore volume %s from %s? Current volume contents will be replaced.", volumeName, restoreArchiveFile))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Cancelled")
			return nil
		}
	}

	// Uploading and restoring large backups can take a long time
	c.SetTimeout(30 * time.Minute)

	sessionID, err := cmdutil.UploadFileInChunks(cmd.Context(), c, uploadtypes.KindVolumeBackup, restoreArchiveFile, !jsonOutput)
	if err != nil {
		return err
	}

	respBody, err := c.DoRaw(cmd.Context(), http.MethodPost, types.VolumeBackupUploadRestore(c.EnvID(), volumeName), uploadtypes.ConsumeRequest{UploadID: sessionID})
	if err != nil {
		cmdutil.AbortUploadSession(cmd.Context(), c, uploadtypes.KindVolumeBackup, sessionID)
		return errors.WrapIf(err, "failed to upload and restore backup")
	}

	if jsonOutput {
		fmt.Println(string(respBody))
		return nil
	}

	output.Success("Backup uploaded and restored into volume %s successfully", volumeName)
	return nil
}

var backupsDeleteCmd = &cobra.Command{
	Use:          "delete <backup-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a volume backup",
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

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.VolumeBackup(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete backup")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete backup")
		}

		output.Success("Backup %s deleted successfully", args[0])
		return nil
	},
}

var backupsUploadCmd = &cobra.Command{
	Use:          "upload <backup-id>",
	Short:        "Upload an existing local backup to an S3 destination",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Uploading large backups can take a long time
		c.SetTimeout(30 * time.Minute)

		req := volume.UploadBackupRequest{S3DestinationID: backupUploadS3Destination}
		resp, err := c.Post(cmd.Context(), types.VolumeBackupUpload(c.EnvID(), args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to upload backup")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to upload backup")
		}

		var result base.ApiResponse[volume.BackupEntry]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Backup %s uploaded to S3 successfully", args[0])
		return nil
	},
}

var backupsDownloadCmd = &cobra.Command{
	Use:          "download <backup-id> [output-file]",
	Short:        "Download a volume backup archive",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Downloading large backups can take a long time
		c.SetTimeout(30 * time.Minute)

		resp, err := c.Get(cmd.Context(), types.VolumeBackupDownload(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to download backup")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to download backup")
		}

		outputFile := ""
		if len(args) == 2 {
			outputFile = args[1]
		}
		if outputFile == "" {
			outputFile = downloadFilename(resp, args[0]+".tar.gz")
		}

		if err := writeResponseToFile(resp.Body, outputFile); err != nil {
			return err
		}

		output.Success("Backup downloaded to %s", outputFile)
		return nil
	},
}

// downloadFilename extracts the filename from the response's
// Content-Disposition header, falling back to the given default.
func downloadFilename(resp *http.Response, fallback string) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		if name := params["filename"]; name != "" {
			return name
		}
	}
	return fallback
}

func writeResponseToFile(body io.Reader, path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.WrapIff(err, "failed to create file %s", path)
	}
	if _, err := io.Copy(file, body); err != nil {
		_ = file.Close()
		return errors.WrapIff(err, "failed to write file %s", path)
	}
	if err := file.Close(); err != nil {
		return errors.WrapIff(err, "failed to write file %s", path)
	}
	return nil
}

var backupsFilesCmd = &cobra.Command{
	Use:          "files <backup-id>",
	Short:        "List files in a volume backup",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.VolumeBackupFiles(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to list backup files")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to list backup files")
		}

		var result base.ApiResponse[[]string]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		for _, file := range result.Data {
			fmt.Println(file)
		}
		return nil
	},
}

func init() {
	VolumesCmd.AddCommand(renameCmd)
	VolumesCmd.AddCommand(backupsCmd)

	backupsCmd.AddCommand(backupsListCmd)
	backupsCmd.AddCommand(backupsPolicyCmd)
	backupsCmd.AddCommand(backupsCreateCmd)
	backupsCmd.AddCommand(backupsRestoreCmd)
	backupsCmd.AddCommand(backupsDeleteCmd)
	backupsCmd.AddCommand(backupsUploadCmd)
	backupsCmd.AddCommand(backupsDownloadCmd)
	backupsCmd.AddCommand(backupsFilesCmd)

	backupsPolicyCmd.AddCommand(backupsPolicyUpdateCmd)

	renameCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Rename without confirmation")
	renameCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	backupsPolicyCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	backupsPolicyUpdateCmd.Flags().BoolVar(&policyEnabled, "enabled", false, "Enable or disable the policy")
	backupsPolicyUpdateCmd.Flags().StringVar(&policySchedule, "schedule", "", "Backup schedule (cron expression)")
	backupsPolicyUpdateCmd.Flags().IntVar(&policyRetention, "retention", 0, "Number of backups to retain")
	backupsPolicyUpdateCmd.Flags().BoolVar(&policyStopContainers, "stop-containers", false, "Stop containers using the volume during backup")
	backupsPolicyUpdateCmd.Flags().BoolVar(&policyLocal, "local", false, "Store backups locally")
	backupsPolicyUpdateCmd.Flags().BoolVar(&policyS3, "s3", false, "Store backups in S3")
	backupsPolicyUpdateCmd.Flags().StringVar(&policyS3Destination, "s3-destination", "", "Saved S3 destination ID")
	backupsPolicyUpdateCmd.Flags().StringVar(&policyID, "policy-id", "", "Policy to update when the volume has multiple policies")
	backupsPolicyUpdateCmd.Flags().StringVar(&policyFile, "file", "", "JSON file with the full policy collection to PUT")
	backupsPolicyUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	for _, listLike := range []*cobra.Command{backupsCmd, backupsListCmd} {
		listLike.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of backups to show")
		listLike.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
		listLike.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
		listLike.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	}

	backupsCreateCmd.Flags().StringVar(&backupCreateDestination, "destination", "", "Backup destination (local, s3, local_s3)")
	backupsCreateCmd.Flags().StringVar(&backupCreateS3Destination, "s3-destination", "", "Saved S3 destination ID for a remote backup")
	backupsCreateCmd.Flags().StringVar(&backupCreatePolicyID, "policy-id", "", "Backup policy whose settings should be used")
	backupsCreateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	backupsRestoreCmd.Flags().StringArrayVar(&restorePaths, "path", nil, "Restore only this path from the backup (repeatable)")
	backupsRestoreCmd.Flags().StringVar(&restoreArchiveFile, "file", "", "Local tar.gz backup archive to upload and restore")
	backupsRestoreCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Restore without confirmation")
	backupsRestoreCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	backupsDeleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Delete without confirmation")

	backupsUploadCmd.Flags().StringVar(&backupUploadS3Destination, "s3-destination", "", "S3 destination for the uploaded backup")
	backupsUploadCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = backupsUploadCmd.MarkFlagRequired("s3-destination")

	backupsFilesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
