package systembackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	systemVolumeBackupConfigKey = "systemVolumeBackupConfig"
	systemVolumeBackupJobPrefix = "volumes:"
	defaultSystemVolumeSchedule = "0 0 2 * * *"
	legacySystemVolumePolicyID  = "legacy"
)

func defaultSystemVolumeBackupPolicyInternal() backuptypes.SystemVolumeBackupPolicy {
	return backuptypes.SystemVolumeBackupPolicy{
		Schedule: defaultSystemVolumeSchedule, RetentionCount: 7, LocalEnabled: true,
		SelectionMode: backuptypes.SystemVolumeSelectionAll, VolumeNames: []string{}, IgnoreAnonymous: true,
	}
}

func (s *SystemBackupService) loadSystemVolumeBackupPoliciesInternal() (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	collection := &backuptypes.SystemVolumeBackupPolicyCollection{Policies: []backuptypes.SystemVolumeBackupPolicy{}}
	if s.settingsService == nil {
		return collection, nil
	}
	raw := strings.TrimSpace(s.settingsService.GetSettingsConfig().SystemVolumeBackupConfig.Value)
	if raw == "" {
		return collection, nil
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &shape); err != nil {
		return nil, fmt.Errorf("decode system-managed volume backup policies: %w", err)
	}
	if _, isCollection := shape["policies"]; isCollection {
		if err := json.Unmarshal([]byte(raw), collection); err != nil {
			return nil, fmt.Errorf("decode system-managed volume backup policies: %w", err)
		}
		if collection.Policies == nil {
			collection.Policies = []backuptypes.SystemVolumeBackupPolicy{}
		}
	} else {
		legacy := defaultSystemVolumeBackupPolicyInternal()
		if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
			return nil, fmt.Errorf("decode legacy system-managed volume backup policy: %w", err)
		}
		legacy.ID = legacySystemVolumePolicyID
		collection.Policies = append(collection.Policies, legacy)
	}
	for i := range collection.Policies {
		collection.Policies[i].VolumeNames = normalizeVolumeNamesInternal(collection.Policies[i].VolumeNames)
	}
	return collection, nil
}

func (s *SystemBackupService) systemVolumeBackupPolicyInternal(policyID string) (*backuptypes.SystemVolumeBackupPolicy, error) {
	collection, err := s.loadSystemVolumeBackupPoliciesInternal()
	if err != nil {
		return nil, err
	}
	for i := range collection.Policies {
		if collection.Policies[i].ID == policyID {
			policy := collection.Policies[i]
			return &policy, nil
		}
	}
	return nil, nil
}

// GetSystemVolumeBackupConfig returns every saved centralized volume backup policy.
func (s *SystemBackupService) GetSystemVolumeBackupConfig(ctx context.Context) (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	collection, err := s.loadSystemVolumeBackupPoliciesInternal()
	if err != nil {
		return nil, err
	}
	destinations := make(map[string]string)
	if s.s3Destinations != nil {
		if available, listErr := s.s3Destinations.ListAllS3Destinations(ctx); listErr == nil {
			for _, destination := range available {
				destinations[destination.ID] = destination.Name
			}
		}
	}
	for i := range collection.Policies {
		policy := &collection.Policies[i]
		policy.S3DestinationName = destinations[policy.S3DestinationID]
		if s.db == nil {
			continue
		}
		var lastRun volume.VolumeBackup
		runQuery := s.db.WithContext(ctx)
		if policy.ID == legacySystemVolumePolicyID {
			runQuery = runQuery.Where("policy_id LIKE ? AND policy_id NOT LIKE ?", backuptypes.SystemVolumePolicyPrefix+"%", backuptypes.SystemVolumePolicyPrefix+"%:%")
		} else {
			runQuery = runQuery.Where("policy_id LIKE ?", backuptypes.SystemVolumePolicyPrefix+policy.ID+":%")
		}
		runErr := runQuery.Order("created_at DESC").First(&lastRun).Error
		if runErr == nil {
			policy.LastRun = &backuptypes.SystemBackupRun{
				ID: lastRun.ID, Size: lastRun.Size, CreatedAt: lastRun.CreatedAt, Status: string(lastRun.Status),
				Trigger: string(lastRun.Trigger), Destination: backuptypes.SystemBackupDestination(lastRun.Destination),
				LocalSnapshotID: lastRun.LocalSnapshotID, RemoteSnapshotID: lastRun.RemoteSnapshotID,
				S3DestinationID: lastRun.S3DestinationID, S3DestinationName: destinations[lastRun.S3DestinationID],
				PolicyID: lastRun.PolicyID, Error: lastRun.Error,
			}
		} else if !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("load latest system-managed volume backup: %w", runErr)
		}
	}
	return collection, nil
}

