package image

import (
	"context"
	"errors"
	"fmt"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/trust"
	"github.com/spf13/cobra"
)

// pullOptions defines what and how to pull.
type pullOptions struct {
	remote    string
	all       bool
	platform  string
	quiet     bool
	untrusted bool
}

// NewPullCommand creates a new `docker pull` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPullCommand(dockerCLI command.Cli) *cobra.Command {
	return newPullCommand(dockerCLI)
}

// newPullCommand creates a new `docker pull` command
func newPullCommand(dockerCLI command.Cli) *cobra.Command {
	var opts pullOptions

	cmd := &cobra.Command{
		Use:   "pull [OPTIONS] NAME[:TAG|@DIGEST]",
		Short: "Download an image from a registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.remote = args[0]
			return runPull(cmd.Context(), dockerCLI, opts)
		},
		Annotations: map[string]string{
			"category-top": "5",
			"aliases":      "docker image pull, docker pull",
		},
		// Complete with local images to help pulling the latest version
		// of images that are in the image cache.
		ValidArgsFunction: completion.ImageNames(dockerCLI, 1),
	}

	flags := cmd.Flags()

	flags.BoolVarP(&opts.all, "all-tags", "a", false, "Download all tagged images in the repository")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress verbose output")

	addPlatformFlag(flags, &opts.platform)
	flags.BoolVar(&opts.untrusted, "disable-content-trust", !trust.Enabled(), "Skip image verification")

	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)

	return cmd
}

// runPull performs a pull against the engine based on the specified options
func runPull(ctx context.Context, dockerCLI command.Cli, opts pullOptions) error {
	distributionRef, err := reference.ParseNormalizedNamed(opts.remote)
	switch {
	case err != nil:
		return err
	case opts.all && !reference.IsNameOnly(distributionRef):
		return errors.New("tag can't be used with --all-tags/-a")
	case !opts.all && reference.IsNameOnly(distributionRef):
		distributionRef = reference.TagNameOnly(distributionRef)
		if tagged, ok := distributionRef.(reference.Tagged); ok && !opts.quiet {
			_, _ = fmt.Fprintln(dockerCLI.Out(), "Using default tag:", tagged.Tag())
		}
	}

	imgRefAndAuth, err := trust.GetImageReferencesAndAuth(ctx, authResolver(dockerCLI), distributionRef.String())
	if err != nil {
		return err
	}

	// Check if reference has a digest
	_, isCanonical := distributionRef.(reference.Canonical)
	if !opts.untrusted && !isCanonical {
		if err := trustedPull(ctx, dockerCLI, imgRefAndAuth, opts); err != nil {
			return err
		}
	} else {
		if err := imagePullPrivileged(ctx, dockerCLI, imgRefAndAuth, opts); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintln(dockerCLI.Out(), imgRefAndAuth.Reference().String())
	return nil
}
