package gitops

import (
	"fmt"
	"net/url"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/spf13/cobra"
)

var (
	limitFlag       int
	startFlag       int
	allFlag         bool
	forceFlag       bool
	jsonOutput      bool
	gitopsFilesPath string
)

var (
	gitopsUpdateName        string
	gitopsUpdateRepoID      string
	gitopsUpdateBranch      string
	gitopsUpdateComposePath string
	gitopsUpdateProjectName string
	gitopsUpdateAutoSync    bool
	gitopsUpdateInterval    int
)

// GitopsCmd is the parent command for gitops sync operations
var GitopsCmd = &cobra.Command{
	Use:     "gitops",
	Aliases: []string{"gitops-syncs", "gs"},
	Short:   "Manage GitOps syncs",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List GitOps syncs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[gitops.GitOpsSync]{
			Resource: "gitops syncs",
			Endpoint: types.GitOpsSyncs(c.EnvID()),
			Params:   cmdutil.ListParams{Resource: "gitops-syncs", Limit: limitFlag, FallbackDefault: 20, Start: startFlag, All: allFlag},
			JSON:     jsonOutput,
			Headers:  []string{"ID", "NAME", "BRANCH", "AUTO-SYNC", "LAST STATUS", "LAST SYNC"},
			Row: func(sync gitops.GitOpsSync) []string {
				autoSync := "false"
				if sync.AutoSync {
					autoSync = "true"
				}
				lastStatus := "-"
				if sync.LastSyncStatus != nil {
					lastStatus = *sync.LastSyncStatus
				}
				lastSync := "-"
				if sync.LastSyncAt != nil {
					lastSync = sync.LastSyncAt.Format("2006-01-02 15:04:05")
				}
				return []string{
					sync.ID,
					sync.Name,
					sync.Branch,
					autoSync,
					lastStatus,
					lastSync,
				}
			},
		})
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a new GitOps sync",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString("name")
		repoID, _ := cmd.Flags().GetString("repo-id")
		branch, _ := cmd.Flags().GetString("branch")
		composePath, _ := cmd.Flags().GetString("compose-path")
		autoSync, _ := cmd.Flags().GetBool("auto-sync")
		interval, _ := cmd.Flags().GetInt("interval")
		projectName, _ := cmd.Flags().GetString("project-name")

		req := gitops.CreateSyncRequest{
			Name:         name,
			RepositoryID: repoID,
			Branch:       branch,
			ComposePath:  composePath,
			ProjectName:  projectName,
			AutoSync:     &autoSync,
		}
		if cmd.Flags().Changed("interval") {
			req.SyncInterval = &interval
		}

		result, err := c.PostJSON[gitops.GitOpsSync](cmd.Context(), types.GitOpsSyncs(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create gitops sync")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("GitOps sync %s created successfully (ID: %s)", result.Data.Name, result.Data.ID)
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <sync-id|name>",
	Short:        "Get GitOps sync details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, complete, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			result, err := c.GetJSON[gitops.GitOpsSync](cmd.Context(), types.GitOpsSync(c.EnvID(), resolved.ID))
			if err != nil {
				return errors.WrapIf(err, "failed to get gitops sync")
			}
			resolved = &result.Data
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
		}

		output.Header("GitOps Sync Details")
		output.KeyValue("ID", resolved.ID)
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("Branch", resolved.Branch)
		output.KeyValue("Compose Path", resolved.ComposePath)
		output.KeyValue("Project Name", resolved.ProjectName)
		output.KeyValue("Auto Sync", resolved.AutoSync)
		output.KeyValue("Sync Interval", fmt.Sprintf("%d min", resolved.SyncInterval))
		if resolved.LastSyncStatus != nil {
			output.KeyValue("Last Status", *resolved.LastSyncStatus)
		}
		if resolved.LastSyncAt != nil {
			output.KeyValue("Last Sync", resolved.LastSyncAt.Format("2006-01-02 15:04:05"))
		}
		if resolved.LastSyncCommit != nil {
			output.KeyValue("Last Commit", *resolved.LastSyncCommit)
		}
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <sync-id|name>",
	Short:        "Update a GitOps sync",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		req := gitops.UpdateSyncRequest{}
		if cmd.Flags().Changed("name") {
			req.Name = &gitopsUpdateName
		}
		if cmd.Flags().Changed("repo-id") {
			req.RepositoryID = &gitopsUpdateRepoID
		}
		if cmd.Flags().Changed("branch") {
			req.Branch = &gitopsUpdateBranch
		}
		if cmd.Flags().Changed("compose-path") {
			req.ComposePath = &gitopsUpdateComposePath
		}
		if cmd.Flags().Changed("project-name") {
			req.ProjectName = &gitopsUpdateProjectName
		}
		if cmd.Flags().Changed("auto-sync") {
			req.AutoSync = &gitopsUpdateAutoSync
		}
		if cmd.Flags().Changed("interval") {
			req.SyncInterval = &gitopsUpdateInterval
		}

		resp, err := c.Put(cmd.Context(), types.GitOpsSync(c.EnvID(), resolved.ID), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update gitops sync")
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update gitops sync")
		}

		output.Success("GitOps sync %s updated successfully", resolved.Name)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <sync-id|name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a GitOps sync",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		if !forceFlag {
			display := resolved.Name
			if display == "" {
				display = resolved.ID
			}
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete gitops sync %s?", display))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.Delete(cmd.Context(), types.GitOpsSync(c.EnvID(), resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to delete gitops sync")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete gitops sync")
		}

		output.Success("GitOps sync %s deleted successfully", resolved.Name)
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:          "status <sync-id|name>",
	Short:        "Get GitOps sync status",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		result, err := c.GetJSON[gitops.SyncStatus](cmd.Context(), types.GitOpsSyncStatus(c.EnvID(), resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to get gitops sync status")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("GitOps Sync Status")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Auto Sync", result.Data.AutoSync)
		if result.Data.NextSyncAt != nil {
			output.KeyValue("Next Sync", result.Data.NextSyncAt.Format("2006-01-02 15:04:05"))
		}
		if result.Data.LastSyncAt != nil {
			output.KeyValue("Last Sync", result.Data.LastSyncAt.Format("2006-01-02 15:04:05"))
		}
		if result.Data.LastSyncStatus != nil {
			output.KeyValue("Last Status", *result.Data.LastSyncStatus)
		}
		if result.Data.LastSyncError != nil {
			output.KeyValue("Last Error", *result.Data.LastSyncError)
		}
		if result.Data.LastSyncCommit != nil {
			output.KeyValue("Last Commit", *result.Data.LastSyncCommit)
		}
		return nil
	},
}

