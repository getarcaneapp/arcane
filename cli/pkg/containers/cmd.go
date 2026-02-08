package containers

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
	"github.com/getarcaneapp/arcane/types/container"
	"github.com/spf13/cobra"
)

var (
	containersLimit int
	containersAll   bool
	forceFlag       bool
	jsonOutput      bool
)

const maxPromptOptions = 20

// ContainersCmd is the parent command for container operations
var ContainersCmd = &cobra.Command{
	Use:     "containers",
	Aliases: []string{"container", "c"},
	Short:   "Manage containers",
}

var containersListCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Containers(c.EnvID())
		if containersLimit > 0 {
			path = fmt.Sprintf("%s?pageSize=%d", path, containersLimit)
		}
		if containersAll {
			separator := "?"
			if strings.Contains(path, "?") {
				separator = "&"
			}
			path = fmt.Sprintf("%s%sall=true", path, separator)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[container.Summary]
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

		headers := []string{"ID", "NAME", "IMAGE", "STATE", "STATUS"}
		rows := make([][]string, len(result.Data))
		for i, container := range result.Data {
			name := ""
			if len(container.Names) > 0 {
				name = strings.TrimPrefix(container.Names[0], "/")
			}
			rows[i] = []string{
				shortID(container.ID),
				name,
				container.Image,
				container.State,
				container.Status,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d containers\n", result.Pagination.TotalItems)
		return nil
	},
}

var containersGetCmd = &cobra.Command{
	Use:          "get <container-id|name>",
	Short:        "Get detailed container information",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, complete, err := resolveContainer(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			path := types.Endpoints.Container(c.EnvID(), resolved.ID)
			resp, err := c.Get(cmd.Context(), path)
			if err != nil {
				return fmt.Errorf("failed to get container: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result base.ApiResponse[container.Details]
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

		output.Header("Container Details")
		output.KeyValue("ID", resolved.ID)
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("Image", resolved.Image)
		output.KeyValue("State", fmt.Sprintf("%s (Running: %v)", resolved.State.Status, resolved.State.Running))
		output.KeyValue("Created", resolved.Created)
		return nil
	},
}

var containersStartCmd = &cobra.Command{
	Use:          "start <container-id|name>",
	Short:        "Start a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerStart(c.EnvID(), resolved.ID)
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s started successfully", containerDisplayName(resolved))
		return nil
	},
}

var containersStopCmd = &cobra.Command{
	Use:          "stop <container-id|name>",
	Short:        "Stop a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerStop(c.EnvID(), resolved.ID)
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s stopped successfully", containerDisplayName(resolved))
		return nil
	},
}

var containersRestartCmd = &cobra.Command{
	Use:          "restart <container-id|name>",
	Short:        "Restart a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerRestart(c.EnvID(), resolved.ID)
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to restart container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s restarted successfully", containerDisplayName(resolved))
		return nil
	},
}

var containersUpdateCmd = &cobra.Command{
	Use:          "update <container-id|name>",
	Short:        "Update a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		// Updating a container can take a long time as it pulls the image
		c.SetTimeout(30 * time.Minute)

		path := types.Endpoints.ContainerUpdate(c.EnvID(), resolved.ID)
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to update container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s updated successfully", containerDisplayName(resolved))
		return nil
	},
}

var containersDeleteCmd = &cobra.Command{
	Use:          "delete <container-id|name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		displayName := containerDisplayName(resolved)

		if !forceFlag {
			fmt.Printf("Are you sure you want to delete container %s? (y/N): ", displayName)
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

		path := types.Endpoints.Container(c.EnvID(), resolved.ID)
		resp, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to delete container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s deleted successfully", displayName)
		return nil
	},
}

var containersCountsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get container status counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainersCounts(c.EnvID())
		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to get container counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.StatusCounts]
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

		output.Header("Container Status Counts")
		output.KeyValue("Running", result.Data.RunningContainers)
		output.KeyValue("Stopped", result.Data.StoppedContainers)
		output.KeyValue("Total", result.Data.TotalContainers)
		return nil
	},
}

func init() {
	ContainersCmd.AddCommand(containersListCmd)
	ContainersCmd.AddCommand(containersGetCmd)
	ContainersCmd.AddCommand(containersStartCmd)
	ContainersCmd.AddCommand(containersStopCmd)
	ContainersCmd.AddCommand(containersRestartCmd)
	ContainersCmd.AddCommand(containersUpdateCmd)
	ContainersCmd.AddCommand(containersDeleteCmd)
	ContainersCmd.AddCommand(containersCountsCmd)

	// List command flags
	containersListCmd.Flags().IntVarP(&containersLimit, "limit", "n", 20, "Number of containers to show")
	containersListCmd.Flags().BoolVarP(&containersAll, "all", "a", false, "Show all containers (including stopped)")
	containersListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	containersDeleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")

	// Global JSON output flags
	containersGetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStartCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStopCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersRestartCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersDeleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersCountsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func containerDisplayName(details *container.Details) string {
	if details == nil {
		return ""
	}
	if strings.TrimSpace(details.Name) != "" {
		return details.Name
	}
	if details.ID != "" {
		return shortID(details.ID)
	}
	return ""
}

func resolveContainer(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*container.Details, bool, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, false, fmt.Errorf("container identifier is required")
	}

	details, complete, found, err := fetchContainerByIdentifier(ctx, c, trimmed)
	if err != nil {
		return nil, false, err
	}
	if found {
		return details, complete, nil
	}

	matches, err := searchContainerMatches(ctx, c, trimmed)
	if err != nil {
		return nil, false, err
	}

	selected, err := selectContainerMatch(matches, trimmed, allowPrompt)
	if err != nil {
		return nil, false, err
	}
	if selected != nil {
		return containerDetailsFromSummary(*selected), false, nil
	}

	identifierLower := strings.ToLower(trimmed)
	if looksLikeIDPrefix(identifierLower) {
		fallback, ok, err := fallbackContainerByIDPrefix(ctx, c, identifierLower)
		if err != nil {
			return nil, false, err
		}
		if ok {
			return fallback, false, nil
		}
	}

	return nil, false, fmt.Errorf("container %q not found; use the container ID or run `arcane containers list`", trimmed)
}

