package image

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/cli/cli/trust"
	"github.com/docker/cli/internal/jsonstream"
	"github.com/docker/cli/internal/registry"
	"github.com/docker/docker/api/types/image"
	registrytypes "github.com/docker/docker/api/types/registry"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/theupdateframework/notary/client"
	"github.com/theupdateframework/notary/tuf/data"
)

type target struct {
	name   string
	digest digest.Digest
	size   int64
}

// notaryClientProvider is used in tests to provide a dummy notary client.
type notaryClientProvider interface {
	NotaryClient(imgRefAndAuth trust.ImageRefAndAuth, actions []string) (client.Repository, error)
}

// newNotaryClient provides a Notary Repository to interact with signed metadata for an image.
func newNotaryClient(cli command.Streams, imgRefAndAuth trust.ImageRefAndAuth) (client.Repository, error) {
	if ncp, ok := cli.(notaryClientProvider); ok {
		// notaryClientProvider is used in tests to provide a dummy notary client.
		return ncp.NotaryClient(imgRefAndAuth, []string{"pull"})
	}
	return trust.GetNotaryRepository(cli.In(), cli.Out(), command.UserAgent(), imgRefAndAuth.RepoInfo(), imgRefAndAuth.AuthConfig(), "pull")
}

// pushTrustedReference pushes a canonical reference to the trust server.
func pushTrustedReference(ctx context.Context, dockerCLI command.Cli, ref reference.Named, responseBody io.Reader) error {
	// Resolve the Repository name from fqn to RepositoryInfo, and create an
	// IndexInfo. Docker Content Trust uses the IndexInfo.Official field to
	// select the right domain for Docker Hub's Notary server;
	// https://github.com/docker/cli/blob/v28.4.0/cli/trust/trust.go#L65-L79
	indexInfo := registry.NewIndexInfo(ref)
	repoInfo := &trust.RepositoryInfo{
		Name:  reference.TrimNamed(ref),
		Index: indexInfo,
	}
	authConfig := command.ResolveAuthConfig(dockerCLI.ConfigFile(), indexInfo)
	return trust.PushTrustedReference(ctx, dockerCLI, repoInfo, ref, authConfig, responseBody, command.UserAgent())
}

// trustedPull handles content trust pulling of an image
func trustedPull(ctx context.Context, cli command.Cli, imgRefAndAuth trust.ImageRefAndAuth, opts pullOptions) error {
	refs, err := getTrustedPullTargets(cli, imgRefAndAuth)
	if err != nil {
		return err
	}

	ref := imgRefAndAuth.Reference()
	for i, r := range refs {
		displayTag := r.name
		if displayTag != "" {
			displayTag = ":" + displayTag
		}
		_, _ = fmt.Fprintf(cli.Out(), "Pull (%d of %d): %s%s@%s\n", i+1, len(refs), reference.FamiliarName(ref), displayTag, r.digest)

		trustedRef, err := reference.WithDigest(reference.TrimNamed(ref), r.digest)
		if err != nil {
			return err
		}
		updatedImgRefAndAuth, err := trust.GetImageReferencesAndAuth(ctx, authResolver(cli), trustedRef.String())
		if err != nil {
			return err
		}
		if err := imagePullPrivileged(ctx, cli, updatedImgRefAndAuth, pullOptions{
			all:      false,
			platform: opts.platform,
			quiet:    opts.quiet,
			remote:   opts.remote,
		}); err != nil {
			return err
		}

		tagged, err := reference.WithTag(reference.TrimNamed(ref), r.name)
		if err != nil {
			return err
		}

		// Use familiar references when interacting with client and output
		familiarRef := reference.FamiliarString(tagged)
		trustedFamiliarRef := reference.FamiliarString(trustedRef)
		_, _ = fmt.Fprintf(cli.Err(), "Tagging %s as %s\n", trustedFamiliarRef, familiarRef)
		if err := cli.Client().ImageTag(ctx, trustedFamiliarRef, familiarRef); err != nil {
			return err
		}
	}
	return nil
}

