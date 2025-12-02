package image

import (
	"context"
	"errors"
	"fmt"

	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/docker/api/types/image"
	"github.com/spf13/cobra"
)

type removeOptions struct {
	force     bool
	noPrune   bool
	platforms []string
}

// NewRemoveCommand creates a new `docker remove` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewRemoveCommand(dockerCLI command.Cli) *cobra.Command {
	return newRemoveCommand(dockerCLI)
}

// newRemoveCommand creates a new `docker remove` command
func newRemoveCommand(dockerCLI command.Cli) *cobra.Command {
	var options removeOptions

	cmd := &cobra.Command{
		Use:   "rmi [OPTIONS] IMAGE [IMAGE...]",
		Short: "Remove one or more images",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd.Context(), dockerCLI, options, args)
		},
		ValidArgsFunction: completion.ImageNames(dockerCLI, -1),
		Annotations: map[string]string{
			"aliases": "docker image rm, docker image remove, docker rmi",
		},
	}

	flags := cmd.Flags()

	flags.BoolVarP(&options.force, "force", "f", false, "Force removal of the image")
	flags.BoolVar(&options.noPrune, "no-prune", false, "Do not delete untagged parents")

	// TODO(thaJeztah): create a "platforms" option for this (including validation / parsing).
	flags.StringSliceVar(&options.platforms, "platform", nil, `Remove only the given platform variant. Formatted as "os[/arch[/variant]]" (e.g., "linux/amd64")`)
	_ = flags.SetAnnotation("platform", "version", []string{"1.50"})

	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)
	return cmd
}

// newImageRemoveCommand is a sub-command under `image` (`docker image rm`)
func newImageRemoveCommand(dockerCli command.Cli) *cobra.Command {
	cmd := *newRemoveCommand(dockerCli)
	cmd.Aliases = []string{"rmi", "remove"}
	cmd.Use = "rm [OPTIONS] IMAGE [IMAGE...]"
	return &cmd
}

func runRemove(ctx context.Context, dockerCLI command.Cli, opts removeOptions, images []string) error {
	apiClient := dockerCLI.Client()

	options := image.RemoveOptions{
		Force:         opts.force,
		PruneChildren: !opts.noPrune,
	}

	for _, v := range opts.platforms {
		p, err := platforms.Parse(v)
		if err != nil {
			return err
		}
		options.Platforms = append(options.Platforms, p)
	}

	// TODO(thaJeztah): this logic can likely be simplified: do we want to print "not found" errors at all when using "force"?
	fatalErr := false
	var errs []error
	for _, img := range images {
		dels, err := apiClient.ImageRemove(ctx, img, options)
		if err != nil {
			if !errdefs.IsNotFound(err) {
				fatalErr = true
			}
			errs = append(errs, err)
		} else {
			for _, del := range dels {
				if del.Deleted != "" {
					_, _ = fmt.Fprintln(dockerCLI.Out(), "Deleted:", del.Deleted)
				} else {
					_, _ = fmt.Fprintln(dockerCLI.Out(), "Untagged:", del.Untagged)
				}
			}
		}
	}

	if err := errors.Join(errs...); err != nil {
		if !opts.force || fatalErr {
			return err
		}
		_, _ = fmt.Fprintln(dockerCLI.Err(), err)
	}
	return nil
}
