package containers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/spf13/cobra"
)

var (
	containersLimit           int
	containersStart           int
	containersAll             bool
	containersUpdatesFilter   string
	containersStandalone      string
	containersIncludeInternal bool
	containersDeleteVolumes   bool
	forceFlag                 bool
	jsonOutput                bool

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
		return runContainersList(cmd, false)
	},
}

var containersUpdatesCmd = &cobra.Command{
	Use:          "updates",
	Short:        "List containers with available updates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runContainersList(cmd, true)
	},
}

func runContainersList(cmd *cobra.Command, forceHasUpdateFilter bool) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	path, err := buildContainersListPath(cmd, c, forceHasUpdateFilter)
	if err != nil {
		return err
	}
	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return errors.WrapIf(err, "failed to list containers")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := cmdutil.ReadJSONBody(resp)
	if err != nil {
		return errors.WrapIf(err, "failed to list containers")
	}

	if jsonOutput {
		return cmdutil.PrintRawJSON(body)
	}

	var result base.Paginated[container.Summary]
	if err := json.Unmarshal(body, &result); err != nil {
		return errors.WrapIf(err, "failed to parse response")
	}

	effectiveUpdatesFilter := strings.TrimSpace(containersUpdatesFilter)
	if forceHasUpdateFilter {
		effectiveUpdatesFilter = "has_update"
	}
	if effectiveUpdatesFilter != "" {
		output.Header("Container Updates")
		headers := []string{"ID", "NAME", "IMAGE", "STATE", "UPDATES", "LATEST"}
		rows := make([][]string, len(result.Data))
		for i, item := range result.Data {
			rows[i] = []string{
				shortID(item.ID),
				containerSummaryName(item),
				item.Image,
				item.State,
				containerUpdateStatus(item),
				containerUpdateLatest(item),
			}
		}
		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "containers")
		return nil
	}

	headers := []string{"ID", "NAME", "IMAGE", "STATE", "STATUS"}
	rows := make([][]string, len(result.Data))
	for i, item := range result.Data {
		rows[i] = []string{
			shortID(item.ID),
			containerSummaryName(item),
			item.Image,
			item.State,
			item.Status,
		}
	}

	output.Table(headers, rows)
	output.Showing(len(result.Data), result.Pagination.TotalItems, "containers")
	return nil
}

