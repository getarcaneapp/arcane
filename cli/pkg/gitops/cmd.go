package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/gitops"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
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

const maxPromptOptions = 20

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

		path := types.Endpoints.GitOpsSyncs(c.EnvID())
		effectiveLimit := cmdutil.EffectiveLimit(cmd, "gitops-syncs", "limit", limitFlag, 20)
		if effectiveLimit > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, effectiveLimit)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list gitops syncs: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[gitops.GitOpsSync]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "NAME", "BRANCH", "AUTO-SYNC", "LAST STATUS", "LAST SYNC"}
		rows := make([][]string, len(result.Data))
		for i, sync := range result.Data {
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
			rows[i] = []string{
				sync.ID,
				sync.Name,
				sync.Branch,
				autoSync,
				lastStatus,
				lastSync,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d gitops syncs\n", result.Pagination.TotalItems)
		return nil
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

		resp, err := c.Post(cmd.Context(), types.Endpoints.GitOpsSyncs(c.EnvID()), req)
		if err != nil {
			return fmt.Errorf("failed to create gitops sync: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to create gitops sync: %w", err)
		}

		var result base.ApiResponse[gitops.GitOpsSync]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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
		resolved, complete, err := resolveGitOpsSync(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			resp, err := c.Get(cmd.Context(), types.Endpoints.GitOpsSync(c.EnvID(), resolved.ID))
			if err != nil {
				return fmt.Errorf("failed to get gitops sync: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result base.ApiResponse[gitops.GitOpsSync]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			resolved = &result.Data
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(resolved, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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

		resolved, _, err := resolveGitOpsSync(cmd.Context(), c, args[0], false)
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

		resp, err := c.Put(cmd.Context(), types.Endpoints.GitOpsSync(c.EnvID(), resolved.ID), req)
		if err != nil {
			return fmt.Errorf("failed to update gitops sync: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update gitops sync: %w", err)
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

		resolved, _, err := resolveGitOpsSync(cmd.Context(), c, args[0], false)
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.GitOpsSync(c.EnvID(), resolved.ID))
		if err != nil {
			return fmt.Errorf("failed to delete gitops sync: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete gitops sync: %w", err)
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

		resolved, _, err := resolveGitOpsSync(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.GitOpsSyncStatus(c.EnvID(), resolved.ID))
		if err != nil {
			return fmt.Errorf("failed to get gitops sync status: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[gitops.SyncStatus]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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

		resolved, _, err := resolveGitOpsSync(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.GitOpsSyncTrigger(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to trigger gitops sync: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[gitops.SyncResult]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		if result.Data.Success {
			output.Success("Sync triggered successfully: %s", result.Data.Message)
			return nil
		}

		errMsg := ""
		if result.Data.Error != nil {
			errMsg = *result.Data.Error
		}
		return fmt.Errorf("sync failed: %s %s", result.Data.Message, errMsg)
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

		resolved, _, err := resolveGitOpsSync(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.GitOpsSyncFiles(c.EnvID(), resolved.ID))
		if err != nil {
			return fmt.Errorf("failed to get gitops sync files: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[gitops.BrowseResponse]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		files := result.Data.Files
		if jsonOutput {
			resultBytes, err := json.MarshalIndent(files, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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

		resp, err := c.Post(cmd.Context(), types.Endpoints.GitOpsSyncsImport(c.EnvID()), req)
		if err != nil {
			return fmt.Errorf("failed to import gitops sync: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to import gitops sync: %w", err)
		}

		var result base.ApiResponse[gitops.ImportGitOpsSyncResponse]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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

func resolveGitOpsSync(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*gitops.GitOpsSync, bool, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, false, fmt.Errorf("gitops sync identifier is required")
	}

	resp, err := c.Get(ctx, types.Endpoints.GitOpsSync(c.EnvID(), trimmed))
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve gitops sync %q: %w", trimmed, err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read gitops sync response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[gitops.GitOpsSync]
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, false, fmt.Errorf("failed to parse gitops sync response: %w", err)
		}
		return &result.Data, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, false, fmt.Errorf("failed to resolve gitops sync %q (status %d): %s", trimmed, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	identifierLower := strings.ToLower(trimmed)

	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.GitOpsSyncs(c.EnvID()), url.QueryEscape(trimmed), 200)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to search gitops syncs: %w", err)
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read gitops syncs response: %w", err)
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("failed to search gitops syncs (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result base.Paginated[gitops.GitOpsSync]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, false, fmt.Errorf("failed to parse gitops syncs response: %w", err)
	}

	matches := make([]gitops.GitOpsSync, 0)
	for _, sync := range result.Data {
		if gitOpsSyncMatches(sync, identifierLower, trimmed) {
			matches = append(matches, sync)
		}
	}

	if len(matches) == 1 {
		return &matches[0], false, nil
	}

	if len(matches) > 1 {
		if !allowPrompt {
			return nil, false, fmt.Errorf("multiple gitops syncs match %q; use the sync ID or run `arcane gitops list`", trimmed)
		}
		if len(matches) > maxPromptOptions {
			return nil, false, fmt.Errorf("multiple gitops syncs match %q (%d results); refine your query or use the sync ID", trimmed, len(matches))
		}

		options := make([]string, 0, len(matches))
		for _, match := range matches {
			lastStatus := "-"
			if match.LastSyncStatus != nil {
				lastStatus = *match.LastSyncStatus
			}
			options = append(options, fmt.Sprintf("%s (%s, %s)", match.Name, match.ID, lastStatus))
		}
		choice, err := prompt.Select("gitops sync", options)
		if err != nil {
			return nil, false, err
		}
		return &matches[choice], false, nil
	}

	return nil, false, fmt.Errorf("gitops sync %q not found; use the sync ID or run `arcane gitops list`", trimmed)
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
			size = formatSize(node.Size)
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

func formatSize(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
