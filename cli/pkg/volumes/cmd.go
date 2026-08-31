package volumes

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/spf13/cobra"
)

var (
	limitFlag           int
	startFlag           int
	allFlag             bool
	forceFlag           bool
	jsonOutput          bool
	inUseOnlyFlag       bool
	unusedOnlyFlag      bool
	includeInternalFlag bool
)

var (
	volumeCreateName   string
	volumeCreateDriver string
	volumeCreateOpts   []string
	volumeCreateLabels []string
)

// VolumesCmd is the parent command for volume operations
var VolumesCmd = &cobra.Command{
	Use:     "volumes",
	Aliases: []string{"volume", "vol", "v"},
	Short:   "Manage volumes",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List volumes",
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
		if includeInternalFlag {
			query.Set("includeInternal", "true")
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[volume.Volume]{
			Resource: "volumes",
			Endpoint: types.Volumes(c.EnvID()),
			Params: cmdutil.ListParams{
				Resource:        "volumes",
				Limit:           limitFlag,
				FallbackDefault: 20,
				Start:           startFlag,
				All:             allFlag,
			},
			Query:   query,
			JSON:    jsonOutput,
			Headers: []string{"NAME", "DRIVER", "MOUNTPOINT", "CREATED", "IN USE"},
			Row: func(vol volume.Volume) []string {
				inUse := "No"
				if vol.InUse {
					inUse = "Yes"
				}
				return []string{
					vol.Name,
					vol.Driver,
					vol.Mountpoint,
					vol.CreatedAt,
					inUse,
				}
			},
		})
	},
}

var getCmd = &cobra.Command{
	Use:          "get <volume-name>",
	Short:        "Get volume details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
		}

		output.Header("Volume Details")
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("Driver", resolved.Driver)
		output.KeyValue("Mountpoint", resolved.Mountpoint)
		output.KeyValue("Scope", resolved.Scope)
		output.KeyValue("Created", resolved.CreatedAt)
		output.KeyValue("In Use", resolved.InUse)
		if resolved.Size > 0 {
			output.KeyValue("Size", output.Bytes(resolved.Size))
		}
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <volume-name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a volume",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !forceFlag && prompt.IsInteractive()
		resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete volume %s?", resolved.Name))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		deletePath := cmdutil.AppendQuery(
			types.Volume(c.EnvID(), resolved.Name),
			url.Values{"force": []string{strconv.FormatBool(forceFlag)}},
		)
		resp, err := c.Delete(cmd.Context(), deletePath)
		if err != nil {
			return errors.WrapIf(err, "failed to delete volume")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete volume")
		}

		output.Success("Volume %s deleted successfully", resolved.Name)
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get volume usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVolumeDataCommand(cmd, volumeDataCommandConfig{
			endpoint:       types.VolumesCounts,
			failureMessage: "failed to get volume counts",
			header:         "Volume Usage Counts",
			marshalMessage: "failed to marshal volume counts",
		})
	},
}

var pruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused volumes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, "Are you sure you want to prune unused volumes?")
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

		return cmdutil.RunPostAction[any](cmd, c, cmdutil.PostActionSpec{
			Path:           types.VolumesPrune(c.EnvID()),
			FailureMessage: "failed to prune volumes",
			SuccessMessage: "Volumes pruned successfully",
			JSON:           jsonOutput,
		})
	},
}

var sizesCmd = &cobra.Command{
	Use:          "sizes",
	Short:        "Get volume sizes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVolumeDataCommand(cmd, volumeDataCommandConfig{
			endpoint:       types.VolumesSizes,
			failureMessage: "failed to get volume sizes",
			header:         "Volume Sizes",
			marshalMessage: "failed to marshal volume sizes",
		})
	},
}

type volumeDataCommandConfig struct {
	endpoint       func(string) string
	failureMessage string
	header         string
	marshalMessage string
}

