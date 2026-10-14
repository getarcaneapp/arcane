package containers

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/spf13/cobra"
	"go.getarcane.app/docker"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command builds the containers command tree.
func Command() *cobra.Command {
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeResource},
		Use:         "containers",
		Aliases:     []string{"container", "c"},
		Short:       "Manage containers",
	}
	command.AddCommand(
		newListCmd(),
		newUpdatesCmd(),
		newGetCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newUpdateCmd(),
		newRedeployCmd(),
		newDeleteCmd(),
		newCountsCmd(),
		newCreateCmd(),
		newKillCmd(),
		newPauseCmd(),
		newUnpauseCmd(),
		newCommitCmd(),
		newEditCmd(),
		newAutoUpdateCmd(),
	)
	return command
}

func newListCmd() *cobra.Command {
	return newContainerListCmd(false)
}

func newContainerListCmd(updatesOnly bool) *cobra.Command {
	var opts struct {
		limit         int
		start         int
		all           bool
		updatesFilter string
		standalone    string
		labelFilter   string
		include       bool
		includeHidden bool
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeMerged},
		Use:          kit.Ternary(updatesOnly, "updates", "list"),
		Aliases:      kit.Ternary(updatesOnly, []string(nil), []string{"ls"}),
		Short:        kit.Ternary(updatesOnly, "List containers with available updates", "List containers"),
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			query := url.Values{}
			if opts.standalone != "" {
				query.Set("standalone", opts.standalone)
			}
			if opts.includeHidden {
				query.Set("includeHidden", "true")
			}
			if opts.include {
				query.Set("include", "true")
			}
			updatesFilter := kit.Ternary(cmd.Name() == "updates", "has_update", strings.TrimSpace(opts.updatesFilter))
			if updatesFilter != "" {
				query.Set("updates", updatesFilter)
			}
			if labelFilter := strings.TrimSpace(opts.labelFilter); labelFilter != "" {
				query.Set("label", labelFilter)
			}

			if updatesFilter != "" && !cmdutil.JSONOutputEnabled(cmd) {
				output.Header("Container Updates")
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[container.Summary]{
				Resource: "containers",
				Endpoint: types.Containers(c.EnvID()),
				Params:   cmdutil.ListParams{Resource: "containers", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Query:    query,
				Headers:  kit.Ternary(updatesFilter != "", []string{"ID", "NAME", "IMAGE", "STATE", "UPDATES", "LATEST"}, []string{"ID", "NAME", "IMAGE", "STATE", "STATUS"}),
				Row: func(item container.Summary) []string {
					if updatesFilter == "" {
						return []string{
							item.ID[:min(len(item.ID), 12)],
							docker.ContainerNameFromNames(item.Names),
							item.Image,
							item.State,
							item.Status,
						}
					}
					status, latest := "unknown", "-"
					if item.UpdateInfo != nil {
						switch {
						case item.UpdateInfo.Error != "":
							status = "error"
						case item.UpdateInfo.HasUpdate:
							status = "has_update"
						default:
							status = "up_to_date"
						}
						switch {
						case strings.TrimSpace(item.UpdateInfo.LatestVersion) != "":
							latest = item.UpdateInfo.LatestVersion
						case strings.TrimSpace(item.UpdateInfo.LatestDigest) != "":
							latest = item.UpdateInfo.LatestDigest
						}
					}
					return []string{
						item.ID[:min(len(item.ID), 12)],
						docker.ContainerNameFromNames(item.Names),
						item.Image,
						item.State,
						status,
						latest,
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of containers to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	if !updatesOnly {
		cmdutil.EnumVarP(command, command.Flags(), &opts.updatesFilter, "updates", "", "", "Filter by update status", "has_update", "up_to_date", "error", "unknown")
		cmdutil.EnumVarP(command, command.Flags(), &opts.standalone, "standalone", "", "", "Filter standalone containers only", "true", "false")
		command.Flags().StringVar(&opts.labelFilter, "label", "", "Filter by label (key or key=value)")
		command.Flags().BoolVar(&opts.includeHidden, "include-hidden", false, "Include hidden containers")
		command.Flags().BoolVar(&opts.include, "include-internal", false, "Include Arcane-internal containers")
	}

	return command
}

func newUpdatesCmd() *cobra.Command {
	return newContainerListCmd(true)
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <container-id|name>",
		Short:        "Get detailed container information",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, err := ContainerRef.Fetch(cmd.Context(), c, args[0], cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}

			if cmdutil.JSONOutputEnabled(cmd) {
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
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "start <container-id|name>",
		Short:        "Start a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ContainerStart(c.EnvID(), resolved.ID),
				FailureMessage: "failed to start container",
				Render: func(base.MessageResponse) error {
					output.Success("Container %s started successfully", containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stop <container-id|name>",
		Short:        "Stop a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ContainerStop(c.EnvID(), resolved.ID),
				FailureMessage: "failed to stop container",
				Render: func(base.MessageResponse) error {
					output.Success("Container %s stopped successfully", containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "restart <container-id|name>",
		Short:        "Restart a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ContainerRestart(c.EnvID(), resolved.ID),
				FailureMessage: "failed to restart container",
				Render: func(base.MessageResponse) error {
					output.Success("Container %s restarted successfully", containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
}

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "update <container-id|name>",
		Short:        "Update a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			// This route is served by the updater handler, so it answers with an
			// updater.Result rather than a container.ActionResult.
			return cmdutil.Run(cmd, c, cmdutil.Spec[updater.Result]{
				Method:         http.MethodPost,
				Path:           types.ContainerUpdate(c.EnvID(), resolved.ID),
				FailureMessage: "failed to update container",
				Render: func(updater.Result) error {
					output.Success("Container %s updated successfully", containerDisplayName(resolved))
					return nil
				},
				Timeout: 30 * time.Minute,
			})
		},
	}
}

func newRedeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "redeploy <container-id|name>",
		Short:        "Redeploy a container (pull image and recreate)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[container.Details]{
				Method:         http.MethodPost,
				Path:           types.ContainerRedeploy(c.EnvID(), resolved.ID),
				FailureMessage: "failed to redeploy container",
				Render: func(container.Details) error {
					output.Success("Container %s redeployed successfully", containerDisplayName(resolved))
					return nil
				},
				Timeout: 30 * time.Minute,
			})
		},
	}
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		deleteVolumes bool
		force         bool
	}
	command := &cobra.Command{
		Use:          "delete <container-id|name>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			displayName := containerDisplayName(resolved)

			query := url.Values{
				"force":   {strconv.FormatBool(opts.force)},
				"volumes": {strconv.FormatBool(opts.deleteVolumes)},
			}
			path := types.Container(c.EnvID(), resolved.ID) + "?" + query.Encode()
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete container %s?", displayName),
				Path:           path,
				FailureMessage: "failed to delete container",
				SuccessMessage: fmt.Sprintf("Container %s deleted successfully", displayName),
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force removal of a running container and skip the confirmation prompt")
	command.Flags().BoolVar(&opts.deleteVolumes, "volumes", false, "Remove anonymous volumes associated with the container")
	return command
}

func newCountsCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeRead},
		Use:          "counts",
		Short:        "Get container status counts",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[container.StatusCounts]{
				Method:         http.MethodGet,
				Path:           types.ContainersCounts(c.EnvID()),
				FailureMessage: "failed to get container counts",
				Render: func(data container.StatusCounts) error {
					output.Header("Container Status Counts")
					output.KeyValue("Running", data.RunningContainers)
					output.KeyValue("Stopped", data.StoppedContainers)
					output.KeyValue("Total", data.TotalContainers)
					return nil
				},
			})
		},
	}
}

