// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/cli/cli/trust"
	"github.com/docker/cli/internal/jsonstream"
	"github.com/docker/cli/internal/tui"
	"github.com/docker/docker/api/types/auxprogress"
	"github.com/docker/docker/api/types/image"
	"github.com/morikuni/aec"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type pushOptions struct {
	all       bool
	remote    string
	untrusted bool
	quiet     bool
	platform  string
}

// NewPushCommand creates a new `docker push` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPushCommand(dockerCLI command.Cli) *cobra.Command {
	return newPushCommand(dockerCLI)
}

// newPushCommand creates a new `docker push` command
func newPushCommand(dockerCLI command.Cli) *cobra.Command {
	var opts pushOptions

	cmd := &cobra.Command{
		Use:   "push [OPTIONS] NAME[:TAG]",
		Short: "Upload an image to a registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.remote = args[0]
			return runPush(cmd.Context(), dockerCLI, opts)
		},
		Annotations: map[string]string{
			"category-top": "6",
			"aliases":      "docker image push, docker push",
		},
		ValidArgsFunction: completion.ImageNames(dockerCLI, 1),
	}

	flags := cmd.Flags()
	flags.BoolVarP(&opts.all, "all-tags", "a", false, "Push all tags of an image to the repository")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress verbose output")
	flags.BoolVar(&opts.untrusted, "disable-content-trust", !trust.Enabled(), "Skip image signing")

	// Don't default to DOCKER_DEFAULT_PLATFORM env variable, always default to
	// pushing the image as-is. This also avoids forcing the platform selection
	// on older APIs which don't support it.
	flags.StringVar(&opts.platform, "platform", "",
		`Push a platform-specific manifest as a single-platform image to the registry.
Image index won't be pushed, meaning that other manifests, including attestations won't be preserved.
'os[/arch[/variant]]': Explicit platform (eg. linux/amd64)`)
	flags.SetAnnotation("platform", "version", []string{"1.46"})

	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)

	return cmd
}

// runPush performs a push against the engine based on the specified options.
func runPush(ctx context.Context, dockerCli command.Cli, opts pushOptions) error {
	var platform *ocispec.Platform
	out := tui.NewOutput(dockerCli.Out())
	if opts.platform != "" {
		p, err := platforms.Parse(opts.platform)
		if err != nil {
			_, _ = fmt.Fprintf(dockerCli.Err(), "Invalid platform %s", opts.platform)
			return err
		}
		platform = &p

		out.PrintNote(`Using --platform pushes only the specified platform manifest of a multi-platform image index.
Other components, like attestations, will not be included.
To push the complete multi-platform image, remove the --platform flag.
`)
	}

	ref, err := reference.ParseNormalizedNamed(opts.remote)
	if err != nil {
		return err
	}

	switch {
	case opts.all && !reference.IsNameOnly(ref):
		return errors.New("tag can't be used with --all-tags/-a")
	case !opts.all && reference.IsNameOnly(ref):
		ref = reference.TagNameOnly(ref)
		if tagged, ok := ref.(reference.Tagged); ok && !opts.quiet {
			_, _ = fmt.Fprintln(dockerCli.Out(), "Using default tag:", tagged.Tag())
		}
	}

	// Resolve the Auth config relevant for this server
	encodedAuth, err := command.RetrieveAuthTokenFromImage(dockerCli.ConfigFile(), ref.String())
	if err != nil {
		return err
	}

	responseBody, err := dockerCli.Client().ImagePush(ctx, reference.FamiliarString(ref), image.PushOptions{
		All:           opts.all,
		RegistryAuth:  encodedAuth,
		PrivilegeFunc: nil,
		Platform:      platform,
	})
	if err != nil {
		return err
	}

	defer func() {
		_ = responseBody.Close()
		for _, note := range notes {
			out.PrintNote(note)
		}
	}()

	if !opts.untrusted {
		return pushTrustedReference(ctx, dockerCli, ref, responseBody)
	}

	if opts.quiet {
		err = jsonstream.Display(ctx, responseBody, streams.NewOut(io.Discard), jsonstream.WithAuxCallback(handleAux()))
		if err == nil {
			_, _ = fmt.Fprintln(dockerCli.Out(), ref.String())
		}
		return err
	}
	return jsonstream.Display(ctx, responseBody, dockerCli.Out(), jsonstream.WithAuxCallback(handleAux()))
}

var notes []string

func handleAux() func(jm jsonstream.JSONMessage) {
	return func(jm jsonstream.JSONMessage) {
		b := []byte(*jm.Aux)

		var stripped auxprogress.ManifestPushedInsteadOfIndex
		err := json.Unmarshal(b, &stripped)
		if err == nil && stripped.ManifestPushedInsteadOfIndex {
			note := fmt.Sprintf("Not all multiplatform-content is present and only the available single-platform image was pushed\n%s -> %s",
				aec.RedF.Apply(stripped.OriginalIndex.Digest.String()),
				aec.GreenF.Apply(stripped.SelectedManifest.Digest.String()),
			)
			notes = append(notes, note)
		}

		var missing auxprogress.ContentMissing
		err = json.Unmarshal(b, &missing)
		if err == nil && missing.ContentMissing {
			note := `You're trying to push a manifest list/index which 
				references multiple platform specific manifests, but not all of them are available locally
				or available to the remote repository.

				Make sure you have all the referenced content and try again.

				You can also push only a single platform specific manifest directly by specifying the platform you want to push with the --platform flag.`
			notes = append(notes, note)
		}
	}
}
