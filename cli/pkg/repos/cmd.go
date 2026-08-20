package repos

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	startFlag  int
	allFlag    bool
	forceFlag  bool
	jsonOutput bool
)

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

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[gitops.GitRepository]{
			Resource: "repositories",
			Endpoint: types.GitRepositories(),
			Params:   cmdutil.ListParams{Resource: "repos", Limit: limitFlag, FallbackDefault: 20, Start: startFlag, All: allFlag},
			JSON:     jsonOutput,
			Headers:  []string{"ID", "NAME", "URL", "AUTH TYPE", "ENABLED", "CREATED"},
			Row: func(repo gitops.GitRepository) []string {
				enabled := "false"
				if repo.Enabled {
					enabled = "true"
				}
				return []string{
					repo.ID,
					repo.Name,
					repo.URL,
					repo.AuthType,
					enabled,
					repo.CreatedAt.Format("2006-01-02 15:04:05"),
				}
			},
		})
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
				return errors.WrapIf(err, "failed to read SSH key file")
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

		result, err := c.PostJSON[gitops.GitRepository](cmd.Context(), types.GitRepositories(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create repository")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

		resolved, _, err := repoRef.Resolve(cmd.Context(), c, args[0], prompt.IsInteractive())
		if err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
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

		resolved, _, err := repoRef.Resolve(cmd.Context(), c, args[0], prompt.IsInteractive())
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
				return errors.WrapIf(err, "failed to read SSH key file")
			}
			req.SSHKey = new(string(sshKeyData))
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

		result, err := c.PutJSON[gitops.GitRepository](cmd.Context(), types.GitRepository(resolved.ID), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update repository")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

		resolved, _, err := repoRef.Resolve(cmd.Context(), c, args[0], prompt.IsInteractive())
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

		resp, err := c.Delete(cmd.Context(), types.GitRepository(resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to delete repository")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete repository")
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

		resolved, _, err := repoRef.Resolve(cmd.Context(), c, args[0], prompt.IsInteractive())
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.GitRepositoryTest(resolved.ID),
			FailureMessage: "repository connection test failed",
			SuccessMessage: "Repository connection test successful",
			JSON:           jsonOutput,
		})
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

		resolved, _, err := repoRef.Resolve(cmd.Context(), c, args[0], prompt.IsInteractive())
		if err != nil {
			return err
		}

		result, err := c.GetJSON[gitops.BranchesResponse](cmd.Context(), types.GitRepositoryBranches(resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to list branches")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

		resolved, _, err := repoRef.Resolve(cmd.Context(), c, args[0], prompt.IsInteractive())
		if err != nil {
			return err
		}

		path := types.GitRepositoryFiles(resolved.ID)
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

		result, err := c.GetJSON[gitops.BrowseResponse](cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list files")
		}

		files := result.Data.Files
		if jsonOutput {
			return cmdutil.PrintJSON(files)
		}

		headers := []string{"NAME", "TYPE", "PATH", "SIZE"}
		rows := make([][]string, len(files))
		for i, node := range files {
			size := ""
			if node.Type == gitops.FileTreeNodeTypeFile {
				size = strconv.FormatInt(node.Size, 10)
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

var repoRef = cmdutil.ResourceRef[gitops.GitRepository, gitops.GitRepository]{
	Singular: "repository",
	Plural:   "repositories",
	IDHint:   "the repository ID",
	ListCmd:  "arcane repos list",
	GetPath:  func(_ string, identifier string) string { return types.GitRepository(identifier) },
	ListPath: func(string) string { return types.GitRepositories() },
	Matches:  repoMatches,
	Label: func(match gitops.GitRepository) string {
		return fmt.Sprintf("%s (%s)", match.Name, match.ID)
	},
	Promote: func(match gitops.GitRepository) *gitops.GitRepository { return &match },
	Exact: func(item gitops.GitRepository, _ string, original string) bool {
		return strings.EqualFold(item.Name, original) || strings.EqualFold(item.ID, original)
	},
}

func repoMatches(item gitops.GitRepository, identifierLower, original string) bool {
	if strings.EqualFold(item.Name, original) || strings.EqualFold(item.ID, original) {
		return true
	}
	if strings.HasPrefix(strings.ToLower(item.ID), identifierLower) {
		return true
	}
	return strings.Contains(strings.ToLower(item.Name), identifierLower)
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

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of repositories to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
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
	filesCmd.Flags().StringVar(&filesBranch, "branch", "", "Branch to browse (required)")
	_ = filesCmd.MarkFlagRequired("branch")
	filesCmd.Flags().StringVar(&filesPath, "path", "", "Path within repository")
	filesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Sync command flags
}
