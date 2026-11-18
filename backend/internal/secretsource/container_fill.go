package secretsource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

// maxContainerFillTargets bounds how many targets one container create can
// read.
const maxContainerFillTargets = 10

// ResolveTargets fetches targets once, for a container being created, and
// merges their values; on a key that several deliver, the earliest wins.
// Names that are not valid environment variable names are dropped. Nothing
// is stored: the values end up only in the new container's environment.
func (s *SecretSourceService) ResolveTargets(ctx context.Context, refs []types.SecretSourceTargetRef, actor types.Actor) (map[string]string, error) {
	if len(refs) > maxContainerFillTargets {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("a container can be filled from at most %d targets", maxContainerFillTargets))
	}
	values := make(map[string]string)
	for i, ref := range refs {
		source, err := s.loadSourceInternal(ctx, ref.SourceID)
		if err != nil {
			return nil, err
		}
		target, err := normalizeTargetInternal(source.Provider, ref.Target)
		if err != nil {
			return nil, err
		}
		provider, err := s.providerForSourceInternal(source)
		if err != nil {
			return nil, err
		}
		fetchCtx, cancel := context.WithTimeout(ctx, providerTimeout)
		fetched, _, fetchErr := provider.Fetch(fetchCtx, target)
		cancel()
		if fetchErr != nil {
			return nil, common.Classify(common.ErrSecretFetchFailed, fmt.Errorf("source %q: %w", source.Name, fetchErr))
		}
		delivered := 0
		for key, value := range fetched {
			if !envKeyPattern.MatchString(key) {
				continue
			}
			if _, taken := values[key]; !taken {
				values[key] = value
				delivered++
			}
		}
		slog.InfoContext(ctx, "filled container environment from a secret source",
			"sourceId", source.ID, "target", describeTargetInternal(&ProjectSecretBinding{Target: target}),
			"position", i, "keyCount", delivered, "user", actor.Username)
	}
	if len(values) == 0 && len(refs) > 0 {
		return nil, common.Classify(common.ErrSecretFetchFailed, errors.New("the selected secret sources returned no usable variables"))
	}
	return values, nil
}
