package gitops

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"kit.getarcane.app"

	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/cli/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

var (
	gitopsSyncRef = cmdutil.ResourceRef[types.GitOpsSync, types.GitOpsSync]{
		EnvironmentScoped: true,
		Singular:          "gitops sync",
		Plural:            "gitops syncs",
		IDHint:            "the sync ID",
		ListCmd:           "arcane gitops list",
		GetPath:           clitypes.GitOpsSync,
		IdentifierOf:      func(item types.GitOpsSync) string { return item.ID },
		ListPath:          clitypes.GitOpsSyncs,
		Matches: func(item types.GitOpsSync, identifierLower, original string) bool {
			return strings.Contains(strings.ToLower(item.ID), identifierLower) ||
				strings.Contains(strings.ToLower(item.Name), identifierLower) || strings.EqualFold(item.Name, original)
		},
		Label: func(match types.GitOpsSync) string {
			return fmt.Sprintf("%s (%s, %s)", match.Name, match.ID, cmp.Or(kit.FromPtr(match.LastSyncStatus), "-"))
		},
		Promote: func(match types.GitOpsSync) *types.GitOpsSync { return &match },
	}

	syncModes = []string{types.GitOpsSyncModeDeploy, types.GitOpsSyncModeBackup, types.GitOpsSyncModeBidirectional}
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gitops",
		Aliases: []string{"gitops-syncs", "gs"},
		Short:   "Manage GitOps syncs",
	}
	cmdutil.SetScope(cmdutil.ScopeResource, cmd)
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newFilesCmd())
	cmd.AddCommand(newImportCmd())
	cmd.AddCommand(newPreviewCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newResolveCmd())
	return cmd
}

