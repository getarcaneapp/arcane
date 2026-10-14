package volumes

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volumes",
		Aliases: []string{"volume", "vol", "v"},
		Short:   "Manage volumes",
	}
	cmd.AddCommand(
		newWorkspaceCmd(),
		newRenameCmd(),
		newBackupsCmd(),
		newListCmd(),
		newGetCmd(),
		newDeleteCmd(),
		newCountsCmd(),
		newPruneCmd(),
		newSizesCmd(),
		newUsageCmd(),
		newCreateCmd(),
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
		includeFlag    bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List volumes",
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
			if opts.includeFlag {
				query.Set("include", "true")
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[volume.Volume]{
				Resource: "volumes",
				Endpoint: types.Volumes(c.EnvID()),
				Params: cmdutil.ListParams{
					Resource:        "volumes",
					Limit:           opts.limitFlag,
					FallbackDefault: 20,
					Start:           opts.startFlag,
					All:             opts.allFlag,
				},
				Query:   query,
				Headers: []string{"NAME", "DRIVER", "MOUNTPOINT", "CREATED", "IN USE"},
				Row: func(vol volume.Volume) []string {
					return []string{
						vol.Name,
						vol.Driver,
						vol.Mountpoint,
						vol.CreatedAt,
						kit.Ternary(vol.InUse, "Yes", "No"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limitFlag, "limit", "n", 20, "Number of volumes to show")
	cmd.Flags().IntVar(&opts.startFlag, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	cmd.Flags().BoolVar(&opts.inUseOnlyFlag, "inuse", false, "Only show volumes currently in use")
	cmd.Flags().BoolVar(&opts.unusedOnlyFlag, "unused", false, "Only show volumes not in use")
	cmd.Flags().BoolVar(&opts.includeFlag, "include-internal", false, "Include Arcane-internal volumes")
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error { return cmdutil.ExclusiveFlags(cmd, "inuse", "unused") }
	return cmd
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <volume-name>",
		Short:        "Get volume details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, err := volumeRef.Fetch(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			if cmdutil.JSONOutputEnabled(cmd) {
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
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		forceFlag bool
	}
	cmd := &cobra.Command{
		Use:          "delete <volume-name>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a volume",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := !opts.forceFlag && cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.forceFlag,
				Prompt:         fmt.Sprintf("Are you sure you want to delete volume %s?", resolved.Name),
				Path:           cmdutil.AppendQuery(types.Volume(c.EnvID(), resolved.Name), url.Values{"force": []string{strconv.FormatBool(opts.forceFlag)}}),
				FailureMessage: "failed to delete volume",
				SuccessMessage: fmt.Sprintf("Volume %s deleted successfully", resolved.Name),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Force removal of a volume that is in use and skip the confirmation prompt")
	return cmd
}

func newCountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "counts",
		Short:        "Get volume usage counts",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[any]{
				Method:         http.MethodGet,
				Path:           types.VolumesCounts(c.EnvID()),
				FailureMessage: "failed to get volume counts",
				Render: func(data any) error {
					output.Header("Volume Usage Counts")
					return cmdutil.PrintJSON(data)
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
		Short:        "Remove unused volumes",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.forceFlag, "Are you sure you want to prune unused volumes?")
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[any]{
				Method:         http.MethodPost,
				Path:           types.VolumesPrune(c.EnvID()),
				FailureMessage: "failed to prune volumes",
				Render:         func(any) error { output.Success("%s", "Volumes pruned successfully"); return nil },
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Force prune without confirmation")
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	return cmd
}

func newSizesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sizes",
		Short:        "Get volume sizes",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[any]{
				Method:         http.MethodGet,
				Path:           types.VolumesSizes(c.EnvID()),
				FailureMessage: "failed to get volume sizes",
				Render: func(data any) error {
					output.Header("Volume Sizes")
					return cmdutil.PrintJSON(data)
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	return cmd
}

func newUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "usage <volume-name>",
		Short:        "Get specific volume usage",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[any]{
				Method:         http.MethodGet,
				Path:           types.VolumeUsage(c.EnvID(), resolved.Name),
				FailureMessage: "failed to get volume usage",
				Render: func(data any) error {
					output.Header("Volume Usage: %s", resolved.Name)
					return cmdutil.PrintJSON(data)
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		volumeCreateName   string
		volumeCreateDriver string
		volumeCreateOpts   []string
		volumeCreateLabels []string
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a volume",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			var driverOpts, labels map[string]string

			if len(opts.volumeCreateOpts) > 0 {
				driverOpts = make(map[string]string)
				for _, opt := range opts.volumeCreateOpts {
					parts := strings.SplitN(opt, "=", 2)
					if len(parts) == 2 {
						driverOpts[parts[0]] = parts[1]
					}
				}
			}

			if len(opts.volumeCreateLabels) > 0 {
				labels = make(map[string]string)
				for _, lbl := range opts.volumeCreateLabels {
					parts := strings.SplitN(lbl, "=", 2)
					if len(parts) == 2 {
						labels[parts[0]] = parts[1]
					}
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[volume.Volume]{
				Method: http.MethodPost,
				Path:   types.Volumes(c.EnvID()),
				Body: volume.Create{
					Name:       opts.volumeCreateName,
					Driver:     opts.volumeCreateDriver,
					DriverOpts: driverOpts,
					Labels:     labels,
				},
				FailureMessage: "failed to create volume",
				Render: func(data volume.Volume) error {
					output.Success("Volume %s created successfully", data.Name)
					output.KeyValue("Name", data.Name)
					output.KeyValue("Driver", data.Driver)
					output.KeyValue("Mountpoint", data.Mountpoint)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.volumeCreateName, "name", "", "Volume name")
	cmd.Flags().StringVar(&opts.volumeCreateDriver, "driver", "", "Volume driver")
	cmd.Flags().StringArrayVar(&opts.volumeCreateOpts, "opt", nil, "Driver option (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&opts.volumeCreateLabels, "label", nil, "Label (KEY=VALUE)")
	_ = cmd.MarkFlagRequired("name")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

var volumeRef = cmdutil.ResourceRef[volume.Volume, volume.Volume]{
	EnvironmentScoped: true,
	Singular:          "volume",
	Plural:            "volumes",
	IDHint:            "the volume name",
	ListCmd:           "arcane volumes list",
	GetPath:           types.Volume,
	IdentifierOf:      func(data volume.Volume) string { return data.Name },
	ListPath:          types.Volumes,
	Matches: func(item volume.Volume, identifierLower, original string) bool {
		idLower := strings.ToLower(item.ID)
		return strings.EqualFold(item.Name, original) || strings.Contains(strings.ToLower(item.Name), identifierLower) ||
			idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower))
	},
	Label: func(match volume.Volume) string {
		if match.Driver != "" {
			return fmt.Sprintf("%s (%s)", match.Name, match.Driver)
		}
		return match.Name
	},
	Promote: func(match volume.Volume) *volume.Volume { return &match },
}