func runVolumeDataCommand(cmd *cobra.Command, cfg volumeDataCommandConfig) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	result, err := c.GetJSON[any](cmd.Context(), cfg.endpoint(c.EnvID()))
	if err != nil {
		return errors.WrapIff(err, "%s", cfg.failureMessage)
	}

	if !jsonOutput {
		output.Header("%s", cfg.header)
	}
	return errors.WrapIff(cmdutil.PrintJSON(result.Data), "%s", cfg.marshalMessage)
}

var usageCmd = &cobra.Command{
	Use:          "usage <volume-name>",
	Short:        "Get specific volume usage",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		result, err := c.GetJSON[any](cmd.Context(), types.VolumeUsage(c.EnvID(), resolved.Name))
		if err != nil {
			return errors.WrapIf(err, "failed to get volume usage")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Volume Usage: %s", resolved.Name)
		return cmdutil.PrintJSON(result.Data)
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a volume",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		req := volume.Create{
			Name:   volumeCreateName,
			Driver: volumeCreateDriver,
		}

		if len(volumeCreateOpts) > 0 {
			req.DriverOpts = make(map[string]string)
			for _, opt := range volumeCreateOpts {
				parts := strings.SplitN(opt, "=", 2)
				if len(parts) == 2 {
					req.DriverOpts[parts[0]] = parts[1]
				}
			}
		}

		if len(volumeCreateLabels) > 0 {
			req.Labels = make(map[string]string)
			for _, lbl := range volumeCreateLabels {
				parts := strings.SplitN(lbl, "=", 2)
				if len(parts) == 2 {
					req.Labels[parts[0]] = parts[1]
				}
			}
		}

		result, err := c.PostJSON[volume.Volume](cmd.Context(), types.Volumes(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create volume")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Volume %s created successfully", result.Data.Name)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Driver", result.Data.Driver)
		output.KeyValue("Mountpoint", result.Data.Mountpoint)
		return nil
	},
}

func init() {
	VolumesCmd.AddCommand(listCmd)
	VolumesCmd.AddCommand(getCmd)
	VolumesCmd.AddCommand(deleteCmd)
	VolumesCmd.AddCommand(countsCmd)
	VolumesCmd.AddCommand(pruneCmd)
	VolumesCmd.AddCommand(sizesCmd)
	VolumesCmd.AddCommand(usageCmd)
	VolumesCmd.AddCommand(createCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of volumes to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&inUseOnlyFlag, "inuse", false, "Only show volumes currently in use")
	listCmd.Flags().BoolVar(&unusedOnlyFlag, "unused", false, "Only show volumes not in use")
	listCmd.Flags().BoolVar(&includeInternalFlag, "include-internal", false, "Include Arcane-internal volumes")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force removal of a volume that is in use and skip the confirmation prompt")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Prune command flags
	pruneCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force prune without confirmation")
	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Other command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	sizesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	usageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringVar(&volumeCreateName, "name", "", "Volume name")
	createCmd.Flags().StringVar(&volumeCreateDriver, "driver", "", "Volume driver")
	createCmd.Flags().StringArrayVar(&volumeCreateOpts, "opt", nil, "Driver option (KEY=VALUE)")
	createCmd.Flags().StringArrayVar(&volumeCreateLabels, "label", nil, "Label (KEY=VALUE)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
}

var volumeRef = cmdutil.ResourceRef[volume.Volume, volume.Volume]{
	Singular: "volume",
	Plural:   "volumes",
	IDHint:   "the volume name",
	ListCmd:  "arcane volumes list",
	GetPath:  types.Volume,
	ListPath: types.Volumes,
	Matches:  volumeMatches,
	Label: func(match volume.Volume) string {
		if match.Driver != "" {
			return fmt.Sprintf("%s (%s)", match.Name, match.Driver)
		}
		return match.Name
	},
	Promote: func(match volume.Volume) *volume.Volume { return &match },
}

func volumeMatches(item volume.Volume, identifierLower, original string) bool {
	if strings.EqualFold(item.Name, original) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), identifierLower) {
		return true
	}
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	return false
}