func newListCmd() *cobra.Command {
	var opts struct {
		mode  string
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List GitOps syncs",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.GitOpsSync]{
				Query:    kit.Ternary(opts.mode != "", url.Values{"mode": []string{opts.mode}}, nil),
				Resource: "gitops syncs",
				Endpoint: clitypes.GitOpsSyncs(c.EnvID()),
				Params:   cmdutil.ListParams{Resource: "gitops-syncs", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "MODE", "BRANCH", "AUTO-SYNC", "LAST STATUS", "LAST SYNC"},
				Row: func(sync types.GitOpsSync) []string {
					return []string{
						sync.ID,
						sync.Name,
						cmp.Or(sync.Mode, types.GitOpsSyncModeDeploy),
						sync.Branch,
						strconv.FormatBool(sync.AutoSync),
						cmp.Or(kit.FromPtr(sync.LastSyncStatus), "-"),
						kit.Ternary(sync.LastSyncAt != nil, kit.FromPtr(sync.LastSyncAt).Format("2006-01-02 15:04:05"), "-"),
					}
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.mode, "mode", "", "", "Only show syncs with this mode", syncModes...)
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of syncs to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return cmd
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name            string
		mode            string
		repoID          string
		branch          string
		composePath     string
		projectName     string
		project         string
		backupDirectory string
		backupPaths     []string
		backupOnSave    bool
		autoSync        bool
		interval        int
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new GitOps sync",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			req := map[string]any{
				"name":         opts.name,
				"mode":         opts.mode,
				"repositoryId": opts.repoID,
				"branch":       opts.branch,
				"autoSync":     opts.autoSync,
			}
			if opts.project != "" {
				resolved, _, resolveErr := projects.ProjectRef.Resolve(cmd.Context(), c, opts.project, cmdutil.AllowPrompt(cmd))
				if resolveErr != nil {
					return resolveErr
				}
				req["projectId"] = resolved.ID
			}
			for _, field := range []struct {
				value any
				flag  string
				key   string
			}{
				{flag: "compose-path", key: "composePath", value: opts.composePath},
				{flag: "project-name", key: "projectName", value: opts.projectName},
				{flag: "interval", key: "syncInterval", value: opts.interval},
				{flag: "backup-directory", key: "backupDirectory", value: opts.backupDirectory},
				{flag: "backup-path", key: "backupPaths", value: opts.backupPaths},
				{flag: "backup-on-save", key: "backupOnSave", value: opts.backupOnSave},
			} {
				if cmd.Flags().Changed(field.flag) {
					req[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsSync]{
				Method:         http.MethodPost,
				Path:           clitypes.GitOpsSyncs(c.EnvID()),
				Body:           req,
				FailureMessage: "failed to create gitops sync",
				Render: func(data types.GitOpsSync) error {
					output.Success("GitOps sync %s created successfully (ID: %s)", data.Name, data.ID)
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	cmd.Flags().StringVar(&opts.name, "name", "", "Name of the sync configuration")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.mode, "mode", "", types.GitOpsSyncModeDeploy, "Sync direction: pull from Git, push to Git, or both", syncModes...)
	cmd.Flags().StringVar(&opts.repoID, "repo-id", "", "Repository ID")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Branch to sync from")
	cmd.Flags().StringVar(&opts.composePath, "compose-path", "", "Path to docker-compose file (required for deploy mode)")
	cmd.Flags().BoolVar(&opts.autoSync, "auto-sync", false, "Enable automatic sync")
	cmd.Flags().IntVar(&opts.interval, "interval", 0, "Sync interval in minutes")
	cmd.Flags().StringVar(&opts.projectName, "project-name", "", "Project name for the sync")
	cmd.Flags().StringVar(&opts.project, "project", "", "Existing project ID or name to link (required for backup and bidirectional modes)")
	cmd.Flags().StringVar(&opts.backupDirectory, "backup-directory", "", "Repository directory that project files are pushed to (backup and bidirectional modes)")
	cmd.Flags().StringArrayVar(&opts.backupPaths, "backup-path", nil, "Extra project path to push besides the compose files (repeatable; backup and bidirectional modes)")
	cmd.Flags().BoolVar(&opts.backupOnSave, "backup-on-save", true, "Push to Git whenever project files are saved in Arcane (backup and bidirectional modes)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("repo-id")
	_ = cmd.MarkFlagRequired("branch")
	return cmd
}

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get <sync-id|name>",
		Short:        "Get GitOps sync details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, err := gitopsSyncRef.Fetch(cmd.Context(), c, args[0], cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(resolved)
			}

			output.Header("GitOps Sync Details")
			output.KeyValue("ID", resolved.ID)
			output.KeyValue("Name", resolved.Name)
			output.KeyValue("Mode", cmp.Or(resolved.Mode, types.GitOpsSyncModeDeploy))
			output.KeyValue("Branch", resolved.Branch)
			output.KeyValue("Compose Path", resolved.ComposePath)
			output.KeyValue("Project Name", resolved.ProjectName)
			if resolved.Mode == types.GitOpsSyncModeBackup || resolved.Mode == types.GitOpsSyncModeBidirectional {
				output.KeyValue("Backup Directory", resolved.BackupDirectory)
				output.KeyValue("Backup Paths", cmp.Or(strings.Join(resolved.BackupPaths, ", "), "-"))
				output.KeyValue("Backup On Save", resolved.BackupOnSave)
				output.KeyValue("Backup State", cmp.Or(resolved.BackupState, "-"))
			}
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
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name         string
		repoID       string
		branch       string
		composePath  string
		projectName  string
		backupPaths  []string
		backupOnSave bool
		autoSync     bool
		interval     int
	}
	cmd := &cobra.Command{
		Use:          "update <sync-id|name>",
		Short:        "Update a GitOps sync",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			req := map[string]any{}
			for _, field := range []struct {
				value any
				flag  string
				key   string
			}{
				{flag: "name", key: "name", value: opts.name},
				{flag: "repo-id", key: "repositoryId", value: opts.repoID},
				{flag: "branch", key: "branch", value: opts.branch},
				{flag: "compose-path", key: "composePath", value: opts.composePath},
				{flag: "project-name", key: "projectName", value: opts.projectName},
				{flag: "auto-sync", key: "autoSync", value: opts.autoSync},
				{flag: "interval", key: "syncInterval", value: opts.interval},
				{flag: "backup-path", key: "backupPaths", value: opts.backupPaths},
				{flag: "backup-on-save", key: "backupOnSave", value: opts.backupOnSave},
			} {
				if cmd.Flags().Changed(field.flag) {
					req[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsSync]{
				Method:         http.MethodPatch,
				Path:           clitypes.GitOpsSync(c.EnvID(), resolved.ID),
				Body:           req,
				FailureMessage: "failed to update gitops sync",
				Render: func(types.GitOpsSync) error {
					output.Success("GitOps sync %s updated successfully", resolved.Name)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "Name of the sync configuration")
	cmd.Flags().StringVar(&opts.repoID, "repo-id", "", "Repository ID")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Branch to sync from")
	cmd.Flags().StringVar(&opts.composePath, "compose-path", "", "Path to docker-compose file")
	cmd.Flags().StringVar(&opts.projectName, "project-name", "", "Project name for the sync")
	cmd.Flags().BoolVar(&opts.autoSync, "auto-sync", false, "Enable automatic sync")
	cmd.Flags().IntVar(&opts.interval, "interval", 0, "Sync interval in minutes")
	cmd.Flags().StringArrayVar(&opts.backupPaths, "backup-path", nil, "Extra project path to push besides the compose files; replaces the current list (repeatable; backup and bidirectional modes)")
	cmd.Flags().BoolVar(&opts.backupOnSave, "backup-on-save", false, "Push to Git whenever project files are saved in Arcane (backup and bidirectional modes)")
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <sync-id|name>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a GitOps sync",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete gitops sync %s?", cmp.Or(resolved.Name, resolved.ID)),
				Path:           clitypes.GitOpsSync(c.EnvID(), resolved.ID),
				Body:           nil,
				FailureMessage: "failed to delete gitops sync",
				SuccessMessage: fmt.Sprintf("GitOps sync %s deleted successfully", resolved.Name),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force delete without confirmation")
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "status <sync-id|name>",
		Short:        "Get GitOps sync status",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsSyncStatus]{
				Method:         http.MethodGet,
				Path:           clitypes.GitOpsSyncStatus(c.EnvID(), resolved.ID),
				FailureMessage: "failed to get gitops sync status",
				Render: func(data types.GitOpsSyncStatus) error {
					output.Header("GitOps Sync Status")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Auto Sync", data.AutoSync)
					if data.NextSyncAt != nil {
						output.KeyValue("Next Sync", data.NextSyncAt.Format("2006-01-02 15:04:05"))
					}
					if data.LastSyncAt != nil {
						output.KeyValue("Last Sync", data.LastSyncAt.Format("2006-01-02 15:04:05"))
					}
					if data.LastSyncStatus != nil {
						output.KeyValue("Last Status", *data.LastSyncStatus)
					}
					if data.LastSyncError != nil {
						output.KeyValue("Last Error", *data.LastSyncError)
					}
					if data.LastSyncCommit != nil {
						output.KeyValue("Last Commit", *data.LastSyncCommit)
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sync <sync-id|name>",
		Short:        "Trigger a GitOps sync",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsSyncResult]{
				Method:         http.MethodPost,
				Path:           clitypes.GitOpsSyncAction(c.EnvID(), resolved.ID, "sync"),
				Body:           nil,
				FailureMessage: "failed to trigger gitops sync",
				Render: func(data types.GitOpsSyncResult) error {
					if data.Success {
						output.Success("Sync triggered successfully: %s", data.Message)
						return nil
					}
					return fmt.Errorf("sync failed: %s %s", data.Message, kit.FromPtr(data.Error))
				},
			})
		},
	}
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newFilesCmd() *cobra.Command {
	var opts struct {
		path string
	}
	cmd := &cobra.Command{
		Use:          "files <sync-id|name>",
		Short:        "List files from GitOps sync repository",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			filesPath := clitypes.GitOpsSyncFiles(c.EnvID(), resolved.ID)
			if opts.path != "" {
				filesPath = cmdutil.AppendQuery(filesPath, url.Values{"path": []string{opts.path}})
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsBrowseResponse]{
				Method:         http.MethodGet,
				Path:           filesPath,
				FailureMessage: "failed to get gitops sync files",
				Render: func(data types.GitOpsBrowseResponse) error {
					headers := []string{"NAME", "TYPE", "PATH", "SIZE"}
					return output.Table(headers, flattenFileTree(data.Files, 0))
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.path, "path", "", "Path within the repository to browse")
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newImportCmd() *cobra.Command {
	var opts struct {
		name        string
		repoURL     string
		branch      string
		composePath string
		projectName string
		interval    int
		autoSync    bool
	}
	cmd := &cobra.Command{
		Use:          "import",
		Short:        "Import a GitOps sync configuration",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			req := types.ImportGitOpsSyncRequest{
				SyncName:          opts.name,
				GitRepo:           opts.repoURL,
				Branch:            opts.branch,
				DockerComposePath: opts.composePath,
				AutoSync:          opts.autoSync,
				SyncInterval:      opts.interval,
				ProjectName:       opts.projectName,
			}

			// The endpoint accepts a batch, so a single sync still has to be wrapped
			// in an array.
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ImportGitOpsSyncResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.GitOpsSyncsImport(c.EnvID()),
				Body:           []types.ImportGitOpsSyncRequest{req},
				FailureMessage: "failed to import gitops sync",
				Render: func(data types.ImportGitOpsSyncResponse) error {
					output.Success("Import completed: %d succeeded, %d failed", data.SuccessCount, data.FailedCount)
					if len(data.Errors) > 0 {
						output.Warning("Errors:")
						for _, e := range data.Errors {
							_, _ = fmt.Fprintf(output.Stdout(), "  - %s\n", e)
						}
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	cmd.Flags().StringVar(&opts.name, "name", "", "Name of the sync configuration")
	cmd.Flags().StringVar(&opts.repoURL, "repo-url", "", "Git repository URL")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Branch to sync from")
	cmd.Flags().StringVar(&opts.composePath, "compose-path", "", "Path to docker-compose file")
	cmd.Flags().BoolVar(&opts.autoSync, "auto-sync", false, "Enable automatic sync")
	cmd.Flags().IntVar(&opts.interval, "interval", 5, "Sync interval in minutes")
	cmd.Flags().StringVar(&opts.projectName, "project-name", "", "Project name for the sync")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("repo-url")
	_ = cmd.MarkFlagRequired("branch")
	_ = cmd.MarkFlagRequired("compose-path")
	return cmd
}

func flattenFileTree(nodes []types.FileTreeNode, depth int) [][]string {
	var rows [][]string
	for _, node := range nodes {
		rows = append(rows, []string{
			strings.Repeat("  ", depth) + node.Name,
			string(node.Type),
			node.Path,
			kit.Ternary(node.Type == types.FileTreeNodeTypeFile, output.Bytes(node.Size), "-"),
		})
		if len(node.Children) > 0 {
			rows = append(rows, flattenFileTree(node.Children, depth+1)...)
		}
	}
	return rows
}