func validateSystemVolumeSelectionInternal(mode backuptypes.SystemVolumeSelectionMode) error {
	if mode != backuptypes.SystemVolumeSelectionAll && mode != backuptypes.SystemVolumeSelectionAllowlist && mode != backuptypes.SystemVolumeSelectionBlocklist {
		return errors.New("selectionMode must be all, allowlist, or blocklist")
	}
	return nil
}

func (s *SystemBackupService) normalizeSystemVolumePolicyUpdateInternal(ctx context.Context, input backuptypes.UpdateSystemVolumeBackupPolicy) (backuptypes.SystemVolumeBackupPolicy, error) {
	if err := validateSystemVolumeSelectionInternal(input.SelectionMode); err != nil {
		return backuptypes.SystemVolumeBackupPolicy{}, err
	}
	update, err := backup.ValidatePolicyUpdate(ctx, "system-managed volume", input.UpdateBackupPolicy, func(ctx context.Context, destinationID string) error {
		if s.s3Destinations == nil {
			return errors.New("S3 backup destinations are unavailable")
		}
		if _, destinationErr := s.s3Destinations.Configuration(ctx, destinationID); destinationErr != nil {
			return errors.New("select a valid S3 destination for system-managed volume backups")
		}
		return nil
	})
	if err != nil {
		return backuptypes.SystemVolumeBackupPolicy{}, err
	}
	names := normalizeVolumeNamesInternal(input.VolumeNames)
	if input.SelectionMode == backuptypes.SystemVolumeSelectionAll {
		names = []string{}
	}
	return backuptypes.SystemVolumeBackupPolicy{
		ID: input.ID, Enabled: update.Enabled, Schedule: update.Schedule, RetentionCount: update.RetentionCount,
		StopContainers: update.StopContainers, LocalEnabled: update.LocalEnabled, S3Enabled: update.S3Enabled,
		S3DestinationID: update.S3DestinationID, SelectionMode: input.SelectionMode,
		VolumeNames: names, IgnoreAnonymous: input.IgnoreAnonymous,
	}, nil
}

// UpdateSystemVolumeBackupConfig reconciles, saves, and reschedules centralized volume policies.
func (s *SystemBackupService) UpdateSystemVolumeBackupConfig(ctx context.Context, updates []backuptypes.UpdateSystemVolumeBackupPolicy) (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	if s.settingsService == nil {
		return nil, errors.New("settings service is unavailable")
	}
	existing, err := s.loadSystemVolumeBackupPoliciesInternal()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]backuptypes.SystemVolumeBackupPolicy, len(existing.Policies))
	for _, policy := range existing.Policies {
		byID[policy.ID] = policy
	}
	policies := make([]backuptypes.SystemVolumeBackupPolicy, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		if update.ID != "" {
			if _, ok := byID[update.ID]; !ok {
				return nil, errors.New("system-managed volume backup policy not found")
			}
			if _, duplicate := kept[update.ID]; duplicate {
				return nil, errors.New("duplicate system-managed volume backup policy")
			}
		} else {
			update.ID = uuid.NewString()
		}
		policy, normalizeErr := s.normalizeSystemVolumePolicyUpdateInternal(ctx, update)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		kept[policy.ID] = struct{}{}
		policies = append(policies, policy)
	}
	encoded, err := json.Marshal(backuptypes.SystemVolumeBackupPolicyCollection{Policies: policies})
	if err != nil {
		return nil, fmt.Errorf("encode system-managed volume backup policies: %w", err)
	}
	if err := s.settingsService.UpdateSetting(ctx, systemVolumeBackupConfigKey, string(encoded)); err != nil {
		return nil, fmt.Errorf("save system-managed volume backup policies: %w", err)
	}
	for _, policy := range existing.Policies {
		if _, ok := kept[policy.ID]; !ok {
			s.jobs.Unregister(ctx, systemVolumeBackupJobPrefix+policy.ID)
		}
	}
	for i := range policies {
		s.rescheduleSystemVolumeBackupInternal(ctx, &policies[i])
	}
	return s.GetSystemVolumeBackupConfig(ctx)
}