var syncCmd = &cobra.Command{
	Use:          "sync <sync-id|name>",
	Short:        "Trigger a GitOps sync",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		result, err := c.PostJSON[gitops.SyncResult](cmd.Context(), types.GitOpsSyncTrigger(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to trigger gitops sync")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		if result.Data.Success {
			output.Success("Sync triggered successfully: %s", result.Data.Message)
			return nil
		}

		errMsg := ""
		if result.Data.Error != nil {
			errMsg = *result.Data.Error
		}
		return errors.Errorf("sync failed: %s %s", result.Data.Message, errMsg)
	},
}

var filesCmd = &cobra.Command{
	Use:          "files <sync-id|name>",
	Short:        "List files from GitOps sync repository",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		filesPath := types.GitOpsSyncFiles(c.EnvID(), resolved.ID)
		if gitopsFilesPath != "" {
			filesPath = cmdutil.AppendQuery(filesPath, url.Values{"path": []string{gitopsFilesPath}})
		}

		result, err := c.GetJSON[gitops.BrowseResponse](cmd.Context(), filesPath)
		if err != nil {
			return errors.WrapIf(err, "failed to get gitops sync files")
		}

		files := result.Data.Files
		if jsonOutput {
			return cmdutil.PrintJSON(files)
		}

		headers := []string{"NAME", "TYPE", "PATH", "SIZE"}
		rows := flattenFileTree(files, 0)

		output.Table(headers, rows)
		return nil
	},
}

var importCmd = &cobra.Command{
	Use:          "import",
	Short:        "Import a GitOps sync configuration",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString("name")
		repo, _ := cmd.Flags().GetString("repo-url")
		branch, _ := cmd.Flags().GetString("branch")
		composePath, _ := cmd.Flags().GetString("compose-path")
		autoSync, _ := cmd.Flags().GetBool("auto-sync")
		interval, _ := cmd.Flags().GetInt("interval")

		req := gitops.ImportGitOpsSyncRequest{
			SyncName:          name,
			GitRepo:           repo,
			Branch:            branch,
			DockerComposePath: composePath,
			AutoSync:          autoSync,
			SyncInterval:      interval,
		}

		// The endpoint accepts a batch, so a single sync still has to be wrapped
		// in an array.
		result, err := c.PostJSON[gitops.ImportGitOpsSyncResponse](cmd.Context(), types.GitOpsSyncsImport(c.EnvID()), []gitops.ImportGitOpsSyncRequest{req})
		if err != nil {
			return errors.WrapIf(err, "failed to import gitops sync")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Import completed: %d succeeded, %d failed", result.Data.SuccessCount, result.Data.FailedCount)
		if len(result.Data.Errors) > 0 {
			output.Warning("Errors:")
			for _, e := range result.Data.Errors {
				fmt.Printf("  - %s\n", e)
			}
		}
		return nil
	},
}

