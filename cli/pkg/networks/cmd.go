package networks

import (
	"fmt"
	"net/url"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/network"
	"github.com/spf13/cobra"
)

var (
	limitFlag      int
	startFlag      int
	allFlag        bool
	forceFlag      bool
	jsonOutput     bool
	inUseOnlyFlag  bool
	unusedOnlyFlag bool
)

var (
	networkCreateName       string
	networkCreateDriver     string
	networkCreateSubnet     string
	networkCreateGateway    string
	networkCreateIPRange    string
	networkCreateInternal   bool
	networkCreateAttachable bool
	networkCreateIPv6       bool
	networkCreateLabels     []string
	networkCreateOpts       []string
)

var (
	connectAliases []string
	connectIPv4    string
	connectIPv6    string

	disconnectForce bool
)

// NetworksCmd is the parent command for network operations
var NetworksCmd = &cobra.Command{
	Use:     "networks",
	Aliases: []string{"network", "net", "n"},
	Short:   "Manage networks",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List networks",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if inUseOnlyFlag && unusedOnlyFlag {
			return errors.New("--inuse and --unused cannot be used together")
		}
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Filter server-side so that pagination and the reported totals apply to
		// the matching set rather than to whichever page happened to be fetched.
		query := url.Values{}
		if inUseOnlyFlag {
			query.Set("inUse", "true")
		}
		if unusedOnlyFlag {
			query.Set("inUse", "false")
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[network.Summary]{
			Resource: "networks",
			Endpoint: types.Networks(c.EnvID()),
			Params: cmdutil.ListParams{
				Resource:        "networks",
				Limit:           limitFlag,
				FallbackDefault: 20,
				Start:           startFlag,
				All:             allFlag,
			},
			Query:   query,
			JSON:    jsonOutput,
			Headers: []string{"ID", "NAME", "DRIVER", "SCOPE", "CREATED", "IN USE"},
			Row: func(net network.Summary) []string {
				inUse := "No"
				if net.InUse {
					inUse = "Yes"
				}
				return []string{
					shortID(net.ID),
					net.Name,
					net.Driver,
					net.Scope,
					net.Created.Format("2006-01-02 15:04"),
					inUse,
				}
			},
		})
	},
}

var getCmd = &cobra.Command{
	Use:          "get <network-id|name>",
	Short:        "Get network details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, complete, err := networkRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			result, err := c.GetJSON[network.Inspect](cmd.Context(), types.Network(c.EnvID(), resolved.ID))
			if err != nil {
				return errors.WrapIf(err, "failed to get network")
			}
			resolved = &result.Data
		}

		if jsonOutput {
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

var deleteCmd = &cobra.Command{
	Use:          "delete <network-id|name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a network",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := networkRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		display := resolved.Name
		if display == "" {
			display = shortID(resolved.ID)
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete network %s?", display))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		result, err := c.DeleteJSON[base.MessageResponse](cmd.Context(), types.Network(c.EnvID(), resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to delete network")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Network %s deleted successfully", display)
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get network usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.GetJSON[network.UsageCounts](cmd.Context(), types.NetworksCounts(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get network counts")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Network Usage Counts")
		output.KeyValue("Total networks", result.Data.Total)
		output.KeyValue("In use", result.Data.Inuse)
		output.KeyValue("Unused", result.Data.Unused)
		return nil
	},
}

var pruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused networks",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, "Are you sure you want to prune unused networks?")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.PostJSON[network.PruneReport](cmd.Context(), types.NetworksPrune(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to prune networks")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Networks pruned successfully")
		output.KeyValue("Deleted networks", len(result.Data.NetworksDeleted))
		output.KeyValue("Space Reclaimed", output.UnsignedBytes(result.Data.SpaceReclaimed))
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a network",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		req := network.CreateRequest{
			Name: networkCreateName,
			Options: network.CreateOptions{
				Driver:     networkCreateDriver,
				Internal:   networkCreateInternal,
				Attachable: networkCreateAttachable,
				EnableIPv6: networkCreateIPv6,
			},
		}

		if networkCreateSubnet != "" || networkCreateGateway != "" || networkCreateIPRange != "" {
			req.Options.IPAM = &network.IPAM{
				Config: []network.IPAMConfig{{
					Subnet:  networkCreateSubnet,
					Gateway: networkCreateGateway,
					IPRange: networkCreateIPRange,
				}},
			}
		}

		if len(networkCreateOpts) > 0 {
			req.Options.Options = make(map[string]string)
			for _, opt := range networkCreateOpts {
				parts := strings.SplitN(opt, "=", 2)
				if len(parts) == 2 {
					req.Options.Options[parts[0]] = parts[1]
				}
			}
		}

		if len(networkCreateLabels) > 0 {
			req.Options.Labels = make(map[string]string)
			for _, lbl := range networkCreateLabels {
				parts := strings.SplitN(lbl, "=", 2)
				if len(parts) == 2 {
					req.Options.Labels[parts[0]] = parts[1]
				}
			}
		}

		result, err := c.PostJSON[network.CreateResponse](cmd.Context(), types.Networks(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create network")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Network %s created successfully", networkCreateName)
		output.KeyValue("ID", result.Data.ID)
		if result.Data.Warning != "" {
			output.Warning("%s", result.Data.Warning)
		}
		return nil
	},
}

var connectCmd = &cobra.Command{
	Use:          "connect <network-id|name> <container-id|name>",
	Short:        "Connect a container to a network",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, _, err := networkRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		req := network.ConnectContainerRequest{
			ContainerID: args[1],
			Aliases:     connectAliases,
			IPv4Address: connectIPv4,
			IPv6Address: connectIPv6,
		}

		result, err := c.PostJSON[base.MessageResponse](cmd.Context(), types.NetworkConnect(c.EnvID(), resolved.ID), req)
		if err != nil {
			return errors.WrapIf(err, "failed to connect container to network")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		display := resolved.Name
		if display == "" {
			display = shortID(resolved.ID)
		}
		output.Success("Container %s connected to network %s", args[1], display)
		return nil
	},
}

