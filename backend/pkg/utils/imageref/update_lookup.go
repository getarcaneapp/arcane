// Package imageref contains shared image-reference parsing operations.
package imageref

import (
	"fmt"
	"strings"

	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"

	ref "github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
)

// LocalBuildRegistry is Arcane's reserved registry host for locally built image tags.
const LocalBuildRegistry = "arcane.local"

// ParseUpdateLookup normalizes an image reference into the repository and tag
// candidates used to match persisted update records.
func ParseUpdateLookup(imageRef string) (originalRef, tag string, repositoryCandidates map[string]struct{}, ok bool) {
	trimmedRef := strings.TrimSpace(imageRef)
	if trimmedRef == "" {
		return "", "", nil, false
	}

	named, err := ref.ParseNormalizedNamed(trimmedRef)
	if err != nil {
		return "", "", nil, false
	}

	tag = "latest"
	if tagged, taggedOK := named.(ref.NamedTagged); taggedOK {
		tag = strings.TrimSpace(tagged.Tag())
	}
	if tag == "" {
		tag = "latest"
	}

	registryHost := registryauth.NormalizeRegistryForComparison(ref.Domain(named))
	repositoryPath := strings.TrimSpace(ref.Path(named))
	familiarRepository := strings.TrimSpace(ref.FamiliarName(named))

	repositoryCandidates = make(map[string]struct{})
	for _, candidate := range []string{
		repositoryPath,
		familiarRepository,
		fmt.Sprintf("%s/%s", registryHost, repositoryPath),
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			repositoryCandidates[candidate] = struct{}{}
		}
	}

	if registryHost == "docker.io" && strings.HasPrefix(repositoryPath, "library/") {
		repositoryCandidates[strings.TrimPrefix(repositoryPath, "library/")] = struct{}{}
	}

	return trimmedRef, tag, repositoryCandidates, true
}

// UpdatePolicyKey identifies the configured reference and selection policy.
func UpdatePolicyKey(imageRef string, labels map[string]string) string {
	policy := updater.DefaultLabelPolicy().TagPolicy(labels)
	if resolved, err := tagpolicy.Resolve(imageRef, policy); err == nil {
		policy = resolved
	}
	return fmt.Sprintf("%q:%q:%q:%q:%t", refs.NormalizeImageUpdateRef(imageRef), policy.Strategy, policy.Constraint, policy.TagPattern, updater.DefaultLabelPolicy().IsUpdateDisabled(labels))
}
