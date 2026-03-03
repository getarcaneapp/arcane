package repos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

const maxPromptOptions = 20

// ReposCmd is the parent command for git repository operations.
var ReposCmd = &cobra.Command{
	Use:     "repos",
	Aliases: []string{"repo", "git-repositories", "git-repos"},
	Short:   "Manage git repositories",
}

// --- create flags ---
var (
	repoCreateName             string
	repoCreateURL              string
	repoCreateAuthType         string
	repoCreateToken            string
	repoCreateUsername         string
	repoCreateSSHKey           string
	repoCreateSSHHostKeyVerify string
	repoCreateDescription      string
	repoCreateEnabled          bool
)

// --- update flags ---
var (
	repoUpdateName             string
	repoUpdateURL              string
	repoUpdateAuthType         string
	repoUpdateToken            string
	repoUpdateUsername         string
	repoUpdateSSHKey           string
	repoUpdateSSHHostKeyVerify string
	repoUpdateDescription      string
	repoUpdateEnabled          bool
)

// --- files flags ---
var (
	filesBranch string
	filesPath   string
)

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List git repositories",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.GitRepositories()
		effectiveLimit := cmdutil.EffectiveLimit(cmd, "repos", "limit", limitFlag, 20)
		if effectiveLimit > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, effectiveLimit)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list repositories: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[gitops.GitRepository]
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

		headers := []string{"ID", "NAME", "URL", "AUTH TYPE", "ENABLED", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, repo := range result.Data {
			enabled := "false"
			if repo.Enabled {
				enabled = "true"
			}
			rows[i] = []string{
				repo.ID,
				repo.Name,
				repo.URL,
				repo.AuthType,
				enabled,
				repo.CreatedAt.Format("2006-01-02 15:04:05"),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d repositories\n", result.Pagination.TotalItems)
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a git repository",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		req := gitops.CreateRepositoryRequest{
			Name:     repoCreateName,
			URL:      repoCreateURL,
			AuthType: repoCreateAuthType,
		}

		if cmd.Flags().Changed("token") {
			req.Token = repoCreateToken
		}
		if cmd.Flags().Changed("username") {
			req.Username = repoCreateUsername
		}
		if cmd.Flags().Changed("ssh-key") {
			sshKeyData, err := os.ReadFile(repoCreateSSHKey)
			if err != nil {
				return fmt.Errorf("failed to read SSH key file: %w", err)
			}
			req.SSHKey = string(sshKeyData)
		}
		if cmd.Flags().Changed("ssh-host-key-verification") {
			req.SSHHostKeyVerification = repoCreateSSHHostKeyVerify
		}
		if cmd.Flags().Changed("description") {
			req.Description = &repoCreateDescription
		}
		if cmd.Flags().Changed("enabled") {
			req.Enabled = &repoCreateEnabled
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.GitRepositories(), req)
		if err != nil {
			return fmt.Errorf("failed to create repository: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to create repository: %w", err)
		}

		var result base.ApiResponse[gitops.GitRepository]
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

		output.Success("Repository created successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("URL", result.Data.URL)
		output.KeyValue("Auth Type", result.Data.AuthType)
		output.KeyValue("Enabled", result.Data.Enabled)
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <repository>",
	Short:        "Get git repository details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveGitRepository(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(resolved, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Repository Details")
		output.KeyValue("ID", resolved.ID)
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("URL", resolved.URL)
		output.KeyValue("Auth Type", resolved.AuthType)
		if resolved.Username != "" {
			output.KeyValue("Username", resolved.Username)
		}
		if resolved.SSHHostKeyVerification != "" {
			output.KeyValue("SSH Host Key Verification", resolved.SSHHostKeyVerification)
		}
		if resolved.Description != nil {
			output.KeyValue("Description", *resolved.Description)
		}
		output.KeyValue("Enabled", resolved.Enabled)
		output.KeyValue("Created", resolved.CreatedAt.Format("2006-01-02 15:04:05"))
		output.KeyValue("Updated", resolved.UpdatedAt.Format("2006-01-02 15:04:05"))
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <repository>",
	Short:        "Update a git repository",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveGitRepository(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}

		req := gitops.UpdateRepositoryRequest{}

		if cmd.Flags().Changed("name") {
			req.Name = &repoUpdateName
		}
		if cmd.Flags().Changed("url") {
			req.URL = &repoUpdateURL
		}
		if cmd.Flags().Changed("auth-type") {
			req.AuthType = &repoUpdateAuthType
		}
		if cmd.Flags().Changed("token") {
			req.Token = &repoUpdateToken
		}
		if cmd.Flags().Changed("username") {
			req.Username = &repoUpdateUsername
		}
		if cmd.Flags().Changed("ssh-key") {
			sshKeyData, err := os.ReadFile(repoUpdateSSHKey)
			if err != nil {
				return fmt.Errorf("failed to read SSH key file: %w", err)
			}
			sshKey := string(sshKeyData)
			req.SSHKey = &sshKey
		}
		if cmd.Flags().Changed("ssh-host-key-verification") {
			req.SSHHostKeyVerification = &repoUpdateSSHHostKeyVerify
		}
		if cmd.Flags().Changed("description") {
			req.Description = &repoUpdateDescription
		}
		if cmd.Flags().Changed("enabled") {
			req.Enabled = &repoUpdateEnabled
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.GitRepository(resolved.ID), req)
		if err != nil {
			return fmt.Errorf("failed to update repository: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update repository: %w", err)
		}

		var result base.ApiResponse[gitops.GitRepository]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			if jsonOutput {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			output.Success("Repository updated successfully")
			return nil
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Repository updated successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("URL", result.Data.URL)
		output.KeyValue("Auth Type", result.Data.AuthType)
		output.KeyValue("Enabled", result.Data.Enabled)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <repository>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a git repository",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveGitRepository(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete repository %s (%s)?", resolved.Name, resolved.ID))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.Delete(cmd.Context(), types.Endpoints.GitRepository(resolved.ID))
		if err != nil {
			return fmt.Errorf("failed to delete repository: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete repository: %w", err)
		}

		output.Success("Repository deleted successfully")
		return nil
	},
}