func buildContainersListPath(cmd *cobra.Command, c *client.Client, forceHasUpdateFilter bool) (string, error) {
	path, err := cmdutil.ApplyPaginationParams(cmd, types.Endpoints.Containers(c.EnvID()), cmdutil.ListParams{
		Resource:        "containers",
		Limit:           containersLimit,
		FallbackDefault: 20,
		Start:           containersStart,
		All:             containersAll,
	})
	if err != nil {
		return "", errors.WrapIf(err, "failed to build pagination query")
	}

	parsed, err := url.Parse(path)
	if err != nil {
		return "", errors.WrapIf(err, "failed to parse path")
	}

	query := parsed.Query()
	if containersStandalone != "" {
		query.Set("standalone", containersStandalone)
	}
	if containersIncludeInternal {
		query.Set("includeInternal", "true")
	}
	updatesFilter := strings.TrimSpace(containersUpdatesFilter)
	if forceHasUpdateFilter {
		updatesFilter = "has_update"
	}
	if updatesFilter != "" {
		query.Set("updates", updatesFilter)
	}

	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func containerUpdateStatus(item container.Summary) string {
	switch {
	case item.UpdateInfo == nil:
		return "unknown"
	case item.UpdateInfo.Error != "":
		return "error"
	case item.UpdateInfo.HasUpdate:
		return "has_update"
	default:
		return "up_to_date"
	}
}

func containerUpdateLatest(item container.Summary) string {
	if item.UpdateInfo == nil {
		return "-"
	}
	if strings.TrimSpace(item.UpdateInfo.LatestVersion) != "" {
		return item.UpdateInfo.LatestVersion
	}
	if strings.TrimSpace(item.UpdateInfo.LatestDigest) != "" {
		return item.UpdateInfo.LatestDigest
	}
	return "-"
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
		resolved, complete, err := containerRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			result, err := c.GetJSON[container.Details](cmd.Context(), types.Endpoints.Container(c.EnvID(), resolved.ID))
			if err != nil {
				return errors.WrapIf(err, "failed to get container")
			}
			resolved = &result.Data
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
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

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ContainerStart(c.EnvID(), resolved.ID),
			FailureMessage: "failed to start container",
			SuccessMessage: fmt.Sprintf("Container %s started successfully", containerDisplayName(resolved)),
			JSON:           jsonOutput,
		})
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

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ContainerStop(c.EnvID(), resolved.ID),
			FailureMessage: "failed to stop container",
			SuccessMessage: fmt.Sprintf("Container %s stopped successfully", containerDisplayName(resolved)),
			JSON:           jsonOutput,
		})
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

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ContainerRestart(c.EnvID(), resolved.ID),
			FailureMessage: "failed to restart container",
			SuccessMessage: fmt.Sprintf("Container %s restarted successfully", containerDisplayName(resolved)),
			JSON:           jsonOutput,
		})
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

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		// This route is served by the updater handler, so it answers with an
		// updater.Result rather than a container.ActionResult.
		return cmdutil.RunPostAction[updater.Result](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ContainerUpdate(c.EnvID(), resolved.ID),
			FailureMessage: "failed to update container",
			SuccessMessage: fmt.Sprintf("Container %s updated successfully", containerDisplayName(resolved)),
			Timeout:        30 * time.Minute,
			JSON:           jsonOutput,
		})
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

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[container.Details](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ContainerRedeploy(c.EnvID(), resolved.ID),
			FailureMessage: "failed to redeploy container",
			SuccessMessage: fmt.Sprintf("Container %s redeployed successfully", containerDisplayName(resolved)),
			Timeout:        30 * time.Minute,
			JSON:           jsonOutput,
		})
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

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
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

		query := url.Values{}
		query.Set("force", strconv.FormatBool(forceFlag))
		query.Set("volumes", strconv.FormatBool(containersDeleteVolumes))
		path := types.Endpoints.Container(c.EnvID(), resolved.ID) + "?" + query.Encode()
		resp, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to delete container")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[base.MessageResponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to delete container")
		}

		if !result.Success {
			msg := result.Data.Message
			if msg == "" {
				msg = "unknown error"
			}
			return errors.Errorf("failed to delete container: %s", msg)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

		result, err := c.GetJSON[container.StatusCounts](cmd.Context(), types.Endpoints.ContainersCounts(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get container counts")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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
				return errors.WrapIff(err, "failed to read file %s", containerCreateFile)
			}
			if err := json.Unmarshal(data, &req); err != nil {
				return errors.WrapIf(err, "failed to parse config file")
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
			return errors.New("--name is required")
		}
		if req.Image == "" {
			return errors.New("--image is required")
		}

		result, err := c.PostJSON[container.Created](cmd.Context(), types.Endpoints.Containers(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create container")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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
	ContainersCmd.AddCommand(containersUpdatesCmd)
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
	containersListCmd.Flags().IntVar(&containersStart, "start", 0, cmdutil.StartFlagUsage)
	containersListCmd.Flags().BoolVarP(&containersAll, "all", "a", false, cmdutil.AllFlagUsage)
	containersListCmd.Flags().StringVar(&containersUpdatesFilter, "updates", "", "Filter by update status (has_update, up_to_date, error, unknown)")
	containersListCmd.Flags().StringVar(&containersStandalone, "standalone", "", "Filter standalone containers only (true/false)")
	containersListCmd.Flags().BoolVar(&containersIncludeInternal, "include-internal", false, "Include Arcane-internal containers")
	containersListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersUpdatesCmd.Flags().IntVarP(&containersLimit, "limit", "n", 20, "Number of containers to show")
	containersUpdatesCmd.Flags().IntVar(&containersStart, "start", 0, cmdutil.StartFlagUsage)
	containersUpdatesCmd.Flags().BoolVarP(&containersAll, "all", "a", false, cmdutil.AllFlagUsage)
	containersUpdatesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	containersDeleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force removal of a running container and skip the confirmation prompt")
	containersDeleteCmd.Flags().BoolVar(&containersDeleteVolumes, "volumes", false, "Remove anonymous volumes associated with the container")

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

var containerRef = cmdutil.ResourceRef[container.Details, container.Summary]{
	Singular: "container",
	Plural:   "containers",
	IDHint:   "the container ID",
	ListCmd:  "arcane containers list",
	GetPath:  types.Endpoints.Container,
	ListPath: types.Endpoints.Containers,
	Matches:  containerMatches,
	Label:    formatContainerOption,
	Promote:  containerDetailsFromSummary,
	IDOf:     func(item container.Summary) string { return item.ID },
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