type createOptions struct {
	file       string
	name       string
	image      string
	env        []string
	port       []string
	volume     []string
	label      []string
	network    []string
	restart    string
	memory     int64
	cpus       float64
	privileged bool
	hostname   string
	user       string
	workdir    string
	entrypoint string
	cmd        string
}

func newCreateCmd() *cobra.Command {
	opts := &createOptions{}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeDestination},
		Use:          "create",
		Short:        "Create a new container",
		Example:      "  arcane containers create --name web --image nginx --env-var KEY=VALUE\n  arcane containers create --name web --image nginx -e KEY=VALUE",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			c.SetTimeout(30 * time.Minute)

			req, requestErr := opts.request(cmd)
			if requestErr != nil {
				return requestErr
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[container.Created]{
				Method:         http.MethodPost,
				Path:           types.Containers(c.EnvID()),
				Body:           req,
				FailureMessage: "failed to create container",
				Render: func(data container.Created) error {
					output.Success("Container %s created successfully", data.Name)
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("Image", data.Image)
					output.KeyValue("Status", data.Status)
					return nil
				},
			})
		},
	}
	command.Flags().StringVarP(&opts.file, "file", "f", "", "JSON config file for container creation")
	command.Flags().StringVar(&opts.name, "name", "", "Container name")
	command.Flags().StringVar(&opts.image, "image", "", "Docker image")
	command.Flags().StringArrayVarP(&opts.env, "env-var", "e", nil, "Environment variable (KEY=VALUE)")
	command.Flags().Var(command.Flags().Lookup("env-var").Value, "env", "Environment variable (KEY=VALUE)")
	// TODO(v3): remove the deprecated --env flag.
	_ = command.Flags().MarkDeprecated("env", "use --env-var or -e")
	command.Flags().StringArrayVarP(&opts.port, "port", "p", nil, "Port mapping (HOST:CONTAINER)")
	command.Flags().StringArrayVarP(&opts.volume, "volume", "v", nil, "Volume mount (SRC:DST)")
	command.Flags().StringArrayVarP(&opts.label, "label", "l", nil, "Label (KEY=VALUE)")
	command.Flags().StringArrayVar(&opts.network, "network", nil, "Networks to connect to")
	cmdutil.EnumVarP(command, command.Flags(), &opts.restart, "restart", "", "", "Restart policy", "no", "always", "unless-stopped", "on-failure")
	command.Flags().Int64Var(&opts.memory, "memory", 0, "Memory limit in bytes")
	command.Flags().Float64Var(&opts.cpus, "cpus", 0, "Number of CPUs")
	command.Flags().BoolVar(&opts.privileged, "privileged", false, "Run in privileged mode")
	command.Flags().StringVar(&opts.hostname, "hostname", "", "Container hostname")
	command.Flags().StringVar(&opts.user, "user", "", "User to run as")
	command.Flags().StringVar(&opts.workdir, "workdir", "", "Working directory")
	command.Flags().StringVar(&opts.entrypoint, "entrypoint", "", "Entrypoint command")
	command.Flags().StringVar(&opts.cmd, "cmd", "", "Command to run")
	return command
}

