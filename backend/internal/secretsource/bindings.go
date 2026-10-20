package secretsource

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/types/v3"
)

type (
	// DeployEnv is the secret set resolved for one deploy: the merged values of
	// every enabled binding, with earlier bindings winning on a shared key. Hash
	// identifies the set without revealing it; it is empty when no binding
	// delivered anything. Pass the DeployEnv to RecordDeployed once the deploy
	// succeeds.
	DeployEnv struct {
		Values   map[string]string
		bindings map[string]deployedBindingInternal
		Hash     string
	}

	// deployedBindingInternal is what one binding contributed to a deploy.
	deployedBindingInternal struct {
		hash string
		keys []string
	}

	// DriftChange reports a project whose secrets changed since its last deploy,
	// the first time a background check sees that change.
	DriftChange struct {
		ProjectID    string
		ProjectName  string
		AutoRedeploy bool
	}
)

// ---- Bindings ----

// ListBindings returns the project's bindings in the order they apply.
func (s *SecretSourceService) ListBindings(ctx context.Context, projectID string) ([]types.SecretSourceBinding, error) {
	bindings, err := s.loadBindingsInternal(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]types.SecretSourceBinding, 0, len(bindings))
	for i := range bindings {
		result = append(result, bindingToDTOInternal(&bindings[i]))
	}
	return result, nil
}

// CreateBinding binds the project to one more target. It goes last unless a
// position is given.
func (s *SecretSourceService) CreateBinding(ctx context.Context, projectID string, req types.SecretSourceBindingUpsertRequest) (*types.SecretSourceBinding, error) {
	source, target, err := s.validateBindingRequestInternal(ctx, req)
	if err != nil {
		return nil, err
	}
	salt, err := newHashSaltInternal()
	if err != nil {
		return nil, err
	}
	binding := &ProjectSecretBinding{
		OwnerKind:    types.SecretSourceBindingOwnerProject,
		ProjectID:    projectID,
		SourceID:     source.ID,
		Target:       target,
		Required:     req.Required,
		Enabled:      req.Enabled,
		AutoRedeploy: req.AutoRedeploy,
		HashSalt:     salt,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if dupErr := ensureTargetUnboundInternal(tx, projectID, "", source.ID, target); dupErr != nil {
			return dupErr
		}
		var count int64
		if countErr := tx.Model(&ProjectSecretBinding{}).Where("project_id = ?", projectID).Count(&count).Error; countErr != nil {
			return fmt.Errorf("failed to count project secret bindings: %w", countErr)
		}
		binding.Position = int(count)
		if createErr := tx.Create(binding).Error; createErr != nil {
			return fmt.Errorf("failed to save project secret binding: %w", createErr)
		}
		if req.Position != nil {
			return placeBindingInternal(tx, projectID, binding.ID, *req.Position)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getBindingDTOInternal(ctx, projectID, binding.ID)
}

// UpdateBinding changes one binding. It keeps its place unless a position is
// given.
func (s *SecretSourceService) UpdateBinding(ctx context.Context, projectID, bindingID string, req types.SecretSourceBindingUpsertRequest) (*types.SecretSourceBinding, error) {
	source, target, err := s.validateBindingRequestInternal(ctx, req)
	if err != nil {
		return nil, err
	}
	binding, err := s.loadBindingInternal(ctx, projectID, bindingID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return applyBindingUpdateInternal(tx, binding, source, target, req)
	})
	if err != nil {
		return nil, err
	}
	return s.getBindingDTOInternal(ctx, projectID, binding.ID)
}

func applyBindingUpdateInternal(
	tx *gorm.DB,
	binding *ProjectSecretBinding,
	source *SecretSource,
	target types.SecretSourceBindingTarget,
	req types.SecretSourceBindingUpsertRequest,
) error {
	if err := ensureTargetUnboundInternal(tx, binding.ProjectID, binding.ID, source.ID, target); err != nil {
		return err
	}
	// Only the columns this request owns are written: a deploy or drift
	// check that runs meanwhile keeps its deployed and seen hashes.
	columns := []string{"source_id", "target", "required", "enabled", "auto_redeploy", "last_fetch_error", "updated_at"}
	// A different source or target makes the last check meaningless; the
	// deployed hash stays, because the running containers still use it.
	if binding.SourceID != source.ID || !sameTargetInternal(binding.Target, target) {
		binding.LastSeenHash = nil
		binding.LastNotifiedHash = nil
		binding.LastCheckedAt = nil
		columns = append(columns, "last_seen_hash", "last_notified_hash", "last_checked_at")
	}
	binding.SourceID = source.ID
	binding.Source = nil
	binding.Target = target
	binding.Required = req.Required
	binding.Enabled = req.Enabled
	binding.AutoRedeploy = req.AutoRedeploy
	binding.LastFetchError = nil
	now := time.Now()
	binding.UpdatedAt = &now
	err := tx.Model(binding).Select(columns).Updates(binding).Error
	if err != nil {
		return fmt.Errorf("failed to save project secret binding: %w", err)
	}
	if req.Position != nil {
		return placeBindingInternal(tx, binding.ProjectID, binding.ID, *req.Position)
	}
	return nil
}

// DeleteBinding removes one binding; the others close the gap.
func (s *SecretSourceService) DeleteBinding(ctx context.Context, projectID, bindingID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("project_id = ? AND id = ?", projectID, bindingID).Delete(&ProjectSecretBinding{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete project secret binding: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return common.ErrSecretBindingNotFound
		}
		return renumberBindingsInternal(tx, projectID, "", 0)
	})
}

