package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
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

	containerCreateFile       string
	containerCreateName       string
	containerCreateImage      string
	containerCreateEnv        []string
	containerCreatePort       []string
	containerCreateVolume     []string
	containerCreateLabel      []string
	containerCreateNetwork    []string
	containerCreateRestart    string
	containerCreateMemory     int64
	containerCreateCPUs       float64
	containerCreatePrivileged bool
	containerCreateHostname   string
	containerCreateUser       string
	containerCreateWorkdir    string
	containerCreateEntrypoint string
	containerCreateCmd        string
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
		effectiveLimit := cmdutil.EffectiveLimit(cmd, "containers", "limit", containersLimit, 20)
		if effectiveLimit > 0 {
			path = fmt.Sprintf("%s?pageSize=%d", path, effectiveLimit)
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

var containersRedeployCmd = &cobra.Command{
	Use:          "redeploy <container-id|name>",
	Short:        "Redeploy a container (pull image and recreate)",
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

		c.SetTimeout(30 * time.Minute)

		path := types.Endpoints.ContainerRedeploy(c.EnvID(), resolved.ID)
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to redeploy container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.Details]
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

		output.Success("Container %s redeployed successfully", containerDisplayName(resolved))
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
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete container %s?", displayName))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		path := types.Endpoints.Container(c.EnvID(), resolved.ID) + "?force=true"
		resp, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to delete container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[base.MessageResponse]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !result.Success {
			msg := result.Data.Message
			if msg == "" {
				msg = "unknown error"
			}
			return fmt.Errorf("failed to delete container: %s", msg)
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

var containersCreateCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a new container",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		c.SetTimeout(30 * time.Minute)

		var req container.Create

		// File mode: read base config from file
		if containerCreateFile != "" {
			data, err := os.ReadFile(containerCreateFile)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", containerCreateFile, err)
			}
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("failed to parse config file: %w", err)
			}
		}

		// Flag overrides
		if cmd.Flags().Changed("name") {
			req.Name = containerCreateName
		}
		if cmd.Flags().Changed("image") {
			req.Image = containerCreateImage
		}
		if cmd.Flags().Changed("env") {
			req.Env = containerCreateEnv
		}
		if cmd.Flags().Changed("port") {
			// Parse "HOST:CONTAINER" format into ports map
			req.Ports = make(map[string]string)
			for _, p := range containerCreatePort {
				parts := strings.SplitN(p, ":", 2)
				if len(parts) == 2 {
					req.Ports[parts[1]] = parts[0]
				}
			}
		}
		if cmd.Flags().Changed("volume") {
			req.Volumes = containerCreateVolume
		}
		if cmd.Flags().Changed("label") {
			req.Labels = make(map[string]string)
			for _, l := range containerCreateLabel {
				parts := strings.SplitN(l, "=", 2)
				if len(parts) == 2 {
					req.Labels[parts[0]] = parts[1]
				}
			}
		}
		if cmd.Flags().Changed("network") {
			req.Networks = containerCreateNetwork
		}
		if cmd.Flags().Changed("restart") {
			req.RestartPolicy = containerCreateRestart
		}
		if cmd.Flags().Changed("memory") {
			req.Memory = containerCreateMemory
		}
		if cmd.Flags().Changed("cpus") {
			req.CPUs = containerCreateCPUs
		}
		if cmd.Flags().Changed("privileged") {
			req.Privileged = containerCreatePrivileged
		}
		if cmd.Flags().Changed("hostname") {
			req.Hostname = containerCreateHostname
		}
		if cmd.Flags().Changed("user") {
			req.User = containerCreateUser
		}
		if cmd.Flags().Changed("workdir") {
			req.WorkingDir = containerCreateWorkdir
		}
		if cmd.Flags().Changed("entrypoint") {
			req.Entrypoint = []string{containerCreateEntrypoint}
		}
		if cmd.Flags().Changed("cmd") {
			req.Cmd = []string{containerCreateCmd}
		}

		// Validate required fields
		if req.Name == "" {
			return fmt.Errorf("--name is required")
		}
		if req.Image == "" {
			return fmt.Errorf("--image is required")
		}

		path := types.Endpoints.Containers(c.EnvID())
		resp, err := c.Post(cmd.Context(), path, req)
		if err != nil {
			return fmt.Errorf("failed to create container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to create container: %w", err)
		}

		var result base.ApiResponse[container.Created]
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

		output.Success("Container %s created successfully", result.Data.Name)
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Image", result.Data.Image)
		output.KeyValue("Status", result.Data.Status)
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
	ContainersCmd.AddCommand(containersRedeployCmd)
	ContainersCmd.AddCommand(containersDeleteCmd)
	ContainersCmd.AddCommand(containersCountsCmd)
	ContainersCmd.AddCommand(containersCreateCmd)

	// Create command flags
	containersCreateCmd.Flags().StringVarP(&containerCreateFile, "file", "f", "", "JSON config file for container creation")
	containersCreateCmd.Flags().StringVar(&containerCreateName, "name", "", "Container name")
	containersCreateCmd.Flags().StringVar(&containerCreateImage, "image", "", "Docker image")
	containersCreateCmd.Flags().StringArrayVarP(&containerCreateEnv, "env", "e", nil, "Environment variable (KEY=VALUE)")
	containersCreateCmd.Flags().StringArrayVarP(&containerCreatePort, "port", "p", nil, "Port mapping (HOST:CONTAINER)")
	containersCreateCmd.Flags().StringArrayVarP(&containerCreateVolume, "volume", "v", nil, "Volume mount (SRC:DST)")
	containersCreateCmd.Flags().StringArrayVarP(&containerCreateLabel, "label", "l", nil, "Label (KEY=VALUE)")
	containersCreateCmd.Flags().StringArrayVar(&containerCreateNetwork, "network", nil, "Networks to connect to")
	containersCreateCmd.Flags().StringVar(&containerCreateRestart, "restart", "", "Restart policy (no, always, unless-stopped, on-failure)")
	containersCreateCmd.Flags().Int64Var(&containerCreateMemory, "memory", 0, "Memory limit in bytes")
	containersCreateCmd.Flags().Float64Var(&containerCreateCPUs, "cpus", 0, "Number of CPUs")
	containersCreateCmd.Flags().BoolVar(&containerCreatePrivileged, "privileged", false, "Run in privileged mode")
	containersCreateCmd.Flags().StringVar(&containerCreateHostname, "hostname", "", "Container hostname")
	containersCreateCmd.Flags().StringVar(&containerCreateUser, "user", "", "User to run as")
	containersCreateCmd.Flags().StringVar(&containerCreateWorkdir, "workdir", "", "Working directory")
	containersCreateCmd.Flags().StringVar(&containerCreateEntrypoint, "entrypoint", "", "Entrypoint command")
	containersCreateCmd.Flags().StringVar(&containerCreateCmd, "cmd", "", "Command to run")
	containersCreateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

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
	containersRedeployCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
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
