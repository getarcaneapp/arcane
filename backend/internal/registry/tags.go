package registry

import (
	"context"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"go.getarcane.app/updater/refs"
	updaterregistry "go.getarcane.app/updater/registry"
)

// ListImageTags discovers repository tags with the same credential precedence as
// digest checks. External credentials replace local credentials when supplied.
func (s *ContainerRegistryService) ListImageTags(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) ([]string, error) {
	if refs.IsDigestPinnedReference(imageRef) || refs.IsImageIDLikeReference(imageRef) {
		return nil, errors.New("cannot discover tags for an immutable image reference")
	}
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, err
	}
	timeoutSeconds := 0
	if s.settingsService != nil {
		timeoutSeconds = s.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultRegistryTags))
	defer cancel()

	tags, _, err := registryOperationWithCredentialsInternal(lookupCtx, s, parts.RegistryHost, "registry tag listing of "+parts.NormalizedRef, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) ([]string, error) {
			var auth *authn.AuthConfig
			if credential != nil {
				auth = &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
			}
			return updaterregistry.FetchTags(ctx, parts.RegistryHost, parts.Repository, auth, s.distributionHTTPClient)
		})
	return tags, err
}
