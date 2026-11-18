package secretsource

import (
	"context"
	"slices"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/secretsource/children/setupplan"
	"getarcane.dev/types/v3"
)

// ComposeServices lists the services of compose content and the keys each
// already gets.
func ComposeServices(content string) ([]types.SecretSourceComposeService, error) {
	services, err := setupplan.ComposeServices(content)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}
	result := make([]types.SecretSourceComposeService, 0, len(services))
	for _, service := range services {
		result = append(result, types.SecretSourceComposeService{
			Name:      service.Name,
			Available: service.Available,
			Editable:  service.Editable,
			Reason:    service.Reason,
		})
	}
	return result, nil
}

// AddComposeRefs adds environment references for secrets to compose content
// and, for EnvKeys, to .env content. It only transforms the content; callers
// save it.
func AddComposeRefs(req types.SecretSourceComposeRefsRequest) (types.SecretSourceComposeRefsResult, error) {
	updated, added, skipped, err := setupplan.InjectEnvironmentRefs(req.Compose, req.Assignments)
	if err != nil {
		return types.SecretSourceComposeRefsResult{}, common.Classify(common.ErrValidation, err)
	}
	result := types.SecretSourceComposeRefsResult{Compose: updated, Added: added, Skipped: make([]types.SecretSourceComposeSkippedRef, 0, len(skipped)), EnvAdded: []string{}}
	if len(req.EnvKeys) > 0 {
		env, envAdded, envSkipped := setupplan.InjectEnvFileRefs(req.Env, req.EnvKeys)
		result.Env, result.EnvAdded = env, envAdded
		skipped = append(skipped, envSkipped...)
	}
	for _, ref := range skipped {
		result.Skipped = append(result.Skipped, types.SecretSourceComposeSkippedRef{Service: ref.Service, Key: ref.Key, Reason: ref.Reason})
	}
	return result, nil
}

// TargetKeys fetches a target and returns only its variable names, so a
// project can be prepared before it is bound.
func (s *SecretSourceService) TargetKeys(ctx context.Context, sourceID string, target types.SecretSourceBindingTarget) (types.SecretSourceTargetKeys, error) {
	source, err := s.loadSourceInternal(ctx, sourceID)
	if err != nil {
		return types.SecretSourceTargetKeys{}, err
	}
	normalized, err := normalizeTargetInternal(source.Provider, target)
	if err != nil {
		return types.SecretSourceTargetKeys{}, err
	}
	provider, err := s.providerForSourceInternal(source)
	if err != nil {
		return types.SecretSourceTargetKeys{}, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	values, skipped, err := provider.Fetch(fetchCtx, normalized)
	if err != nil {
		return types.SecretSourceTargetKeys{}, common.Classify(common.ErrUnavailable, err)
	}

	result := types.SecretSourceTargetKeys{Keys: []string{}, Skipped: slices.Clone(skipped)}
	for key := range values {
		if envKeyPattern.MatchString(key) {
			result.Keys = append(result.Keys, key)
		} else {
			result.Skipped = append(result.Skipped, key)
		}
	}
	if result.Skipped == nil {
		result.Skipped = []string{}
	}
	slices.Sort(result.Keys)
	slices.Sort(result.Skipped)
	return result, nil
}