func (s *SecretSourceService) validateBindingRequestInternal(
	ctx context.Context,
	req types.SecretSourceBindingUpsertRequest,
) (*SecretSource, types.SecretSourceBindingTarget, error) {
	source, err := s.loadSourceInternal(ctx, req.SourceID)
	if err != nil {
		return nil, types.SecretSourceBindingTarget{}, err
	}
	target, err := normalizeTargetInternal(source.Provider, req.Target)
	if err != nil {
		return nil, types.SecretSourceBindingTarget{}, err
	}
	if req.Position != nil && *req.Position < 0 {
		return nil, types.SecretSourceBindingTarget{}, common.Classify(common.ErrSecretBindingInvalid, errors.New("position cannot be negative"))
	}
	return source, target, nil
}

// ensureTargetUnboundInternal refuses a second binding of one project to the
// same source and target, which would only shadow itself.
func ensureTargetUnboundInternal(tx *gorm.DB, projectID, exceptID, sourceID string, target types.SecretSourceBindingTarget) error {
	var existing []ProjectSecretBinding
	query := tx.Where("project_id = ? AND source_id = ?", projectID, sourceID)
	if exceptID != "" {
		query = query.Where("id <> ?", exceptID)
	}
	if err := query.Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to load project secret bindings: %w", err)
	}
	for i := range existing {
		if sameTargetInternal(existing[i].Target, target) {
			return common.ErrSecretBindingConflict
		}
	}
	return nil
}

// placeBindingInternal moves a binding to position (clamped to the end) and
// renumbers the project's bindings from 0.
func placeBindingInternal(tx *gorm.DB, projectID, bindingID string, position int) error {
	return renumberBindingsInternal(tx, projectID, bindingID, position)
}

// renumberBindingsInternal stores positions 0..n-1 in the current order, with
// movedID (when set) placed at position.
func renumberBindingsInternal(tx *gorm.DB, projectID, movedID string, position int) error {
	var bindings []ProjectSecretBinding
	if err := tx.Where("project_id = ?", projectID).Order("position ASC, created_at ASC, id ASC").Find(&bindings).Error; err != nil {
		return fmt.Errorf("failed to load project secret bindings: %w", err)
	}
	ids := make([]string, 0, len(bindings))
	for i := range bindings {
		if bindings[i].ID != movedID {
			ids = append(ids, bindings[i].ID)
		}
	}
	if movedID != "" {
		position = min(max(position, 0), len(ids))
		ids = slices.Insert(ids, position, movedID)
	}
	for i, id := range ids {
		if err := tx.Model(&ProjectSecretBinding{}).Where("id = ?", id).UpdateColumn("position", i).Error; err != nil {
			return fmt.Errorf("failed to order project secret bindings: %w", err)
		}
	}
	return nil
}