var testCmd = &cobra.Command{
	Use:          "test <repository>",
	Short:        "Test git repository connection",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveGitRepository(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.GitRepositoryTest(resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to test repository: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("repository connection test failed: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Repository connection test successful")
		return nil
	},
}

var branchesCmd = &cobra.Command{
	Use:          "branches <repository>",
	Short:        "List branches for a git repository",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveGitRepository(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.GitRepositoryBranches(resolved.ID))
		if err != nil {
			return fmt.Errorf("failed to list branches: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[gitops.BranchesResponse]
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

		headers := []string{"BRANCH", "DEFAULT"}
		rows := make([][]string, len(result.Data.Branches))
		for i, branch := range result.Data.Branches {
			isDefault := ""
			if branch.IsDefault {
				isDefault = "*"
			}
			rows[i] = []string{
				branch.Name,
				isDefault,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d branches\n", len(result.Data.Branches))
		return nil
	},
}

var filesCmd = &cobra.Command{
	Use:          "files <repository>",
	Short:        "List files in a git repository",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveGitRepository(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}

		path := types.Endpoints.GitRepositoryFiles(resolved.ID)
		params := url.Values{}
		if filesBranch != "" {
			params.Set("branch", filesBranch)
		}
		if filesPath != "" {
			params.Set("path", filesPath)
		}
		if len(params) > 0 {
			path = path + "?" + params.Encode()
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list files: %w", err)
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
		rows := make([][]string, len(files))
		for i, node := range files {
			size := ""
			if node.Type == gitops.FileTreeNodeTypeFile {
				size = fmt.Sprintf("%d", node.Size)
			}
			rows[i] = []string{
				node.Name,
				string(node.Type),
				node.Path,
				size,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d entries\n", len(files))
		return nil
	},
}

var syncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "Sync all git repositories",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.GitRepositoriesSync(), nil)
		if err != nil {
			return fmt.Errorf("failed to sync repositories: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to sync repositories: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Repositories synced successfully")
		return nil
	},
}

// resolveGitRepository attempts to resolve a repository by ID or name.
// It first tries a direct GET by ID. If that returns 404, it falls back to
// searching the list endpoint by name or ID prefix.
func resolveGitRepository(ctx context.Context, c *client.Client, identifier string) (*gitops.GitRepository, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, fmt.Errorf("repository identifier is required")
	}

	// Try direct GET by ID.
	resp, err := c.Get(ctx, types.Endpoints.GitRepository(trimmed))
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			var result base.ApiResponse[gitops.GitRepository]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				return &result.Data, nil
			}
		}
		// Drain body on non-OK response.
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	// Fallback: search via list endpoint.
	listPath := fmt.Sprintf("%s?limit=200", types.Endpoints.GitRepositories())
	listResp, err := c.Get(ctx, listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to search repositories: %w", err)
	}
	defer func() { _ = listResp.Body.Close() }()

	var listResult base.Paginated[gitops.GitRepository]
	if err := json.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		return nil, fmt.Errorf("failed to parse repository list: %w", err)
	}

	lowerIdentifier := strings.ToLower(trimmed)
	var matches []gitops.GitRepository
	for _, repo := range listResult.Data {
		if strings.EqualFold(repo.Name, trimmed) || strings.EqualFold(repo.ID, trimmed) {
			return &repo, nil
		}
		if strings.HasPrefix(strings.ToLower(repo.ID), lowerIdentifier) ||
			strings.Contains(strings.ToLower(repo.Name), lowerIdentifier) {
			matches = append(matches, repo)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("repository %q not found", trimmed)
	case 1:
		return &matches[0], nil
	default:
		if !prompt.IsInteractive() || len(matches) > maxPromptOptions {
			return nil, fmt.Errorf("ambiguous repository %q: %d matches found, please be more specific", trimmed, len(matches))
		}
		options := make([]string, len(matches))
		for i, m := range matches {
			options[i] = fmt.Sprintf("%s (%s)", m.Name, m.ID)
		}
		choice, err := prompt.Select("repository", options)
		if err != nil {
			return nil, err
		}
		return &matches[choice], nil
	}
}

func init() {
	ReposCmd.AddCommand(listCmd)
	ReposCmd.AddCommand(createCmd)
	ReposCmd.AddCommand(getCmd)
	ReposCmd.AddCommand(updateCmd)
	ReposCmd.AddCommand(deleteCmd)
	ReposCmd.AddCommand(testCmd)
	ReposCmd.AddCommand(branchesCmd)
	ReposCmd.AddCommand(filesCmd)
	ReposCmd.AddCommand(syncCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of repositories to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringVar(&repoCreateName, "name", "", "Repository name")
	createCmd.Flags().StringVar(&repoCreateURL, "url", "", "Repository URL")
	createCmd.Flags().StringVar(&repoCreateAuthType, "auth-type", "", "Authentication type (none, http, ssh)")
	createCmd.Flags().StringVar(&repoCreateToken, "token", "", "Token for HTTP authentication")
	createCmd.Flags().StringVar(&repoCreateUsername, "username", "", "Username for HTTP authentication")
	createCmd.Flags().StringVar(&repoCreateSSHKey, "ssh-key", "", "Path to SSH key file")
	createCmd.Flags().StringVar(&repoCreateSSHHostKeyVerify, "ssh-host-key-verification", "", "SSH host key verification (strict, accept_new, skip)")
	createCmd.Flags().StringVar(&repoCreateDescription, "description", "", "Repository description")
	createCmd.Flags().BoolVar(&repoCreateEnabled, "enabled", true, "Enable the repository")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("url")
	_ = createCmd.MarkFlagRequired("auth-type")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Update command flags
	updateCmd.Flags().StringVar(&repoUpdateName, "name", "", "Repository name")
	updateCmd.Flags().StringVar(&repoUpdateURL, "url", "", "Repository URL")
	updateCmd.Flags().StringVar(&repoUpdateAuthType, "auth-type", "", "Authentication type (none, http, ssh)")
	updateCmd.Flags().StringVar(&repoUpdateToken, "token", "", "Token for HTTP authentication")
	updateCmd.Flags().StringVar(&repoUpdateUsername, "username", "", "Username for HTTP authentication")
	updateCmd.Flags().StringVar(&repoUpdateSSHKey, "ssh-key", "", "Path to SSH key file")
	updateCmd.Flags().StringVar(&repoUpdateSSHHostKeyVerify, "ssh-host-key-verification", "", "SSH host key verification (strict, accept_new, skip)")
	updateCmd.Flags().StringVar(&repoUpdateDescription, "description", "", "Repository description")
	updateCmd.Flags().BoolVar(&repoUpdateEnabled, "enabled", true, "Enable the repository")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Test command flags
	testCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Branches command flags
	branchesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Files command flags
	filesCmd.Flags().StringVar(&filesBranch, "branch", "", "Branch to browse")
	filesCmd.Flags().StringVar(&filesPath, "path", "", "Path within repository")
	filesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Sync command flags
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
