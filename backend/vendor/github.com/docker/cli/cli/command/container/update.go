package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/opts"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type updateOptions struct {
	blkioWeight        uint16
	cpuPeriod          int64
	cpuQuota           int64
	cpuRealtimePeriod  int64
	cpuRealtimeRuntime int64
	cpusetCpus         string
	cpusetMems         string
	cpuShares          int64
	memory             opts.MemBytes
	memoryReservation  opts.MemBytes
	memorySwap         opts.MemSwapBytes
	kernelMemory       opts.MemBytes
	restartPolicy      string
	pidsLimit          int64
	cpus               opts.NanoCPUs

	nFlag int

	containers []string
}

// NewUpdateCommand creates a new cobra.Command for `docker update`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewUpdateCommand(dockerCLI command.Cli) *cobra.Command {
	return newUpdateCommand(dockerCLI)
}

func newUpdateCommand(dockerCLI command.Cli) *cobra.Command {
	var options updateOptions

	cmd := &cobra.Command{
		Use:   "update [OPTIONS] CONTAINER [CONTAINER...]",
		Short: "Update configuration of one or more containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.containers = args
			options.nFlag = cmd.Flags().NFlag()
			return runUpdate(cmd.Context(), dockerCLI, &options)
		},
		Annotations: map[string]string{
			"aliases": "docker container update, docker update",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, true),
	}

	flags := cmd.Flags()
	flags.Uint16Var(&options.blkioWeight, "blkio-weight", 0, `Block IO (relative weight), between 10 and 1000, or 0 to disable (default 0)`)
	flags.Int64Var(&options.cpuPeriod, "cpu-period", 0, "Limit CPU CFS (Completely Fair Scheduler) period")
	flags.Int64Var(&options.cpuQuota, "cpu-quota", 0, "Limit CPU CFS (Completely Fair Scheduler) quota")
	flags.Int64Var(&options.cpuRealtimePeriod, "cpu-rt-period", 0, "Limit the CPU real-time period in microseconds")
	flags.SetAnnotation("cpu-rt-period", "version", []string{"1.25"})
	flags.Int64Var(&options.cpuRealtimeRuntime, "cpu-rt-runtime", 0, "Limit the CPU real-time runtime in microseconds")
	flags.SetAnnotation("cpu-rt-runtime", "version", []string{"1.25"})
	flags.StringVar(&options.cpusetCpus, "cpuset-cpus", "", "CPUs in which to allow execution (0-3, 0,1)")
	flags.StringVar(&options.cpusetMems, "cpuset-mems", "", "MEMs in which to allow execution (0-3, 0,1)")
	flags.Int64VarP(&options.cpuShares, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	flags.VarP(&options.memory, "memory", "m", "Memory limit")
	flags.Var(&options.memoryReservation, "memory-reservation", "Memory soft limit")
	flags.Var(&options.memorySwap, "memory-swap", `Swap limit equal to memory plus swap: -1 to enable unlimited swap`)
	flags.Var(&options.kernelMemory, "kernel-memory", "Kernel memory limit (deprecated)")
	// --kernel-memory is deprecated on API v1.42 and up, but our current annotations
	// do not support only showing on < API-version. This option is no longer supported
	// by runc, so hiding it unconditionally.
	flags.SetAnnotation("kernel-memory", "deprecated", nil)
	flags.MarkHidden("kernel-memory")

	flags.StringVar(&options.restartPolicy, "restart", "", "Restart policy to apply when a container exits")
	flags.Int64Var(&options.pidsLimit, "pids-limit", 0, `Tune container pids limit (set -1 for unlimited)`)
	flags.SetAnnotation("pids-limit", "version", []string{"1.40"})

	flags.Var(&options.cpus, "cpus", "Number of CPUs")
	flags.SetAnnotation("cpus", "version", []string{"1.29"})

	_ = cmd.RegisterFlagCompletionFunc("restart", completeRestartPolicies)

	return cmd
}

func runUpdate(ctx context.Context, dockerCli command.Cli, options *updateOptions) error {
	var err error

	if options.nFlag == 0 {
		return errors.New("you must provide one or more flags when using this command")
	}

	var restartPolicy containertypes.RestartPolicy
	if options.restartPolicy != "" {
		restartPolicy, err = opts.ParseRestartPolicy(options.restartPolicy)
		if err != nil {
			return err
		}
	}

	resources := containertypes.Resources{
		BlkioWeight:        options.blkioWeight,
		CpusetCpus:         options.cpusetCpus,
		CpusetMems:         options.cpusetMems,
		CPUShares:          options.cpuShares,
		Memory:             options.memory.Value(),
		MemoryReservation:  options.memoryReservation.Value(),
		MemorySwap:         options.memorySwap.Value(),
		KernelMemory:       options.kernelMemory.Value(),
		CPUPeriod:          options.cpuPeriod,
		CPUQuota:           options.cpuQuota,
		CPURealtimePeriod:  options.cpuRealtimePeriod,
		CPURealtimeRuntime: options.cpuRealtimeRuntime,
		NanoCPUs:           options.cpus.Value(),
	}

	if options.pidsLimit != 0 {
		resources.PidsLimit = &options.pidsLimit
	}

	updateConfig := containertypes.UpdateConfig{
		Resources:     resources,
		RestartPolicy: restartPolicy,
	}

	var (
		warns []string
		errs  []string
	)
	for _, ctr := range options.containers {
		r, err := dockerCli.Client().ContainerUpdate(ctx, ctr, updateConfig)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			_, _ = fmt.Fprintln(dockerCli.Out(), ctr)
		}
		warns = append(warns, r.Warnings...)
	}
	if len(warns) > 0 {
		_, _ = fmt.Fprintln(dockerCli.Out(), strings.Join(warns, "\n"))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}