// ---- Checks and deploys ----

// CheckBindings fetches every binding now and reports, per binding, the key
// names it delivers, keys an earlier binding already delivers, keys no
// service uses, .env keys it overrides, and whether a redeploy is needed. A
// binding that fails reports its error without failing the others. No
// values are returned.
func (s *SecretSourceService) CheckBindings(ctx context.Context, project ProjectRef, actor types.Actor) (types.SecretSourceProjectCheckResult, error) {
	bindings, err := s.loadBindingsInternal(ctx, project.ID)
	if err != nil {
		return types.SecretSourceProjectCheckResult{}, err
	}
	if len(bindings) == 0 {
		return types.SecretSourceProjectCheckResult{}, common.ErrSecretBindingNotFound
	}

	result := types.SecretSourceProjectCheckResult{Bindings: make([]types.SecretSourceCheckResult, 0, len(bindings))}
	delivered := make(map[string]struct{})
	projectDeployed := slices.ContainsFunc(bindings, func(b ProjectSecretBinding) bool { return b.DeployedHash != nil })
	for i := range bindings {
		binding := &bindings[i]
		check := types.SecretSourceCheckResult{
			BindingID:      binding.ID,
			Keys:           []string{},
			ShadowedKeys:   []string{},
			OverriddenKeys: []string{},
			InvalidKeys:    []string{},
			FetchedAt:      time.Now(),
			DeployedAt:     binding.DeployedAt,
			NeverDeployed:  binding.DeployedHash == nil,
		}
		values, skipped, fetchErr := s.fetchInternal(ctx, binding, project, actor, fetchOptions{reason: "check"})
		if fetchErr != nil {
			check.Error = fetchErr.Error()
			result.Bindings = append(result.Bindings, check)
			continue
		}

		hash := hashValuesInternal(binding.HashSalt, values)
		// A disabled binding is not deployed, so what it holds is not drift.
		if binding.Enabled {
			s.recordSeenInternal(ctx, binding.ID, hash)
		}
		keys := sortedKeysInternal(values)
		check.Keys = keys
		check.InvalidKeys = append(check.InvalidKeys, skipped...)
		check.OverriddenKeys = overriddenKeysInternal(ctx, project, keys)
		for _, key := range keys {
			if _, shadowed := delivered[key]; shadowed {
				check.ShadowedKeys = append(check.ShadowedKeys, key)
			}
		}
		// Disabled bindings deliver nothing, so they shadow nothing.
		if binding.Enabled {
			for _, key := range keys {
				delivered[key] = struct{}{}
			}
		}
		// An empty set is reported on its own; calling it drift adds nothing. A
		// binding added after the project's last deploy needs one too.
		if binding.Enabled && len(keys) > 0 {
			if binding.DeployedHash != nil {
				check.RedeployNeeded = hash != *binding.DeployedHash
			} else {
				check.RedeployNeeded = projectDeployed
			}
		}
		result.RedeployNeeded = result.RedeployNeeded || check.RedeployNeeded
		result.Bindings = append(result.Bindings, check)
	}
	return result, nil
}