var disconnectCmd = &cobra.Command{
	Use:          "disconnect <network-id|name> <container-id|name>",
	Short:        "Disconnect a container from a network",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, _, err := networkRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		req := network.DisconnectContainerRequest{
			ContainerID: args[1],
			Force:       disconnectForce,
		}

		result, err := c.PostJSON[base.MessageResponse](cmd.Context(), types.NetworkDisconnect(c.EnvID(), resolved.ID), req)
		if err != nil {
			return errors.WrapIf(err, "failed to disconnect container from network")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		display := resolved.Name
		if display == "" {
			display = shortID(resolved.ID)
		}
		output.Success("Container %s disconnected from network %s", args[1], display)
		return nil
	},
}

var topologyCmd = &cobra.Command{
	Use:          "topology",
	Short:        "Show the network topology",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.GetJSON[network.Topology](cmd.Context(), types.NetworksTopology(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get network topology")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		nodeNames := make(map[string]string, len(result.Data.Nodes))
		networkCount := 0
		containerCount := 0
		for _, node := range result.Data.Nodes {
			name := node.Name
			if name == "" {
				name = shortID(node.ID)
			}
			nodeNames[node.ID] = name
			switch node.Type {
			case network.TopologyNodeTypeNetwork:
				networkCount++
			case network.TopologyNodeTypeContainer:
				containerCount++
			}
		}

		output.Header("Network Topology")
		output.KeyValue("Networks", networkCount)
		output.KeyValue("Containers", containerCount)
		output.KeyValue("Connections", len(result.Data.Edges))

		if len(result.Data.Edges) == 0 {
			return nil
		}

		headers := []string{"NETWORK", "CONTAINER", "IPV4", "IPV6"}
		rows := make([][]string, len(result.Data.Edges))
		for i, edge := range result.Data.Edges {
			source := nodeNames[edge.Source]
			if source == "" {
				source = shortID(edge.Source)
			}
			target := nodeNames[edge.Target]
			if target == "" {
				target = shortID(edge.Target)
			}
			rows[i] = []string{source, target, edge.IPv4Address, edge.IPv6Address}
		}
		output.Table(headers, rows)
		return nil
	},
}

func init() {
	NetworksCmd.AddCommand(listCmd)
	NetworksCmd.AddCommand(getCmd)
	NetworksCmd.AddCommand(deleteCmd)
	NetworksCmd.AddCommand(countsCmd)
	NetworksCmd.AddCommand(pruneCmd)
	NetworksCmd.AddCommand(createCmd)
	NetworksCmd.AddCommand(connectCmd)
	NetworksCmd.AddCommand(disconnectCmd)
	NetworksCmd.AddCommand(topologyCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of networks to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&inUseOnlyFlag, "inuse", false, "Only show networks currently in use")
	listCmd.Flags().BoolVar(&unusedOnlyFlag, "unused", false, "Only show networks not in use")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Prune command flags
	pruneCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force prune without confirmation")
	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Counts command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringVar(&networkCreateName, "name", "", "Network name (required)")
	createCmd.Flags().StringVar(&networkCreateDriver, "driver", "", "Network driver (e.g. bridge, overlay)")
	createCmd.Flags().StringVar(&networkCreateSubnet, "subnet", "", "Subnet in CIDR format")
	createCmd.Flags().StringVar(&networkCreateGateway, "gateway", "", "IPv4 or IPv6 gateway for the subnet")
	createCmd.Flags().StringVar(&networkCreateIPRange, "ip-range", "", "Allocate container IPs from a sub-range")
	createCmd.Flags().BoolVar(&networkCreateInternal, "internal", false, "Restrict external access to the network")
	createCmd.Flags().BoolVar(&networkCreateAttachable, "attachable", false, "Allow manual container attachment")
	createCmd.Flags().BoolVar(&networkCreateIPv6, "ipv6", false, "Enable IPv6 networking")
	createCmd.Flags().StringArrayVar(&networkCreateLabels, "label", nil, "Label (KEY=VALUE)")
	createCmd.Flags().StringArrayVar(&networkCreateOpts, "opt", nil, "Driver option (KEY=VALUE)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")

	// Connect command flags
	connectCmd.Flags().StringArrayVar(&connectAliases, "alias", nil, "Network-scoped alias for the container")
	connectCmd.Flags().StringVar(&connectIPv4, "ip", "", "Static IPv4 address")
	connectCmd.Flags().StringVar(&connectIPv6, "ip6", "", "Static IPv6 address")
	connectCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Disconnect command flags
	disconnectCmd.Flags().BoolVarP(&disconnectForce, "force", "f", false, "Force the disconnect")
	disconnectCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Topology command flags
	topologyCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

var networkRef = cmdutil.ResourceRef[network.Inspect, network.Summary]{
	Singular: "network",
	Plural:   "networks",
	IDHint:   "the network ID",
	ListCmd:  "arcane networks list",
	GetPath:  types.Network,
	ListPath: types.Networks,
	Matches:  networkMatches,
	Label: func(match network.Summary) string {
		return fmt.Sprintf("%s (%s)", match.Name, shortID(match.ID))
	},
	Promote: func(match network.Summary) *network.Inspect {
		return &network.Inspect{
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

func networkMatches(item network.Summary, identifierLower, original string) bool {
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
	return strings.EqualFold(item.Name, original)
}