func fetchContainerByIdentifier(ctx context.Context, c *client.Client, identifier string) (*container.Details, bool, bool, error) {
	resp, err := c.Get(ctx, types.Endpoints.Container(c.EnvID(), identifier))
	if err != nil {
		return nil, false, false, fmt.Errorf("failed to resolve container %q: %w", identifier, err)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, false, false, fmt.Errorf("failed to read container response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[container.Details]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, false, false, fmt.Errorf("failed to parse container response: %w", err)
		}
		return &result.Data, true, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, false, false, fmt.Errorf("failed to resolve container %q (status %d): %s", identifier, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil, false, false, nil
}

func searchContainerMatches(ctx context.Context, c *client.Client, identifier string) ([]container.Summary, error) {
	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Containers(c.EnvID()), url.QueryEscape(identifier), 200)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, fmt.Errorf("failed to search containers: %w", err)
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read containers response: %w", err)
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to search containers (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result base.Paginated[container.Summary]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse containers response: %w", err)
	}

	identifierLower := strings.ToLower(identifier)
	matches := make([]container.Summary, 0)
	for _, item := range result.Data {
		if containerMatches(item, identifierLower, identifier) {
			matches = append(matches, item)
		}
	}

	return matches, nil
}

func selectContainerMatch(matches []container.Summary, identifier string, allowPrompt bool) (*container.Summary, error) {
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) == 0 {
		return nil, nil
	}

	if !allowPrompt {
		return nil, fmt.Errorf("multiple containers match %q; use the container ID or run `arcane containers list`", identifier)
	}
	if len(matches) > maxPromptOptions {
		return nil, fmt.Errorf("multiple containers match %q (%d results); refine your query or use the container ID", identifier, len(matches))
	}

	options := make([]string, 0, len(matches))
	for _, match := range matches {
		options = append(options, formatContainerOption(match))
	}
	choice, err := prompt.Select("container", options)
	if err != nil {
		return nil, err
	}
	return &matches[choice], nil
}

func containerSummaryName(summary container.Summary) string {
	if len(summary.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(summary.Names[0], "/")
}

func containerDetailsFromSummary(summary container.Summary) *container.Details {
	return &container.Details{ID: summary.ID, Name: containerSummaryName(summary)}
}

func fallbackContainerByIDPrefix(ctx context.Context, c *client.Client, identifierLower string) (*container.Details, bool, error) {
	fallbackPath := fmt.Sprintf("%s?limit=%d", types.Endpoints.Containers(c.EnvID()), 200)
	fallbackResp, err := c.Get(ctx, fallbackPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to search containers: %w", err)
	}
	fallbackBody, err := io.ReadAll(fallbackResp.Body)
	_ = fallbackResp.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read containers response: %w", err)
	}
	if fallbackResp.StatusCode < 200 || fallbackResp.StatusCode >= 300 {
		return nil, false, nil
	}

	var fallbackResult base.Paginated[container.Summary]
	if err := json.Unmarshal(fallbackBody, &fallbackResult); err != nil {
		return nil, false, fmt.Errorf("failed to parse containers response: %w", err)
	}
	for _, item := range fallbackResult.Data {
		if strings.HasPrefix(strings.ToLower(item.ID), identifierLower) {
			return containerDetailsFromSummary(item), true, nil
		}
	}

	return nil, false, nil
}

func containerMatches(item container.Summary, identifierLower, original string) bool {
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	if strings.Contains(idLower, identifierLower) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Image), identifierLower) {
		return true
	}
	for _, name := range item.Names {
		trimmedName := strings.TrimPrefix(name, "/")
		if strings.Contains(strings.ToLower(trimmedName), identifierLower) {
			return true
		}
		if strings.EqualFold(trimmedName, original) || strings.EqualFold(name, original) {
			return true
		}
	}
	return false
}

func formatContainerOption(item container.Summary) string {
	name := ""
	if len(item.Names) > 0 {
		name = strings.TrimPrefix(item.Names[0], "/")
	}
	if name == "" {
		name = shortID(item.ID)
	}
	image := item.Image
	if image == "" {
		image = "<unknown>"
	}
	state := item.State
	if state == "" {
		state = "unknown"
	}
	return fmt.Sprintf("%s (%s, %s)", name, shortID(item.ID), image+" / "+state)
}

func looksLikeIDPrefix(value string) bool {
	if len(value) < 4 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