// ResolveDeployEnv fetches the secrets of every enabled binding for a deploy
// and merges them; on a key that several bindings deliver, the earliest
// binding wins. Values is nil when no binding was read. A binding that was
// read but holds nothing still counts, so values added later are reported as
// drift. When a fetch fails or returns nothing usable, a required binding
// returns an error and blocks the deploy; an optional one logs and is left
// out.
func (s *SecretSourceService) ResolveDeployEnv(ctx context.Context, project ProjectRef, actor types.Actor) (DeployEnv, error) {
	bindings, err := s.loadBindingsInternal(ctx, project.ID)
	if err != nil {
		return DeployEnv{}, err
	}

	var env DeployEnv
	var hashes []string
	for i := range bindings {
		binding := &bindings[i]
		if !binding.Enabled {
			continue
		}
		values, _, fetchErr := s.fetchInternal(ctx, binding, project, actor, fetchOptions{reason: "deploy", requireKeys: binding.Required})
		if fetchErr != nil {
			if binding.Required {
				return DeployEnv{}, fetchErr
			}
			slog.WarnContext(ctx, "optional secret binding failed; deploying without its secrets",
				"projectId", project.ID, "bindingId", binding.ID, "error", fetchErr)
			continue
		}
		if env.Values == nil {
			env.Values = make(map[string]string, len(values))
			env.bindings = make(map[string]deployedBindingInternal, len(bindings))
		}
		for key, value := range values {
			if _, taken := env.Values[key]; !taken {
				env.Values[key] = value
			}
		}
		hash := hashValuesInternal(binding.HashSalt, values)
		env.bindings[binding.ID] = deployedBindingInternal{hash: hash, keys: sortedKeysInternal(values)}
		hashes = append(hashes, binding.ID+":"+hash)
	}
	if len(env.bindings) > 0 {
		env.Hash = hashValuesInternal("deploy", map[string]string{"bindings": strings.Join(hashes, ",")})
	}
	return env, nil
}

// RecordDeployed stores, for each binding the deploy used, the hash and key
// names of its secret set. The deploy resolves any pending drift, so the seen
// and notified hashes move with it.
func (s *SecretSourceService) RecordDeployed(ctx context.Context, projectID string, env DeployEnv) {
	now := time.Now()
	for bindingID, deployed := range env.bindings {
		keys, marshalErr := json.Marshal(deployed.keys)
		if marshalErr != nil {
			slog.WarnContext(ctx, "failed to encode deployed secret keys", "bindingId", bindingID, "error", marshalErr)
			continue
		}
		err := s.db.WithContext(ctx).Model(&ProjectSecretBinding{}).
			Where("project_id = ? AND id = ?", projectID, bindingID).
			UpdateColumns(map[string]any{
				"deployed_hash":      deployed.hash,
				"deployed_keys":      string(keys),
				"deployed_at":        now,
				"last_seen_hash":     deployed.hash,
				"last_notified_hash": deployed.hash,
			}).Error
		if err != nil {
			slog.WarnContext(ctx, "failed to record deployed secret hash", "projectId", projectID, "bindingId", bindingID, "error", err)
		}
	}
}

