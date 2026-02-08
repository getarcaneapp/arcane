package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/project"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

const maxPromptOptions = 20

// ProjectsCmd is the parent command for project operations
var ProjectsCmd = &cobra.Command{
	Use:     "projects",
	Aliases: []string{"project", "proj", "p"},
	Short:   "Manage projects",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List projects",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Projects(c.EnvID())
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[project.Details]
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

		headers := []string{"ID", "NAME", "STATUS", "SERVICES", "RUNNING", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, proj := range result.Data {
			rows[i] = []string{
				proj.ID,
				proj.Name,
				proj.Status,
				fmt.Sprintf("%d", proj.ServiceCount),
				fmt.Sprintf("%d", proj.RunningCount),
				proj.CreatedAt,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d projects\n", result.Pagination.TotalItems)
		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:          "destroy <project-id|name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Destroy project and remove all containers",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveProject(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		if !forceFlag {
			display := resolved.Name
			if display == "" {
				display = resolved.ID
			}
			fmt.Printf("Are you sure you want to destroy project %s? This will remove all containers! (y/N): ", display)
			var response string
			if _, err := fmt.Scanln(&response); err != nil {
				fmt.Println("Cancelled")
				return nil
			}
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.Delete(cmd.Context(), types.Endpoints.ProjectDestroy(c.EnvID(), resolved.ID))
		if err != nil {
			return fmt.Errorf("failed to destroy project: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Project %s destroyed successfully", resolved.Name)
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <project-id|name>",
	Short:        "Get project details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, complete, err := resolveProject(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			resp, err := c.Get(cmd.Context(), types.Endpoints.Project(c.EnvID(), resolved.ID))
			if err != nil {
				return fmt.Errorf("failed to get project: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result base.ApiResponse[project.Details]
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

		output.Header("Project Details")
		output.KeyValue("ID", resolved.ID)
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("Status", resolved.Status)
		output.KeyValue("Services", resolved.ServiceCount)
		output.KeyValue("Running", resolved.RunningCount)
		return nil
	},
}

var upCmd = &cobra.Command{
	Use:          "up <project-id|name>",
	Short:        "Start project services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveProject(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ProjectUp(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to start project: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Project %s started successfully", resolved.Name)
		return nil
	},
}

var downCmd = &cobra.Command{
	Use:          "down <project-id|name>",
	Short:        "Stop project services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveProject(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ProjectDown(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to stop project: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Project %s stopped successfully", resolved.Name)
		return nil
	},
}

var restartCmd = &cobra.Command{
	Use:          "restart <project-id|name>",
	Short:        "Restart project services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveProject(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ProjectRestart(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to restart project: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Project %s restarted successfully", resolved.Name)
		return nil
	},
}

var redeployCmd = &cobra.Command{
	Use:          "redeploy <project-id|name>",
	Short:        "Redeploy project (pull images and restart)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveProject(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		// Redeploying can take a long time as it pulls images and restarts containers
		c.SetTimeout(30 * time.Minute)

		resp, err := c.Post(cmd.Context(), types.Endpoints.ProjectRedeploy(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to redeploy project: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Project %s redeployed successfully", resolved.Name)
		return nil
	},
}

var pullCmd = &cobra.Command{
	Use:          "pull <project-id|name>",
	Short:        "Pull latest images for project",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveProject(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		// Pulling images can take a long time
		c.SetTimeout(30 * time.Minute)

		resp, err := c.Post(cmd.Context(), types.Endpoints.ProjectPull(c.EnvID(), resolved.ID), nil)
		if err != nil {
			return fmt.Errorf("failed to pull images: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Images pulled successfully for project %s", resolved.Name)
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get project counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.ProjectsCounts(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get project counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[map[string]interface{}]
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

		output.Header("Project Counts")
		for k, v := range result.Data {
			output.KeyValue(k, v)
		}
		return nil
	},
}

func init() {
	ProjectsCmd.AddCommand(listCmd)
	ProjectsCmd.AddCommand(getCmd)
	ProjectsCmd.AddCommand(upCmd)
	ProjectsCmd.AddCommand(downCmd)
	ProjectsCmd.AddCommand(restartCmd)
	ProjectsCmd.AddCommand(redeployCmd)
	ProjectsCmd.AddCommand(pullCmd)
	ProjectsCmd.AddCommand(countsCmd)
	ProjectsCmd.AddCommand(destroyCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of projects to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Counts command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Destroy command flags
	destroyCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force destroy without confirmation")
	destroyCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func resolveProject(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*project.Details, bool, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, false, fmt.Errorf("project identifier is required")
	}

	resp, err := c.Get(ctx, types.Endpoints.Project(c.EnvID(), trimmed))
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve project %q: %w", trimmed, err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read project response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[project.Details]
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, false, fmt.Errorf("failed to parse project response: %w", err)
		}
		return &result.Data, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, false, fmt.Errorf("failed to resolve project %q (status %d): %s", trimmed, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	identifierLower := strings.ToLower(trimmed)

	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Projects(c.EnvID()), url.QueryEscape(trimmed), 200)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to search projects: %w", err)
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read projects response: %w", err)
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("failed to search projects (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result base.Paginated[project.Details]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, false, fmt.Errorf("failed to parse projects response: %w", err)
	}

	matches := make([]project.Details, 0)
	for _, proj := range result.Data {
		if projectMatches(proj, identifierLower, trimmed) {
			matches = append(matches, proj)
		}
	}

	if len(matches) == 1 {
		return &matches[0], false, nil
	}

	if len(matches) > 1 {
		if !allowPrompt {
			return nil, false, fmt.Errorf("multiple projects match %q; use the project ID or run `arcane projects list`", trimmed)
		}
		if len(matches) > maxPromptOptions {
			return nil, false, fmt.Errorf("multiple projects match %q (%d results); refine your query or use the project ID", trimmed, len(matches))
		}

		options := make([]string, 0, len(matches))
		for _, match := range matches {
			options = append(options, fmt.Sprintf("%s (%s, %s)", match.Name, match.ID, match.Status))
		}
		choice, err := prompt.Select("project", options)
		if err != nil {
			return nil, false, err
		}
		return &matches[choice], false, nil
	}

	return nil, false, fmt.Errorf("project %q not found; use the project ID or run `arcane projects list`", trimmed)
}

func projectMatches(item project.Details, identifierLower, original string) bool {
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
	if strings.Contains(strings.ToLower(item.Path), identifierLower) {
		return true
	}
	return false
}
