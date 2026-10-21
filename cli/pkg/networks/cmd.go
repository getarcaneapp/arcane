package networks

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/containers"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "networks",
		Aliases: []string{"network", "net", "n"},
		Short:   "Manage networks",
	}
	cmd.AddCommand(
		newListCmd(),
		newGetCmd(),
		newDeleteCmd(),
		newCountsCmd(),
		newPruneCmd(),
		newCreateCmd(),
		newConnectCmd(),
		newDisconnectCmd(),
		newTopologyCmd(),
	)
	cmdutil.SetScope(cmdutil.ScopeResource, cmd)
	return cmd
}

func newListCmd() *cobra.Command {
	var opts struct {
		limitFlag      int
		startFlag      int
		allFlag        bool
		inUseOnlyFlag  bool
		unusedOnlyFlag bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List networks",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			// Filter server-side so that pagination and the reported totals apply to
			// the matching set rather than to whichever page happened to be fetched.
			query := url.Values{}
			if opts.inUseOnlyFlag {
				query.Set("inUse", "true")
			}
			if opts.unusedOnlyFlag {
				query.Set("inUse", "false")
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.NetworkSummary]{
				Resource: "networks",
				Endpoint: clitypes.Networks(c.EnvID()),
				Params: cmdutil.ListParams{
					Resource:        "networks",
					Limit:           opts.limitFlag,
					FallbackDefault: 20,
					Start:           opts.startFlag,
					All:             opts.allFlag,
				},
				Query:   query,
				Headers: []string{"ID", "NAME", "DRIVER", "SCOPE", "CREATED", "IN USE"},
				Row: func(net types.NetworkSummary) []string {
					return []string{
						net.ID[:min(len(net.ID), 12)],
						net.Name,
						net.Driver,
						net.Scope,
						net.Created.Format("2006-01-02 15:04"),
						kit.Ternary(net.InUse, "Yes", "No"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limitFlag, "limit", "n", 20, "Number of networks to show")
	cmd.Flags().IntVar(&opts.startFlag, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	cmd.Flags().BoolVar(&opts.inUseOnlyFlag, "inuse", false, "Only show networks currently in use")
	cmd.Flags().BoolVar(&opts.unusedOnlyFlag, "unused", false, "Only show networks not in use")
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error { return cmdutil.ExclusiveFlags(cmd, "inuse", "unused") }
	return cmd
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <network-id|name>",
		Short:        "Get network details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, err := networkRef.Fetch(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(resolved)
			}

			output.Header("Network Details")
			output.KeyValue("ID", resolved.ID)
			output.KeyValue("Name", resolved.Name)
			output.KeyValue("Driver", resolved.Driver)
			output.KeyValue("Scope", resolved.Scope)
			output.KeyValue("Created", resolved.Created.Format("2006-01-02 15:04"))
			output.KeyValue("Internal", resolved.Internal)
			output.KeyValue("Attachable", resolved.Attachable)
			output.KeyValue("Ingress", resolved.Ingress)
			output.KeyValue("Containers", len(resolved.ContainersList))
			return nil
		},
	}
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		forceFlag bool
	}
	cmd := &cobra.Command{
		Use:          "delete <network-id|name>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a network",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := networkRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			display := cmp.Or(resolved.Name, resolved.ID[:min(len(resolved.ID), 12)])
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.forceFlag,
				Prompt:         fmt.Sprintf("Are you sure you want to delete network %s?", display),
				Path:           clitypes.Network(c.EnvID(), resolved.ID),
				FailureMessage: "failed to delete network",
				SuccessMessage: fmt.Sprintf("Network %s deleted successfully", display),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Force deletion without confirmation")
	return cmd
}

func newCountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "counts",
		Short:        "Get network usage counts",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.NetworkUsageCounts]{
				Method:         http.MethodGet,
				Path:           clitypes.NetworksCounts(c.EnvID()),
				FailureMessage: "failed to get network counts",
				Render: func(data types.NetworkUsageCounts) error {
					output.Header("Network Usage Counts")
					output.KeyValue("Total networks", data.Total)
					output.KeyValue("In use", data.Inuse)
					output.KeyValue("Unused", data.Unused)
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newPruneCmd() *cobra.Command {
	var opts struct {
		forceFlag bool
	}
	cmd := &cobra.Command{
		Use:          "prune",
		Short:        "Remove unused networks",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.forceFlag, "Are you sure you want to prune unused networks?")
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.NetworkPruneReport]{
				Method:         http.MethodPost,
				Path:           clitypes.NetworksPrune(c.EnvID()),
				Body:           nil,
				FailureMessage: "failed to prune networks",
				Render: func(data types.NetworkPruneReport) error {
					output.Success("Networks pruned successfully")
					output.KeyValue("Deleted networks", len(data.NetworksDeleted))
					output.KeyValue("Space Reclaimed", output.UnsignedBytes(data.SpaceReclaimed))
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Force prune without confirmation")
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	return cmd
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		networkCreateName       string
		networkCreateDriver     string
		networkCreateSubnet     string
		networkCreateGateway    string
		networkCreateIPRange    string
		networkCreate           bool
		networkCreateAttachable bool
		networkCreateIPv6       bool
		networkCreateLabels     []string
		networkCreateOpts       []string
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a network",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			var ipam *types.NetworkIPAM
			var options, labels map[string]string

			if opts.networkCreateSubnet != "" || opts.networkCreateGateway != "" || opts.networkCreateIPRange != "" {
				ipam = &types.NetworkIPAM{
					Config: []types.NetworkIPAMConfig{{
						Subnet:  opts.networkCreateSubnet,
						Gateway: opts.networkCreateGateway,
						IPRange: opts.networkCreateIPRange,
					}},
				}
			}

			if len(opts.networkCreateOpts) > 0 {
				options = make(map[string]string)
				for _, opt := range opts.networkCreateOpts {
					parts := strings.SplitN(opt, "=", 2)
					if len(parts) == 2 {
						options[parts[0]] = parts[1]
					}
				}
			}

			if len(opts.networkCreateLabels) > 0 {
				labels = make(map[string]string)
				for _, lbl := range opts.networkCreateLabels {
					parts := strings.SplitN(lbl, "=", 2)
					if len(parts) == 2 {
						labels[parts[0]] = parts[1]
					}
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.NetworkCreateResponse]{
				Method: http.MethodPost,
				Path:   clitypes.Networks(c.EnvID()),
				Body: types.Network{
					Name: opts.networkCreateName,
					Options: types.NetworkCreateOptions{
						Driver:     opts.networkCreateDriver,
						Internal:   opts.networkCreate,
						Attachable: opts.networkCreateAttachable,
						EnableIPv6: opts.networkCreateIPv6,
						IPAM:       ipam,
						Options:    options,
						Labels:     labels,
					},
				},
				FailureMessage: "failed to create network",
				Render: func(data types.NetworkCreateResponse) error {
					output.Success("Network %s created successfully", opts.networkCreateName)
					output.KeyValue("ID", data.ID)
					if data.Warning != "" {
						output.Warning("%s", data.Warning)
					}
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.networkCreateName, "name", "", "Network name (required)")
	cmd.Flags().StringVar(&opts.networkCreateDriver, "driver", "", "Network driver (e.g. bridge, overlay)")
	cmd.Flags().StringVar(&opts.networkCreateSubnet, "subnet", "", "Subnet in CIDR format")
	cmd.Flags().StringVar(&opts.networkCreateGateway, "gateway", "", "IPv4 or IPv6 gateway for the subnet")
	cmd.Flags().StringVar(&opts.networkCreateIPRange, "ip-range", "", "Allocate container IPs from a sub-range")
	cmd.Flags().BoolVar(&opts.networkCreate, "internal", false, "Restrict external access to the network")
	cmd.Flags().BoolVar(&opts.networkCreateAttachable, "attachable", false, "Allow manual container attachment")
	cmd.Flags().BoolVar(&opts.networkCreateIPv6, "ipv6", false, "Enable IPv6 networking")
	cmd.Flags().StringArrayVar(&opts.networkCreateLabels, "label", nil, "Label (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&opts.networkCreateOpts, "opt", nil, "Driver option (KEY=VALUE)")
	_ = cmd.MarkFlagRequired("name")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newConnectCmd() *cobra.Command {
	var opts struct {
		connectAliases []string
		connectIPv4    string
		connectIPv6    string
	}
	cmd := &cobra.Command{
		Use:          "connect <network-id|name> <container-id|name>",
		Short:        "Connect a container to a network",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			app, _ := runtime.From(cmd.Context())
			preview := app.PreviewEnabled()
			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := networkRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			containerID := args[1]
			if preview {
				dependent, _, resolveErr := containers.ContainerRef.Resolve(cmd.Context(), c, args[1], allowPrompt)
				if resolveErr != nil {
					return resolveErr
				}
				containerID = dependent.ID
			}

			req := types.NetworkConnectContainerRequest{
				ContainerID: containerID,
				Aliases:     opts.connectAliases,
				IPv4Address: opts.connectIPv4,
				IPv6Address: opts.connectIPv6,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.NetworkConnect(c.EnvID(), resolved.ID),
				Body:           req,
				FailureMessage: "failed to connect container to network",
				Render: func(types.MessageResponse) error {
					display := cmp.Or(resolved.Name, resolved.ID[:min(len(resolved.ID), 12)])
					output.Success("Container %s connected to network %s", args[1], display)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringArrayVar(&opts.connectAliases, "alias", nil, "Network-scoped alias for the container")
	cmd.Flags().StringVar(&opts.connectIPv4, "ip", "", "Static IPv4 address")
	cmd.Flags().StringVar(&opts.connectIPv6, "ip6", "", "Static IPv6 address")
	return cmd
}

func newDisconnectCmd() *cobra.Command {
	var opts struct {
		disconnectForce bool
	}
	cmd := &cobra.Command{
		Use:          "disconnect <network-id|name> <container-id|name>",
		Short:        "Disconnect a container from a network",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			app, _ := runtime.From(cmd.Context())
			preview := app.PreviewEnabled()
			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := networkRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			containerID := args[1]
			if preview {
				dependent, _, resolveErr := containers.ContainerRef.Resolve(cmd.Context(), c, args[1], allowPrompt)
				if resolveErr != nil {
					return resolveErr
				}
				containerID = dependent.ID
			}

			req := types.NetworkDisconnectContainerRequest{
				ContainerID: containerID,
				Force:       opts.disconnectForce,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.NetworkDisconnect(c.EnvID(), resolved.ID),
				Body:           req,
				FailureMessage: "failed to disconnect container from network",
				Render: func(types.MessageResponse) error {
					display := cmp.Or(resolved.Name, resolved.ID[:min(len(resolved.ID), 12)])
					output.Success("Container %s disconnected from network %s", args[1], display)
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.disconnectForce, "force", "f", false, "Force the disconnect")
	return cmd
}

func newTopologyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "topology",
		Short:        "Show the network topology",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.NetworkTopology]{
				Method:         http.MethodGet,
				Path:           clitypes.NetworksTopology(c.EnvID()),
				FailureMessage: "failed to get network topology",
				Render: func(data types.NetworkTopology) error {
					nodeNames := make(map[string]string, len(data.Nodes))
					networkCount := 0
					containerCount := 0
					for _, node := range data.Nodes {
						nodeNames[node.ID] = cmp.Or(node.Name, node.ID[:min(len(node.ID), 12)])
						switch node.Type {
						case types.NetworkTopologyNodeTypeNetwork:
							networkCount++
						case types.NetworkTopologyNodeTypeContainer:
							containerCount++
						}
					}

					output.Header("Network Topology")
					output.KeyValue("Networks", networkCount)
					output.KeyValue("Containers", containerCount)
					output.KeyValue("Connections", len(data.Edges))

					if len(data.Edges) == 0 {
						return nil
					}

					headers := []string{"NETWORK", "CONTAINER", "IPV4", "IPV6"}
					rows := make([][]string, len(data.Edges))
					for i, edge := range data.Edges {
						source := cmp.Or(nodeNames[edge.Source], edge.Source[:min(len(edge.Source), 12)])
						target := cmp.Or(nodeNames[edge.Target], edge.Target[:min(len(edge.Target), 12)])
						rows[i] = []string{source, target, edge.IPv4Address, edge.IPv6Address}
					}
					return cmdutil.PrintTable(cmd, headers, rows)
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

var networkRef = cmdutil.ResourceRef[types.NetworkInspect, types.NetworkSummary]{
	EnvironmentScoped: true,
	Singular:          "network",
	Plural:            "networks",
	IDHint:            "the network ID",
	ListCmd:           "arcane networks list",
	GetPath:           clitypes.Network,
	IdentifierOf:      func(data types.NetworkInspect) string { return data.ID },
	ListPath:          clitypes.Networks,
	Matches: func(item types.NetworkSummary, identifierLower, original string) bool {
		return strings.Contains(strings.ToLower(item.ID), identifierLower) ||
			strings.Contains(strings.ToLower(item.Name), identifierLower) || strings.EqualFold(item.Name, original)
	},
	Label: func(match types.NetworkSummary) string {
		return fmt.Sprintf("%s (%s)", match.Name, match.ID[:min(len(match.ID), 12)])
	},
	Promote: func(match types.NetworkSummary) *types.NetworkInspect {
		return &types.NetworkInspect{
			ID:      match.ID,
			Name:    match.Name,
			Driver:  match.Driver,
			Scope:   match.Scope,
			Created: match.Created,
			Options: match.Options,
			Labels:  match.Labels,
		}
	},
}
