// Package imageref contains shared image-reference parsing operations.
package imageref

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	ref "github.com/distribution/reference"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/registryauth"
)

const (
	// UpdateCheckLabel opts a container or service out of update checks and
	// notifications. Automatic installation is governed separately by the
	// updater label and the UI exclusion list.
	UpdateCheckLabel = "com.getarcaneapp.arcane.update-check"

	// UpdateDelayLabel overrides the environment's auto-update delay, in hours,
	// for a container or service. A value of 0 applies updates immediately.
	UpdateDelayLabel = "com.getarcaneapp.arcane.updater.delay"

	// MaxUpdateDelayHours caps the auto-update delay at one year, keeping it
	// within time.Duration.
	MaxUpdateDelayHours = 8760

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

// UpdateDelay returns the auto-update delay override in hours, capped at
// MaxUpdateDelayHours. ok is false when the label is absent or is not a
// non-negative integer.
func UpdateDelay(labels map[string]string) (hours int, ok bool) {
	hours, err := strconv.Atoi(strings.TrimSpace(labels[UpdateDelayLabel]))
	if err != nil || hours < 0 {
		return 0, false
	}
	return min(hours, MaxUpdateDelayHours), true
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