// DeployedKeys returns the names of the secrets the project's last deploy
// delivered, across its bindings, so views of the running containers can
// mask their values.
func (s *SecretSourceService) DeployedKeys(ctx context.Context, projectID string) ([]string, error) {
	var bindings []ProjectSecretBinding
	err := s.db.WithContext(ctx).Select("id", "deployed_keys").
		Where("project_id = ? AND deployed_hash IS NOT NULL", projectID).Find(&bindings).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load deployed secret keys: %w", err)
	}
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	for i := range bindings {
		for _, key := range bindings[i].DeployedKeys {
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	return keys, nil
}

// CheckDrift fetches every enabled, deployed binding and returns the projects
// whose secrets changed since their last deploy, once per project. Each change
// is returned, and logged as an event, only once; failures are logged when
// they first appear.
func (s *SecretSourceService) CheckDrift(ctx context.Context, resolve ProjectResolver) []DriftChange {
	var bindings []ProjectSecretBinding
	err := s.db.WithContext(ctx).Preload("Source").
		Where("owner_kind = ? AND enabled = ? AND deployed_hash IS NOT NULL", types.SecretSourceBindingOwnerProject, true).
		Order("project_id ASC, position ASC").
		Find(&bindings).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to load secret bindings for drift check", "error", err)
		return nil
	}

	var changes []DriftChange
	byProject := make(map[string]int)
	projects := make(map[string]ProjectRef)
	for i := range bindings {
		binding := &bindings[i]
		project, ok := projects[binding.ProjectID]
		if !ok {
			project = ProjectRef{ID: binding.ProjectID, Name: binding.ProjectID}
			if resolve != nil {
				if resolved, resolveErr := resolve(ctx, binding.ProjectID); resolveErr == nil {
					project = resolved
				}
			}
			projects[binding.ProjectID] = project
		}

		values, _, fetchErr := s.fetchInternal(ctx, binding, project, types.SystemUser, fetchOptions{reason: "drift-check", quiet: true})
		if fetchErr != nil || len(values) == 0 {
			continue
		}
		hash := hashValuesInternal(binding.HashSalt, values)
		s.recordSeenInternal(ctx, binding.ID, hash)
		if binding.DeployedHash == nil || hash == *binding.DeployedHash {
			continue
		}
		if binding.LastNotifiedHash != nil && hash == *binding.LastNotifiedHash {
			continue
		}

		s.recordNotifiedInternal(ctx, binding.ID, hash)
		metadata := database.JSON{
			"bindingId":    binding.ID,
			"sourceId":     binding.SourceID,
			"target":       describeTargetInternal(binding),
			"autoRedeploy": binding.AutoRedeploy,
		}
		if binding.Source != nil {
			metadata["sourceName"] = binding.Source.Name
			metadata["provider"] = binding.Source.Provider
		}
		s.logEventInternal(ctx, event.EventTypeProjectSecretsChanged, project, types.SystemUser, metadata)

		if index, seen := byProject[project.ID]; seen {
			changes[index].AutoRedeploy = changes[index].AutoRedeploy || binding.AutoRedeploy
			continue
		}
		byProject[project.ID] = len(changes)
		changes = append(changes, DriftChange{ProjectID: project.ID, ProjectName: project.Name, AutoRedeploy: binding.AutoRedeploy})
	}
	return changes
}

// ---- Persistence ----

// loadBindingsInternal returns the project's bindings in order, each with
// its source.
func (s *SecretSourceService) loadBindingsInternal(ctx context.Context, projectID string) ([]ProjectSecretBinding, error) {
	var bindings []ProjectSecretBinding
	err := s.db.WithContext(ctx).Preload("Source").
		Where("project_id = ?", projectID).Order("position ASC, created_at ASC, id ASC").Find(&bindings).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load project secret bindings: %w", err)
	}
	return bindings, nil
}

func (s *SecretSourceService) loadBindingInternal(ctx context.Context, projectID, bindingID string) (*ProjectSecretBinding, error) {
	var binding ProjectSecretBinding
	err := s.db.WithContext(ctx).Preload("Source").Where("project_id = ? AND id = ?", projectID, bindingID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.ErrSecretBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load project secret binding: %w", err)
	}
	return &binding, nil
}

func (s *SecretSourceService) getBindingDTOInternal(ctx context.Context, projectID, bindingID string) (*types.SecretSourceBinding, error) {
	binding, err := s.loadBindingInternal(ctx, projectID, bindingID)
	if err != nil {
		return nil, err
	}
	dto := bindingToDTOInternal(binding)
	return &dto, nil
}

func bindingToDTOInternal(binding *ProjectSecretBinding) types.SecretSourceBinding {
	dto := types.SecretSourceBinding{
		ID:             binding.ID,
		ProjectID:      binding.ProjectID,
		Position:       binding.Position,
		SourceID:       binding.SourceID,
		Target:         binding.Target,
		Required:       binding.Required,
		Enabled:        binding.Enabled,
		AutoRedeploy:   binding.AutoRedeploy,
		RedeployNeeded: binding.redeployNeededInternal(),
		DeployedAt:     binding.DeployedAt,
		LastFetchedAt:  binding.LastFetchedAt,
		LastCheckedAt:  binding.LastCheckedAt,
		LastFetchError: binding.LastFetchError,
	}
	if binding.Source != nil {
		dto.SourceName = binding.Source.Name
		dto.Provider = binding.Source.Provider
	}
	return dto
}