func normalizeVolumeNamesInternal(names []string) []string {
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			unique[name] = struct{}{}
		}
	}
	result := slices.Collect(maps.Keys(unique))
	slices.Sort(result)
	return result
}

// ListSystemVolumeBackupOptions returns live choices plus configured names that are currently unavailable.
func (s *SystemBackupService) ListSystemVolumeBackupOptions(ctx context.Context) ([]backuptypes.SystemVolumeBackupOption, error) {
	if s.volumeService == nil {
		return nil, errors.New("volume service is unavailable")
	}
	options, err := s.volumeService.ListBackupVolumeOptions(ctx)
	if err != nil {
		return nil, err
	}
	collection, err := s.loadSystemVolumeBackupPoliciesInternal()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		known[option.Name] = struct{}{}
	}
	for _, policy := range collection.Policies {
		for _, name := range policy.VolumeNames {
			if _, ok := known[name]; !ok {
				options = append(options, backuptypes.SystemVolumeBackupOption{Name: name})
				known[name] = struct{}{}
			}
		}
	}
	slices.SortFunc(options, func(a, b backuptypes.SystemVolumeBackupOption) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return options, nil
}

func selectSystemVolumeBackupCandidatesInternal(policy backuptypes.SystemVolumeBackupPolicy, options []backuptypes.SystemVolumeBackupOption) []backuptypes.SystemVolumeBackupOption {
	configured := make(map[string]struct{}, len(policy.VolumeNames))
	for _, name := range policy.VolumeNames {
		configured[name] = struct{}{}
	}
	result := make([]backuptypes.SystemVolumeBackupOption, 0, len(options))
	for _, option := range options {
		if !option.Available {
			continue
		}
		_, selected := configured[option.Name]
		matches := policy.SelectionMode == backuptypes.SystemVolumeSelectionAll ||
			(policy.SelectionMode == backuptypes.SystemVolumeSelectionAllowlist && selected) ||
			(policy.SelectionMode == backuptypes.SystemVolumeSelectionBlocklist && !selected)
		if matches && (!policy.IgnoreAnonymous || !option.Anonymous) {
			result = append(result, option)
		}
	}
	return result
}

func systemVolumePolicyIDInternal(policyID, volumeName string) string {
	sum := sha256.Sum256([]byte(volumeName))
	seriesID := hex.EncodeToString(sum[:8])
	if policyID == legacySystemVolumePolicyID {
		return backuptypes.SystemVolumePolicyPrefix + seriesID
	}
	return backuptypes.SystemVolumePolicyPrefix + policyID + ":" + seriesID
}

func systemVolumeManualPolicyIDInternal(volumeName string) string {
	sum := sha256.Sum256([]byte(volumeName))
	return backuptypes.SystemVolumePolicyPrefix + "manual:" + hex.EncodeToString(sum[:8])
}

func customSystemVolumePolicyInternal(custom *backuptypes.SystemVolumeBackupCustomRun) backuptypes.UpdateSystemVolumeBackupPolicy {
	if custom == nil {
		return backuptypes.UpdateSystemVolumeBackupPolicy{
			Enabled: true, Schedule: defaultSystemVolumeSchedule, LocalEnabled: true,
			SelectionMode: backuptypes.SystemVolumeSelectionAll, VolumeNames: []string{}, IgnoreAnonymous: true,
		}
	}
	localEnabled := custom.Destination == backuptypes.SystemBackupDestinationLocal || custom.Destination == backuptypes.SystemBackupDestinationLocalS3
	s3Enabled := custom.Destination == backuptypes.SystemBackupDestinationS3 || custom.Destination == backuptypes.SystemBackupDestinationLocalS3
	return backuptypes.UpdateSystemVolumeBackupPolicy{
		Enabled: true, Schedule: defaultSystemVolumeSchedule, RetentionCount: 0, StopContainers: custom.StopContainers,
		LocalEnabled: localEnabled, S3Enabled: s3Enabled, S3DestinationID: custom.S3DestinationID,
		SelectionMode: custom.SelectionMode, VolumeNames: custom.VolumeNames, IgnoreAnonymous: custom.IgnoreAnonymous,
	}
}

