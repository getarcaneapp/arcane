package podman

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	internaltypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func newQuadletsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "quadlets",
		Aliases: []string{"quadlet", "q"},
		Short:   "Manage Podman quadlets",
	}
	cmd.AddCommand(
		newQuadletListCmd(),
		newQuadletGetCmd(),
		newQuadletInstallCmd(),
		newQuadletDeleteCmd(),
	)
	for _, action := range []struct{ name, verb string }{
		{name: "start", verb: "Start"},
		{name: "stop", verb: "Stop"},
		{name: "restart", verb: "Restart"},
	} {
		cmd.AddCommand(&cobra.Command{
			Use:          action.name + " <name>",
			Short:        action.verb + " a quadlet's systemd unit",
			Args:         cobra.ExactArgs(1),
			SilenceUsage: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := cmdutil.ClientFromCommand(cmd)
				if err != nil {
					return err
				}

				return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodmanUnitStatus]{
					Method:         http.MethodPost,
					Path:           internaltypes.PodmanQuadletAction(c.EnvID(), args[0], action.name),
					FailureMessage: "failed to " + action.name + " quadlet",
					Render: func(data types.PodmanUnitStatus) error {
						output.Success("Quadlet %s: %s completed", args[0], action.name)
						output.KeyValue("Unit", data.Name)
						output.KeyValue("State", output.TintStatus(data.ActiveState)+" ("+data.SubState+")")
						return nil
					},
				})
			},
		})
	}
	return cmd
}

func newQuadletListCmd() *cobra.Command {
	var opts struct {
		names    []string
		pods     []string
		statuses []string
		limit    int
		start    int
		all      bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List quadlets",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			query := url.Values{
				"name":   opts.names,
				"pod":    opts.pods,
				"status": opts.statuses,
			}.Encode()
			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.QuadletDetail]{
				Resource: "quadlets",
				Endpoint: internaltypes.PodmanQuadlets(c.EnvID()) + kit.Ternary(query != "", "?"+query, ""),
				Params: cmdutil.ListParams{
					Resource:        "quadlets",
					Limit:           opts.limit,
					FallbackDefault: 20,
					Start:           opts.start,
					All:             opts.all,
				},
				Headers: []string{"NAME", "UNIT", "TYPE", "STATUS", "APP"},
				Row: func(quadlet types.QuadletDetail) []string {
					return []string{
						quadlet.Name,
						quadlet.UnitName,
						string(quadlet.Type),
						output.TintStatus(quadlet.Status),
						cmp.Or(quadlet.App, "-"),
					}
				},
			})
		},
	}
	cmd.Flags().StringArrayVar(&opts.names, "name", nil, "Filter by quadlet name")
	cmd.Flags().StringArrayVar(&opts.pods, "pod", nil, "Filter by pod (Podman 6.0+)")
	cmd.Flags().StringArrayVar(&opts.statuses, "status", nil, "Filter by status (Podman 6.0+)")
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of quadlets to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newQuadletGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <name>",
		Aliases:      []string{"inspect"},
		Short:        "Show quadlet details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.QuadletDetail]{
				Method:         http.MethodGet,
				Path:           internaltypes.PodmanQuadlet(c.EnvID(), args[0]),
				FailureMessage: "failed to get quadlet",
				Render: func(data types.QuadletDetail) error {
					output.Header("Quadlet Details")
					output.KeyValue("Name", data.Name)
					output.KeyValue("Unit", data.UnitName)
					output.KeyValue("Type", data.Type)
					output.KeyValue("Path", data.Path)
					output.KeyValue("Status", output.TintStatus(data.Status))
					output.KeyValue("App", cmp.Or(data.App, "-"))
					output.KeyValue("Template", kit.Ternary(data.Template, "Yes", "No"))
					if data.UnitStatus != nil {
						output.KeyValue("Load state", data.UnitStatus.LoadState)
						output.KeyValue("Active state", output.TintStatus(data.UnitStatus.ActiveState)+" ("+data.UnitStatus.SubState+")")
					}
					if len(data.Containers) == 0 {
						return nil
					}
					rows := make([][]string, len(data.Containers))
					for i, container := range data.Containers {
						rows[i] = []string{
							container.ID[:min(len(container.ID), 12)],
							strings.Join(container.Names, ", "),
							container.Image,
							output.TintStatus(container.State),
						}
					}
					return cmdutil.PrintTable(cmd, []string{"ID", "NAME", "IMAGE", "STATE"}, rows)
				},
			})
		},
	}
}

func newQuadletInstallCmd() *cobra.Command {
	var opts struct {
		app        string
		extraFiles []string
	}
	cmd := &cobra.Command{
		Use:          "install <file>",
		Short:        "Install a quadlet file",
		Long:         "Install a quadlet file. The file name, including its extension (.container, .pod, .kube, .network, .volume, .image, .build), becomes the quadlet name.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read quadlet file: %w", err)
			}
			extraFiles := make([]types.QuadletFile, len(opts.extraFiles))
			for i, extraPath := range opts.extraFiles {
				extraContent, readErr := os.ReadFile(extraPath)
				if readErr != nil {
					return fmt.Errorf("failed to read extra file: %w", readErr)
				}
				extraFiles[i] = types.QuadletFile{
					Name:    filepath.Base(extraPath),
					Content: string(extraContent),
				}
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.QuadletInstallReport]{
				Method: http.MethodPost,
				Path:   internaltypes.PodmanQuadlets(c.EnvID()),
				Body: types.QuadletInstallRequest{
					Name:       filepath.Base(args[0]),
					Content:    string(content),
					ExtraFiles: extraFiles,
					App:        opts.app,
				},
				FailureMessage: "failed to install quadlet",
				Render: func(data types.QuadletInstallReport) error {
					for _, name := range slices.Sorted(maps.Keys(data.InstalledQuadlets)) {
						output.Success("Installed %s at %s", name, data.InstalledQuadlets[name])
					}
					for _, name := range slices.Sorted(maps.Keys(data.QuadletErrors)) {
						output.Warning("%s: %s", name, data.QuadletErrors[name])
					}
					if len(data.QuadletErrors) > 0 {
						return fmt.Errorf("%d quadlet file(s) failed to install", len(data.QuadletErrors))
					}
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.app, "app", "", "Application the quadlet belongs to (Podman 6.1+)")
	cmd.Flags().StringArrayVar(&opts.extraFiles, "extra-file", nil, "Additional file installed beside the quadlet, such as kube YAML (repeatable)")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newQuadletDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <name>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a quadlet",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete quadlet %s?", args[0]),
				Path:           internaltypes.PodmanQuadlet(c.EnvID(), args[0]),
				FailureMessage: "failed to delete quadlet",
				SuccessMessage: fmt.Sprintf("Quadlet %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}