func containerDisplayName(details *container.Details) string {
	if details == nil {
		return ""
	}
	if strings.TrimSpace(details.Name) != "" {
		return details.Name
	}
	return details.ID[:min(len(details.ID), 12)]
}

var ContainerRef = cmdutil.ResourceRef[container.Details, container.Summary]{
	EnvironmentScoped: true,
	Singular:          "container",
	Plural:            "containers",
	IDHint:            "the container ID",
	ListCmd:           "arcane containers list",
	GetPath:           types.Container,
	ListPath:          types.Containers,
	Matches: func(item container.Summary, identifierLower, original string) bool {
		return strings.Contains(strings.ToLower(item.ID), identifierLower) ||
			strings.Contains(strings.ToLower(item.Image), identifierLower) ||
			slices.ContainsFunc(item.Names, func(name string) bool {
				trimmedName := strings.TrimPrefix(name, "/")
				return strings.Contains(strings.ToLower(trimmedName), identifierLower) || strings.EqualFold(trimmedName, original) || strings.EqualFold(name, original)
			})
	},
	Label: func(item container.Summary) string {
		name := cmp.Or(docker.ContainerNameFromNames(item.Names), item.ID[:min(len(item.ID), 12)])
		image := cmp.Or(item.Image, "<unknown>")
		state := cmp.Or(item.State, "unknown")
		return fmt.Sprintf("%s (%s, %s)", name, item.ID[:min(len(item.ID), 12)], image+" / "+state)
	},
	Promote: func(summary container.Summary) *container.Details {
		return &container.Details{ID: summary.ID, Name: docker.ContainerNameFromNames(summary.Names)}
	},
	IDOf:         func(item container.Summary) string { return item.ID },
	IdentifierOf: func(details container.Details) string { return details.ID },
}

func (opts *createOptions) request(cmd *cobra.Command) (container.Create, error) {
	var req container.Create

	// File mode: read base config from file
	if opts.file != "" {
		data, readFileErr := os.ReadFile(opts.file)
		if readFileErr != nil {
			return req, fmt.Errorf("failed to read file %s: %w", opts.file, readFileErr)
		}
		if unmarshalErr := json.Unmarshal(data, &req); unmarshalErr != nil {
			return req, fmt.Errorf("failed to parse config file: %w", unmarshalErr)
		}
	}

	// Flag overrides
	if cmd.Flags().Changed("name") {
		req.Name = opts.name
	}
	if cmd.Flags().Changed("image") {
		req.Image = opts.image
	}
	// TODO(v3): drop the deprecated flag handling.
	if cmd.Flags().Changed("env-var") || cmd.Flags().Changed("env") {
		req.Env = opts.env
	}
	if cmd.Flags().Changed("port") {
		// Parse "HOST:CONTAINER" format into ports map
		req.Ports = make(map[string]string)
		for _, p := range opts.port {
			parts := strings.SplitN(p, ":", 2)
			if len(parts) == 2 {
				req.Ports[parts[1]] = parts[0]
			}
		}
	}
	if cmd.Flags().Changed("volume") {
		req.Volumes = opts.volume
	}
	if cmd.Flags().Changed("label") {
		req.Labels = make(map[string]string)
		for _, l := range opts.label {
			parts := strings.SplitN(l, "=", 2)
			if len(parts) == 2 {
				req.Labels[parts[0]] = parts[1]
			}
		}
	}
	if cmd.Flags().Changed("network") {
		req.Networks = opts.network
	}
	if cmd.Flags().Changed("restart") {
		req.RestartPolicy = opts.restart
	}
	if cmd.Flags().Changed("memory") {
		req.Memory = opts.memory
	}
	if cmd.Flags().Changed("cpus") {
		req.CPUs = opts.cpus
	}
	if cmd.Flags().Changed("privileged") {
		req.Privileged = opts.privileged
	}
	if cmd.Flags().Changed("hostname") {
		req.Hostname = opts.hostname
	}
	if cmd.Flags().Changed("user") {
		req.User = opts.user
	}
	if cmd.Flags().Changed("workdir") {
		req.WorkingDir = opts.workdir
	}
	if cmd.Flags().Changed("entrypoint") {
		req.Entrypoint = []string{opts.entrypoint}
	}
	if cmd.Flags().Changed("cmd") {
		req.Cmd = []string{opts.cmd}
	}

	// Validate required fields
	if req.Name == "" {
		return req, errors.New("--name is required")
	}
	if req.Image == "" {
		return req, errors.New("--image is required")
	}
	return req, nil
}
