package podman

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/podman"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
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

				return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
					Method:         http.MethodPost,
					Path:           types.PodmanPodAction(c.EnvID(), args[0], action.name),
					Body:           podman.PodActionRequest{Timeout: kit.Ternary(cmd.Flags().Changed("timeout"), new(timeout), nil)},
					FailureMessage: "failed to " + action.name + " pod",
					Render: func(base.MessageResponse) error {
						output.Success("Pod %s: %s completed", args[0], action.name)
						return nil
					},
				})
			},
		}
		if action.name != "start" {
			actionCmd.Flags().IntVarP(&timeout, "timeout", "t", 10, "Seconds to wait before killing the pod")
		}
		cmd.AddCommand(actionCmd)
	}
	return cmd
}

func newPodListCmd() *cobra.Command {
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]podman.Pod]{
				Method:         http.MethodGet,
				Path:           types.PodmanPods(c.EnvID()),
				FailureMessage: "failed to list pods",
				Render: func(data []podman.Pod) error {
					rows := make([][]string, len(data))
					for i, pod := range data {
						rows[i] = []string{
							pod.ID[:min(len(pod.ID), 12)],
							pod.Name,
							output.TintStatus(pod.Status),
							strconv.Itoa(len(pod.Containers)),
							pod.Created.Format("2006-01-02 15:04"),
						}
					}
					return cmdutil.PrintTable(cmd, []string{"ID", "NAME", "STATUS", "CONTAINERS", "CREATED"}, rows, cmdutil.TableSummary{
						Resource: "pods",
						Total:    int64(len(data)),
					})
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newPodInspectCmd() *cobra.Command {
	return &cobra.Command{
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[podman.PodInspect]{
				Method:         http.MethodGet,
				Path:           types.PodmanPod(c.EnvID(), args[0]),
				FailureMessage: "failed to inspect pod",
				Render: func(data podman.PodInspect) error {
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
}

func newPodCreateCmd() *cobra.Command {
	var opts struct {
		name     string
		hostname string
		network  string
		labels   map[string]string
		publish  []string
		share    []string
		noInfra  bool
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a pod",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ports := make([]podman.PodPortMapping, 0, len(opts.publish))
			for _, spec := range opts.publish {
				mapping, protocol, _ := strings.Cut(spec, "/")
				parts := strings.Split(mapping, ":")
				if len(parts) > 3 {
					return fmt.Errorf("invalid --publish %q: expected [hostIp:][hostPort:]containerPort[/protocol]", spec)
				}
				parts = slices.Concat(make([]string, 3-len(parts)), parts)
				hostPort, hostErr := strconv.ParseUint(cmp.Or(parts[1], "0"), 10, 16)
				containerPort, containerErr := strconv.ParseUint(parts[2], 10, 16)
				if parseErr := errors.Join(hostErr, containerErr); parseErr != nil {
					return fmt.Errorf("invalid --publish %q: %w", spec, parseErr)
				}
				ports = append(ports, podman.PodPortMapping{
					ContainerPort: uint16(containerPort),
					HostPort:      uint16(hostPort),
					HostIP:        parts[0],
					Protocol:      protocol,
				})
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[podman.PodCreateResponse]{
				Method: http.MethodPost,
				Path:   types.PodmanPods(c.EnvID()),
				Body: podman.PodCreateRequest{
					Name:             opts.name,
					Labels:           opts.labels,
					Hostname:         opts.hostname,
					PortMappings:     ports,
					Network:          opts.network,
					SharedNamespaces: opts.share,
					NoInfra:          opts.noInfra,
				},
				FailureMessage: "failed to create pod",
				Render: func(data podman.PodCreateResponse) error {
					output.Success("Pod %s created successfully", opts.name)
					output.KeyValue("ID", data.ID)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "Pod name (required)")
	cmd.Flags().StringVar(&opts.hostname, "hostname", "", "Pod hostname")
	cmd.Flags().StringVar(&opts.network, "network", "", "Network to join")
	cmd.Flags().StringToStringVar(&opts.labels, "label", nil, "Label (KEY=VALUE)")
	cmd.Flags().StringArrayVarP(&opts.publish, "publish", "p", nil, "Publish a port ([hostIp:][hostPort:]containerPort[/protocol])")
	cmd.Flags().StringSliceVar(&opts.share, "share", nil, "Namespaces to share between pod members (e.g. net,ipc,uts)")
	cmd.Flags().BoolVar(&opts.noInfra, "no-infra", false, "Create the pod without an infra container")
	_ = cmd.MarkFlagRequired("name")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
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
				Path:           types.PodmanPod(c.EnvID(), args[0]) + "?" + url.Values{"force": {strconv.FormatBool(opts.force)}}.Encode(),
				FailureMessage: "failed to delete pod",
				SuccessMessage: fmt.Sprintf("Pod %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Stop and remove running containers and skip the confirmation prompt")
	return cmd
}