func init() {
	GitopsCmd.AddCommand(listCmd)
	GitopsCmd.AddCommand(createCmd)
	GitopsCmd.AddCommand(getCmd)
	GitopsCmd.AddCommand(updateCmd)
	GitopsCmd.AddCommand(deleteCmd)
	GitopsCmd.AddCommand(statusCmd)
	GitopsCmd.AddCommand(syncCmd)
	GitopsCmd.AddCommand(filesCmd)
	GitopsCmd.AddCommand(importCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of syncs to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().String("name", "", "Name of the sync configuration")
	createCmd.Flags().String("repo-id", "", "Repository ID")
	createCmd.Flags().String("branch", "", "Branch to sync from")
	createCmd.Flags().String("compose-path", "", "Path to docker-compose file")
	createCmd.Flags().Bool("auto-sync", false, "Enable automatic sync")
	createCmd.Flags().Int("interval", 0, "Sync interval in minutes")
	createCmd.Flags().String("project-name", "", "Project name for the sync")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("repo-id")
	_ = createCmd.MarkFlagRequired("branch")
	_ = createCmd.MarkFlagRequired("compose-path")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Update command flags
	updateCmd.Flags().StringVar(&gitopsUpdateName, "name", "", "Name of the sync configuration")
	updateCmd.Flags().StringVar(&gitopsUpdateRepoID, "repo-id", "", "Repository ID")
	updateCmd.Flags().StringVar(&gitopsUpdateBranch, "branch", "", "Branch to sync from")
	updateCmd.Flags().StringVar(&gitopsUpdateComposePath, "compose-path", "", "Path to docker-compose file")
	updateCmd.Flags().StringVar(&gitopsUpdateProjectName, "project-name", "", "Project name for the sync")
	updateCmd.Flags().BoolVar(&gitopsUpdateAutoSync, "auto-sync", false, "Enable automatic sync")
	updateCmd.Flags().IntVar(&gitopsUpdateInterval, "interval", 0, "Sync interval in minutes")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force delete without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Status command flags
	statusCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Sync command flags
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Files command flags
	filesCmd.Flags().StringVar(&gitopsFilesPath, "path", "", "Path within the repository to browse")
	filesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Import command flags
	importCmd.Flags().String("name", "", "Name of the sync configuration")
	importCmd.Flags().String("repo-url", "", "Git repository URL")
	importCmd.Flags().String("branch", "", "Branch to sync from")
	importCmd.Flags().String("compose-path", "", "Path to docker-compose file")
	importCmd.Flags().Bool("auto-sync", false, "Enable automatic sync")
	importCmd.Flags().Int("interval", 5, "Sync interval in minutes")
	importCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = importCmd.MarkFlagRequired("name")
	_ = importCmd.MarkFlagRequired("repo-url")
	_ = importCmd.MarkFlagRequired("branch")
	_ = importCmd.MarkFlagRequired("compose-path")
}

var gitopsSyncRef = cmdutil.ResourceRef[gitops.GitOpsSync, gitops.GitOpsSync]{
	Singular: "gitops sync",
	Plural:   "gitops syncs",
	IDHint:   "the sync ID",
	ListCmd:  "arcane gitops list",
	GetPath:  types.GitOpsSync,
	ListPath: types.GitOpsSyncs,
	Matches:  gitOpsSyncMatches,
	Label: func(match gitops.GitOpsSync) string {
		lastStatus := "-"
		if match.LastSyncStatus != nil {
			lastStatus = *match.LastSyncStatus
		}
		return fmt.Sprintf("%s (%s, %s)", match.Name, match.ID, lastStatus)
	},
	Promote: func(match gitops.GitOpsSync) *gitops.GitOpsSync { return &match },
}

func gitOpsSyncMatches(item gitops.GitOpsSync, identifierLower, original string) bool {
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	if strings.Contains(idLower, identifierLower) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), identifierLower) {
		return true
	}
	if strings.EqualFold(item.Name, original) {
		return true
	}
	return false
}

func flattenFileTree(nodes []gitops.FileTreeNode, depth int) [][]string {
	var rows [][]string
	for _, node := range nodes {
		prefix := strings.Repeat("  ", depth)
		size := "-"
		if node.Type == gitops.FileTreeNodeTypeFile {
			size = output.Bytes(node.Size)
		}
		rows = append(rows, []string{
			prefix + node.Name,
			string(node.Type),
			node.Path,
			size,
		})
		if len(node.Children) > 0 {
			rows = append(rows, flattenFileTree(node.Children, depth+1)...)
		}
	}
	return rows
}