func (s *SystemBackupService) resolveSystemVolumeRunPolicyInternal(ctx context.Context, request backuptypes.RunSystemVolumeBackupsRequest) (backuptypes.SystemVolumeBackupPolicy, bool, error) {
	if request.PolicyID != "" {
		if request.Custom != nil {
			return backuptypes.SystemVolumeBackupPolicy{}, false, errors.New("select a saved policy or custom configuration, not both")
		}
		policy, err := s.systemVolumeBackupPolicyInternal(request.PolicyID)
		if err != nil {
			return backuptypes.SystemVolumeBackupPolicy{}, false, err
		}
		if policy == nil {
			return backuptypes.SystemVolumeBackupPolicy{}, false, errors.New("system-managed volume backup policy not found")
		}
		return *policy, false, nil
	}
	custom := request.Custom
	if custom != nil && custom.Destination != backuptypes.SystemBackupDestinationLocal && custom.Destination != backuptypes.SystemBackupDestinationS3 && custom.Destination != backuptypes.SystemBackupDestinationLocalS3 {
		return backuptypes.SystemVolumeBackupPolicy{}, false, errors.New("destination must be local, s3, or local_s3")
	}
	policy, err := s.normalizeSystemVolumePolicyUpdateInternal(ctx, customSystemVolumePolicyInternal(custom))
	return policy, true, err
}

// RunSystemVolumeBackups executes a saved policy or transient custom configuration.
func (s *SystemBackupService) RunSystemVolumeBackups(ctx context.Context, request backuptypes.RunSystemVolumeBackupsRequest) (*backuptypes.SystemVolumeBackupRunResult, error) {
	return s.runSystemVolumeBackupsInternal(ctx, request, volume.VolumeBackupTriggerManual)
}

