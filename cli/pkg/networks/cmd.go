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
			Endpoint: types.Endpoints.Networks(c.EnvID()),
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
			result, err := c.GetJSON[network.Inspect](cmd.Context(), types.Endpoints.Network(c.EnvID(), resolved.ID))
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

		result, err := c.DeleteJSON[base.MessageResponse](cmd.Context(), types.Endpoints.Network(c.EnvID(), resolved.ID))
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

		result, err := c.GetJSON[network.UsageCounts](cmd.Context(), types.Endpoints.NetworksCounts(c.EnvID()))
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

		result, err := c.PostJSON[network.PruneReport](cmd.Context(), types.Endpoints.NetworksPrune(c.EnvID()), nil)
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

func init() {
	NetworksCmd.AddCommand(listCmd)
	NetworksCmd.AddCommand(getCmd)
	NetworksCmd.AddCommand(deleteCmd)
	NetworksCmd.AddCommand(countsCmd)
	NetworksCmd.AddCommand(pruneCmd)

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
	GetPath:  types.Endpoints.Network,
	ListPath: types.Endpoints.Networks,
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
