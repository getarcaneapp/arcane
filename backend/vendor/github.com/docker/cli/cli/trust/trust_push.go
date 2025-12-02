package trust

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/cli/internal/jsonstream"
	"github.com/docker/docker/api/types"
	registrytypes "github.com/docker/docker/api/types/registry"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/theupdateframework/notary/client"
	"github.com/theupdateframework/notary/tuf/data"
)

// Streams is an interface which exposes the standard input and output streams.
//
// Same interface as [github.com/docker/cli/cli/command.Streams] but defined here to prevent a circular import.
type Streams interface {
	In() *streams.In
	Out() *streams.Out
	Err() *streams.Out
}

// PushTrustedReference pushes a canonical reference to the trust server.
//
//nolint:gocyclo
func PushTrustedReference(ctx context.Context, ioStreams Streams, repoInfo *RepositoryInfo, ref reference.Named, authConfig registrytypes.AuthConfig, in io.Reader, userAgent string) error {
	// If it is a trusted push we would like to find the target entry which match the
	// tag provided in the function and then do an AddTarget later.
	notaryTarget := &client.Target{}
	// Count the times of calling for handleTarget,
	// if it is called more that once, that should be considered an error in a trusted push.
	cnt := 0
	handleTarget := func(msg jsonstream.JSONMessage) {
		cnt++
		if cnt > 1 {
			// handleTarget should only be called once. This will be treated as an error.
			return
		}

		var pushResult types.PushResult
		err := json.Unmarshal(*msg.Aux, &pushResult)
		if err == nil && pushResult.Tag != "" {
			if dgst, err := digest.Parse(pushResult.Digest); err == nil {
				h, err := hex.DecodeString(dgst.Hex())
				if err != nil {
					notaryTarget = nil
					return
				}
				notaryTarget.Name = pushResult.Tag
				notaryTarget.Hashes = data.Hashes{string(dgst.Algorithm()): h}
				notaryTarget.Length = int64(pushResult.Size)
			}
		}
	}

	var tag string
	switch x := ref.(type) {
	case reference.Canonical:
		return errors.New("cannot push a digest reference")
	case reference.NamedTagged:
		tag = x.Tag()
	default:
		// We want trust signatures to always take an explicit tag,
		// otherwise it will act as an untrusted push.
		if err := jsonstream.Display(ctx, in, ioStreams.Out()); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(ioStreams.Err(), "No tag specified, skipping trust metadata push")
		return nil
	}

	if err := jsonstream.Display(ctx, in, ioStreams.Out(), jsonstream.WithAuxCallback(handleTarget)); err != nil {
		return err
	}

	if cnt > 1 {
		return errors.Errorf("internal error: only one call to handleTarget expected")
	}

	if notaryTarget == nil {
		return errors.Errorf("no targets found, provide a specific tag in order to sign it")
	}

	_, _ = fmt.Fprintln(ioStreams.Out(), "Signing and pushing trust metadata")

	repo, err := GetNotaryRepository(ioStreams.In(), ioStreams.Out(), userAgent, repoInfo, &authConfig, "push", "pull")
	if err != nil {
		return errors.Wrap(err, "error establishing connection to trust repository")
	}

	// get the latest repository metadata so we can figure out which roles to sign
	_, err = repo.ListTargets()

	switch err.(type) {
	case client.ErrRepoNotInitialized, client.ErrRepositoryNotExist:
		keys := repo.GetCryptoService().ListKeys(data.CanonicalRootRole)
		var rootKeyID string
		// always select the first root key
		if len(keys) > 0 {
			sort.Strings(keys)
			rootKeyID = keys[0]
		} else {
			rootPublicKey, err := repo.GetCryptoService().Create(data.CanonicalRootRole, "", data.ECDSAKey)
			if err != nil {
				return err
			}
			rootKeyID = rootPublicKey.ID()
		}

		// Initialize the notary repository with a remotely managed snapshot key
		if err := repo.Initialize([]string{rootKeyID}, data.CanonicalSnapshotRole); err != nil {
			return NotaryError(repoInfo.Name.Name(), err)
		}
		_, _ = fmt.Fprintf(ioStreams.Out(), "Finished initializing %q\n", repoInfo.Name.Name())
		err = repo.AddTarget(notaryTarget, data.CanonicalTargetsRole)
	case nil:
		// already initialized and we have successfully downloaded the latest metadata
		err = AddToAllSignableRoles(repo, notaryTarget)
	default:
		return NotaryError(repoInfo.Name.Name(), err)
	}

	if err == nil {
		err = repo.Publish()
	}

	if err != nil {
		err = errors.Wrapf(err, "failed to sign %s:%s", repoInfo.Name.Name(), tag)
		return NotaryError(repoInfo.Name.Name(), err)
	}

	_, _ = fmt.Fprintf(ioStreams.Out(), "Successfully signed %s:%s\n", repoInfo.Name.Name(), tag)
	return nil
}
