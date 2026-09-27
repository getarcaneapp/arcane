// Package imageref contains shared image-reference parsing operations.
package imageref

import (
	"cmp"
	"fmt"
	"strings"

	ref "github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"
)

const (
	// UpdateCheckLabel opts a container or service out of update checks and
	// notifications. Automatic installation is governed separately by the
	// updater label and the UI exclusion list.
	UpdateCheckLabel = "com.getarcaneapp.arcane.update-check"

	// LocalBuildRegistry is Arcane's reserved registry host for locally built image tags.
	LocalBuildRegistry = "arcane.local"
)

// IsUpdateCheckDisabled reports whether labels opt the resource out of update
// monitoring. Checks stay enabled unless the value parses as false.
func IsUpdateCheckDisabled(labels map[string]string) bool {
	value, ok := labels[UpdateCheckLabel]
	if !ok {
		// Other spellings are accepted; among conflicting ones the smallest key
		// wins so the answer does not depend on map iteration order.
		var matched string
		for key, candidate := range labels {
			if strings.EqualFold(strings.TrimSpace(key), UpdateCheckLabel) && (!ok || key < matched) {
				matched, value, ok = key, candidate, true
			}
		}
	}
	if !ok {
		return false
	}
	enabled, parsed := kit.ParseBool(value)
	return parsed && !enabled
}

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
	tag = cmp.Or(tag, "latest")

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

// UpdatePolicyKey identifies the configured reference, selection policy, and
// monitoring eligibility a stored check result belongs to. Automatic
// installation eligibility is deliberately not part of the key.
func UpdatePolicyKey(imageRef string, labels map[string]string) string {
	policy := updater.DefaultLabelPolicy().TagPolicy(labels)
	if resolved, err := tagpolicy.Resolve(imageRef, policy); err == nil {
		policy = resolved
	}
	return fmt.Sprintf("%q:%q:%q:%q:%t", refs.NormalizeImageUpdateRef(imageRef), policy.Strategy, policy.Constraint, policy.TagPattern, IsUpdateCheckDisabled(labels))
}
