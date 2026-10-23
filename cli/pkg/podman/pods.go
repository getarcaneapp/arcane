package podman

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	internaltypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func newPodsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pods",
		Aliases: []string{"pod"},
		Short:   "Manage Podman pods",
	}
	cmd.AddCommand(
		newPodListCmd(),
		newPodInspectCmd(),
		newPodCreateCmd(),
		newPodDeleteCmd(),
	)
	for _, action := range []struct{ name, verb string }{
		{name: "start", verb: "Start"},
		{name: "stop", verb: "Stop"},
		{name: "restart", verb: "Restart"},
	} {
		var timeout int
		actionCmd := &cobra.Command{
			Use:          action.name + " <pod>",
			Short:        action.verb + " a pod",
			Args:         cobra.ExactArgs(1),
			SilenceUsage: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := cmdutil.ClientFromCommand(cmd)
				if err != nil {
					return err
				}

				return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
					Method:         http.MethodPost,
					Path:           internaltypes.PodmanPodAction(c.EnvID(), args[0], action.name),
					Body:           types.PodActionRequest{Timeout: kit.Ternary(cmd.Flags().Changed("timeout"), new(timeout), nil)},
					FailureMessage: "failed to " + action.name + " pod",
					Render: func(types.MessageResponse) error {
						output.Success("Pod %s: %s completed", args[0], action.name)
						return nil
					},
				})
			},
		}
		cmdutil.SetResource(podRef, actionCmd)
		if action.name == "stop" {
			actionCmd.Flags().IntVarP(&timeout, "timeout", "t", 10, "Seconds to wait before killing the pod")
		}
		cmd.AddCommand(actionCmd)
	}
	return cmd
}

func newPodListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List pods",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Pod]{
				Resource: "pods",
				Endpoint: internaltypes.PodmanPods(c.EnvID()),
				Params: cmdutil.ListParams{
					Resource:        "pods",
					Limit:           opts.limit,
					FallbackDefault: 20,
					Start:           opts.start,
					All:             opts.all,
				},
				Headers: []string{"ID", "NAME", "STATUS", "CONTAINERS", "CREATED"},
				Row: func(pod types.Pod) []string {
					return []string{
						pod.ID[:min(len(pod.ID), 12)],
						pod.Name,
						output.TintStatus(pod.Status),
						strconv.Itoa(len(pod.Containers)),
						pod.Created.Format("2006-01-02 15:04"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of pods to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newPodInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "inspect <pod>",
		Aliases:      []string{"get"},
		Short:        "Show pod details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodInspect]{
				Method:         http.MethodGet,
				Path:           internaltypes.PodmanPod(c.EnvID(), args[0]),
				FailureMessage: "failed to inspect pod",
				Render: func(data types.PodInspect) error {
					output.Header("Pod Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("State", output.TintStatus(data.State))
					output.KeyValue("Created", data.Created.Format("2006-01-02 15:04"))
					output.KeyValue("Hostname", cmp.Or(data.Hostname, "-"))
					output.KeyValue("Infra container", cmp.Or(data.InfraContainerID[:min(len(data.InfraContainerID), 12)], "-"))
					output.KeyValue("Shared namespaces", cmp.Or(strings.Join(data.SharedNamespaces, ", "), "-"))
					output.KeyValue("Containers", data.NumContainers)
					if len(data.Containers) == 0 {
						return nil
					}
					rows := make([][]string, len(data.Containers))
					for i, container := range data.Containers {
						rows[i] = []string{container.ID[:min(len(container.ID), 12)], container.Name, output.TintStatus(container.State)}
					}
					return cmdutil.PrintTable(cmd, []string{"ID", "NAME", "STATE"}, rows)
				},
			})
		},
	}
	cmdutil.SetResource(podRef, cmd)
	return cmd
}

func newPodDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <pod>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a pod",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete pod %s?", args[0]),
				Path:           cmdutil.AppendQuery(internaltypes.PodmanPod(c.EnvID(), args[0]), url.Values{"force": []string{strconv.FormatBool(opts.force)}}),
				FailureMessage: "failed to delete pod",
				SuccessMessage: fmt.Sprintf("Pod %s deleted successfully", args[0]),
			})
		},
	}
	cmdutil.SetResource(podRef, cmd)
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Stop and remove running containers and skip the confirmation prompt")
	return cmd
}

var podRef = cmdutil.ResourceRef[types.PodInspect, types.Pod]{
	EnvironmentScoped: true,
	Singular:          "pod",
	Plural:            "pods",
	IDHint:            "the pod ID",
	ListCmd:           "arcane podman pods list",
	SearchCandidates: func(ctx context.Context, c *client.Client, identifier string) ([]types.Pod, error) {
		return searchPodman(ctx, c, internaltypes.PodmanPods(c.EnvID()), func(pod types.Pod) bool {
			return strings.EqualFold(pod.Name, identifier) || cmdutil.LooksLikeIDPrefix(strings.ToLower(identifier)) && strings.HasPrefix(pod.ID, strings.ToLower(identifier))
		})
	},
	Label: func(pod types.Pod) string {
		return fmt.Sprintf("%s (%s)", pod.Name, pod.ID[:min(len(pod.ID), 12)])
	},
	Promote: func(pod types.Pod) *types.PodInspect {
		return &types.PodInspect{
			ID:   pod.ID,
			Name: pod.Name,
		}
	},
	IdentifierOf: func(pod types.PodInspect) string {
		return pod.Name
	},
	Exact: func(pod types.Pod, identifierLower, original string) bool {
		return strings.EqualFold(pod.Name, original) || pod.ID == identifierLower
	},
}