func (s *SystemBackupService) runSystemVolumeBackupsInternal(ctx context.Context, request backuptypes.RunSystemVolumeBackupsRequest, trigger volume.VolumeBackupTrigger) (*backuptypes.SystemVolumeBackupRunResult, error) {
	if s.volumeService == nil {
		return nil, errors.New("volume service is unavailable")
	}
	s.systemVolumeRunMu.Lock()
	defer s.systemVolumeRunMu.Unlock()
	policyConfig, manualPolicy, err := s.resolveSystemVolumeRunPolicyInternal(ctx, request)
	if err != nil {
		return nil, err
	}
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	defer lease.Release()
	options, err := s.volumeService.ListBackupVolumeOptions(ctx)
	if err != nil {
		return nil, err
	}
	candidates := selectSystemVolumeBackupCandidatesInternal(policyConfig, options)
	result := &backuptypes.SystemVolumeBackupRunResult{
		Matched: len(candidates), Failures: make([]backuptypes.SystemVolumeBackupFailure, 0),
	}
	policy := backuptypes.UpdateBackupPolicy{
		Enabled: true, Schedule: policyConfig.Schedule, RetentionCount: policyConfig.RetentionCount,
		StopContainers: policyConfig.StopContainers, LocalEnabled: policyConfig.LocalEnabled, S3Enabled: policyConfig.S3Enabled,
		S3DestinationID: policyConfig.S3DestinationID,
	}
	for _, candidate := range candidates {
		overridden, policyErr := s.volumeService.HasEnabledBackupPolicy(ctx, candidate.Name)
		if policyErr != nil {
			result.Failed++
			result.Failures = append(result.Failures, backuptypes.SystemVolumeBackupFailure{VolumeName: candidate.Name, Error: policyErr.Error()})
			continue
		}
		if overridden {
			result.Skipped++
			continue
		}
		seriesID := systemVolumePolicyIDInternal(policyConfig.ID, candidate.Name)
		if manualPolicy {
			seriesID = systemVolumeManualPolicyIDInternal(candidate.Name)
		}
		_, backupErr := s.volumeService.CreateSystemManagedBackup(ctx, candidate.Name, common.SystemUser, trigger, seriesID, policy)
		if errors.Is(backupErr, volume.ErrVolumeBackupAlreadyRunning) {
			result.Skipped++
			continue
		}
		if backupErr != nil {
			result.Failed++
			result.Failures = append(result.Failures, backuptypes.SystemVolumeBackupFailure{VolumeName: candidate.Name, Error: backupErr.Error()})
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

func (s *SystemBackupService) runScheduledSystemVolumeBackupInternal(ctx context.Context, policyID string) {
	policy, err := s.systemVolumeBackupPolicyInternal(policyID)
	if err != nil || policy == nil || !policy.Enabled {
		return
	}
	result, err := s.runSystemVolumeBackupsInternal(ctx, backuptypes.RunSystemVolumeBackupsRequest{PolicyID: policyID}, volume.VolumeBackupTriggerScheduled)
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled system-managed volume backups skipped; a system backup is running", "policyId", policyID)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled system-managed volume backups failed", "policyId", policyID, "error", err)
		return
	}
	slog.InfoContext(ctx, "Scheduled system-managed volume backups completed", "policyId", policyID, "matched", result.Matched, "succeeded", result.Succeeded, "failed", result.Failed, "skipped", result.Skipped)
}

func (s *SystemBackupService) rescheduleSystemVolumeBackupInternal(ctx context.Context, policy *backuptypes.SystemVolumeBackupPolicy) {
	if policy == nil {
		return
	}
	jobID := systemVolumeBackupJobPrefix + policy.ID
	if !policy.Enabled {
		s.jobs.Unregister(ctx, jobID)
		return
	}
	policyID := policy.ID
	s.jobs.Register(ctx, jobID, func(context.Context) string {
		current, err := s.systemVolumeBackupPolicyInternal(policyID)
		if err != nil || current == nil {
			return defaultSystemVolumeSchedule
		}
		return current.Schedule
	}, func(ctx context.Context) {
		s.runScheduledSystemVolumeBackupInternal(ctx, policyID)
	})
}

const backupHistoryUnionInternal = `(SELECT id, size, created_at, status, trigger, destination, '' AS format, local_snapshot_id, remote_snapshot_id, s3_destination_id, policy_id, error, 'system' AS type, 'system' AS resource_type, 'Arcane' AS resource_name FROM system_backup_runs UNION ALL SELECT id, size, created_at, status, trigger, destination, format, local_snapshot_id, remote_snapshot_id, s3_destination_id, policy_id, error, CASE WHEN policy_id LIKE 'system-volume:%' THEN 'system' ELSE 'volume' END AS type, 'volume' AS resource_type, volume_name AS resource_name FROM volume_backups) AS backup_history`

// ListBackupHistory returns a unified, server-paginated view of local backup records.
func (s *SystemBackupService) ListBackupHistory(ctx context.Context, params pagination.QueryParams, typeFilter string) ([]backuptypes.HistoryEntry, pagination.Response, error) {
	query := s.db.WithContext(ctx).Table(backupHistoryUnionInternal)
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("id LIKE ? OR status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR COALESCE(error, '') LIKE ? OR resource_name LIKE ? OR resource_type LIKE ? OR type LIKE ?", pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	query = applyHistoryTypeFilterInternal(query, typeFilter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("count backup history: %w", err)
	}
	sortColumn := "created_at"
	switch params.Sort {
	case "id":
		sortColumn = "id"
	case "size":
		sortColumn = "size"
	case "status":
		sortColumn = "status"
	case "trigger":
		sortColumn = "trigger"
	case "destination":
		sortColumn = "destination"
	case "type":
		sortColumn = "type"
	case "resourceName", "resource_name":
		sortColumn = "resource_name"
	}
	order := "ASC"
	if params.Order == pagination.SortDesc {
		order = "DESC"
	}
	query = query.Order(sortColumn + " " + order)
	if params.Limit > 0 {
		query = query.Offset(params.Start).Limit(params.Limit)
	}
	var history []backuptypes.HistoryEntry
	if err := query.Scan(&history).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("list backup history: %w", err)
	}
	decorateHistoryDestinationsInternal(ctx, s.s3Destinations, history)
	return history, pagination.BuildResponse(total, total, params), nil
}

func applyHistoryTypeFilterInternal(query *gorm.DB, typeFilter string) *gorm.DB {
	selected := make(map[string]struct{}, 2)
	for value := range strings.SplitSeq(typeFilter, ",") {
		value = strings.TrimSpace(value)
		if value == string(backuptypes.ManagementTypeSystem) || value == string(backuptypes.ManagementTypeVolume) {
			selected[value] = struct{}{}
		}
	}
	if len(selected) != 1 {
		return query
	}
	for value := range selected {
		return query.Where("type = ?", value)
	}
	return query
}

func decorateHistoryDestinationsInternal(ctx context.Context, service *s3domain.S3DestinationService, history []backuptypes.HistoryEntry) {
	if service == nil {
		return
	}
	destinations, err := service.ListAllS3Destinations(ctx)
	if err != nil {
		return
	}
	names := make(map[string]string, len(destinations))
	for _, destination := range destinations {
		names[destination.ID] = destination.Name
	}
	for i := range history {
		history[i].S3DestinationName = names[history[i].S3DestinationID]
	}
}