func getTrustedPullTargets(cli command.Cli, imgRefAndAuth trust.ImageRefAndAuth) ([]target, error) {
	notaryRepo, err := newNotaryClient(cli, imgRefAndAuth)
	if err != nil {
		return nil, errors.Wrap(err, "error establishing connection to trust repository")
	}

	ref := imgRefAndAuth.Reference()
	tagged, isTagged := ref.(reference.NamedTagged)
	if !isTagged {
		// List all targets
		targets, err := notaryRepo.ListTargets(trust.ReleasesRole, data.CanonicalTargetsRole)
		if err != nil {
			return nil, trust.NotaryError(ref.Name(), err)
		}
		var refs []target
		for _, tgt := range targets {
			t, err := convertTarget(tgt.Target)
			if err != nil {
				_, _ = fmt.Fprintf(cli.Err(), "Skipping target for %q\n", reference.FamiliarName(ref))
				continue
			}
			// Only list tags in the top level targets role or the releases delegation role - ignore
			// all other delegation roles
			if tgt.Role != trust.ReleasesRole && tgt.Role != data.CanonicalTargetsRole {
				continue
			}
			refs = append(refs, t)
		}
		if len(refs) == 0 {
			return nil, trust.NotaryError(ref.Name(), errors.Errorf("No trusted tags for %s", ref.Name()))
		}
		return refs, nil
	}

	t, err := notaryRepo.GetTargetByName(tagged.Tag(), trust.ReleasesRole, data.CanonicalTargetsRole)
	if err != nil {
		return nil, trust.NotaryError(ref.Name(), err)
	}
	// Only get the tag if it's in the top level targets role or the releases delegation role
	// ignore it if it's in any other delegation roles
	if t.Role != trust.ReleasesRole && t.Role != data.CanonicalTargetsRole {
		return nil, trust.NotaryError(ref.Name(), errors.Errorf("No trust data for %s", tagged.Tag()))
	}

	logrus.Debugf("retrieving target for %s role", t.Role)
	r, err := convertTarget(t.Target)
	return []target{r}, err
}

// imagePullPrivileged pulls the image and displays it to the output
func imagePullPrivileged(ctx context.Context, cli command.Cli, imgRefAndAuth trust.ImageRefAndAuth, opts pullOptions) error {
	encodedAuth, err := registrytypes.EncodeAuthConfig(*imgRefAndAuth.AuthConfig())
	if err != nil {
		return err
	}
	responseBody, err := cli.Client().ImagePull(ctx, reference.FamiliarString(imgRefAndAuth.Reference()), image.PullOptions{
		RegistryAuth:  encodedAuth,
		PrivilegeFunc: nil,
		All:           opts.all,
		Platform:      opts.platform,
	})
	if err != nil {
		return err
	}
	defer responseBody.Close()

	out := cli.Out()
	if opts.quiet {
		out = streams.NewOut(io.Discard)
	}
	return jsonstream.Display(ctx, responseBody, out)
}

// TrustedReference returns the canonical trusted reference for an image reference
func TrustedReference(ctx context.Context, cli command.Cli, ref reference.NamedTagged) (reference.Canonical, error) {
	imgRefAndAuth, err := trust.GetImageReferencesAndAuth(ctx, authResolver(cli), ref.String())
	if err != nil {
		return nil, err
	}

	notaryRepo, err := newNotaryClient(cli, imgRefAndAuth)
	if err != nil {
		return nil, errors.Wrap(err, "error establishing connection to trust repository")
	}

	t, err := notaryRepo.GetTargetByName(ref.Tag(), trust.ReleasesRole, data.CanonicalTargetsRole)
	if err != nil {
		return nil, trust.NotaryError(imgRefAndAuth.RepoInfo().Name.Name(), err)
	}
	// Only list tags in the top level targets role or the releases delegation role - ignore
	// all other delegation roles
	if t.Role != trust.ReleasesRole && t.Role != data.CanonicalTargetsRole {
		return nil, trust.NotaryError(imgRefAndAuth.RepoInfo().Name.Name(), client.ErrNoSuchTarget(ref.Tag()))
	}
	r, err := convertTarget(t.Target)
	if err != nil {
		return nil, err
	}
	return reference.WithDigest(reference.TrimNamed(ref), r.digest)
}

func convertTarget(t client.Target) (target, error) {
	h, ok := t.Hashes["sha256"]
	if !ok {
		return target{}, errors.New("no valid hash, expecting sha256")
	}
	return target{
		name:   t.Name,
		digest: digest.NewDigestFromHex("sha256", hex.EncodeToString(h)),
		size:   t.Length,
	}, nil
}

// AuthResolver returns an auth resolver function from a [config.Provider].
//
// Deprecated: this function was only used internally and will be removed in the next release.
func AuthResolver(dockerCLI config.Provider) func(ctx context.Context, index *registrytypes.IndexInfo) registrytypes.AuthConfig {
	return authResolver(dockerCLI)
}

// authResolver returns an auth resolver function from a [config.Provider].
func authResolver(dockerCLI config.Provider) func(ctx context.Context, index *registrytypes.IndexInfo) registrytypes.AuthConfig {
	return func(ctx context.Context, index *registrytypes.IndexInfo) registrytypes.AuthConfig {
		return command.ResolveAuthConfig(dockerCLI.ConfigFile(), index)
	}
}
