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
	"gorm.io/gorm"
)

const (
	systemVolumeBackupConfigKey = "systemVolumeBackupConfig"
	systemVolumeBackupJobID     = "volumes"
	defaultSystemVolumeSchedule = "0 0 2 * * *"
)

func defaultSystemVolumeBackupConfigInternal() backuptypes.SystemVolumeBackupConfig {
	return backuptypes.SystemVolumeBackupConfig{
		Schedule: defaultSystemVolumeSchedule, RetentionCount: 7, LocalEnabled: true,
		SelectionMode: backuptypes.SystemVolumeSelectionAll, VolumeNames: []string{}, IgnoreAnonymous: true,
	}
}

func (s *SystemBackupService) loadSystemVolumeBackupConfigInternal() (*backuptypes.SystemVolumeBackupConfig, error) {
	config := defaultSystemVolumeBackupConfigInternal()
	if s.settingsService == nil {
		return &config, nil
	}
	raw := strings.TrimSpace(s.settingsService.GetSettingsConfig().SystemVolumeBackupConfig.Value)
	if raw == "" {
		return &config, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil, fmt.Errorf("decode system-managed volume backup configuration: %w", err)
	}
	config.VolumeNames = normalizeVolumeNamesInternal(config.VolumeNames)
	return &config, nil
}

// GetSystemVolumeBackupConfig returns the saved centralized volume backup configuration.
func (s *SystemBackupService) GetSystemVolumeBackupConfig(ctx context.Context) (*backuptypes.SystemVolumeBackupConfig, error) {
	config, err := s.loadSystemVolumeBackupConfigInternal()
	if err != nil {
		return nil, err
	}
	if config.S3DestinationID != "" && s.s3Destinations != nil {
		if destinations, listErr := s.s3Destinations.ListAllS3Destinations(ctx); listErr == nil {
			for _, destination := range destinations {
				if destination.ID == config.S3DestinationID {
					config.S3DestinationName = destination.Name
					break
				}
			}
		}
	}
	return config, nil
}

// UpdateSystemVolumeBackupConfig validates, saves, and reschedules the centralized policy.
func (s *SystemBackupService) UpdateSystemVolumeBackupConfig(ctx context.Context, config backuptypes.SystemVolumeBackupConfig) (*backuptypes.SystemVolumeBackupConfig, error) {
	if s.settingsService == nil {
		return nil, errors.New("settings service is unavailable")
	}
	if config.SelectionMode != backuptypes.SystemVolumeSelectionAll && config.SelectionMode != backuptypes.SystemVolumeSelectionAllowlist && config.SelectionMode != backuptypes.SystemVolumeSelectionBlocklist {
		return nil, errors.New("selectionMode must be all, allowlist, or blocklist")
	}
	update, err := backup.ValidatePolicyUpdate(ctx, "system-managed volume", backuptypes.UpdateBackupPolicy{
		Enabled: config.Enabled, Schedule: config.Schedule, RetentionCount: config.RetentionCount,
		StopContainers: config.StopContainers, LocalEnabled: config.LocalEnabled, S3Enabled: config.S3Enabled,
		S3DestinationID: config.S3DestinationID,
	}, func(ctx context.Context, destinationID string) error {
		if s.s3Destinations == nil {
			return errors.New("S3 backup destinations are unavailable")
		}
		if _, destinationErr := s.s3Destinations.Configuration(ctx, destinationID); destinationErr != nil {
			return errors.New("select a valid S3 destination for system-managed volume backups")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	config.Schedule, config.RetentionCount = update.Schedule, update.RetentionCount
	config.LocalEnabled, config.S3Enabled, config.S3DestinationID = update.LocalEnabled, update.S3Enabled, update.S3DestinationID
	if config.SelectionMode == backuptypes.SystemVolumeSelectionAll {
		config.VolumeNames = []string{}
	} else {
		config.VolumeNames = normalizeVolumeNamesInternal(config.VolumeNames)
	}
	config.S3DestinationName = ""
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode system-managed volume backup configuration: %w", err)
	}
	if err := s.settingsService.UpdateSetting(ctx, systemVolumeBackupConfigKey, string(encoded)); err != nil {
		return nil, fmt.Errorf("save system-managed volume backup configuration: %w", err)
	}
	s.rescheduleSystemVolumeBackupInternal(ctx, &config)
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
	config, err := s.loadSystemVolumeBackupConfigInternal()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		known[option.Name] = struct{}{}
	}
	for _, name := range config.VolumeNames {
		if _, ok := known[name]; !ok {
			options = append(options, backuptypes.SystemVolumeBackupOption{Name: name})
		}
	}
	slices.SortFunc(options, func(a, b backuptypes.SystemVolumeBackupOption) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return options, nil
}

func selectSystemVolumeBackupCandidatesInternal(config backuptypes.SystemVolumeBackupConfig, options []backuptypes.SystemVolumeBackupOption) []backuptypes.SystemVolumeBackupOption {
	configured := make(map[string]struct{}, len(config.VolumeNames))
	for _, name := range config.VolumeNames {
		configured[name] = struct{}{}
	}
	result := make([]backuptypes.SystemVolumeBackupOption, 0, len(options))
	for _, option := range options {
		if !option.Available {
			continue
		}
		_, selected := configured[option.Name]
		matches := config.SelectionMode == backuptypes.SystemVolumeSelectionAll ||
			(config.SelectionMode == backuptypes.SystemVolumeSelectionAllowlist && selected) ||
			(config.SelectionMode == backuptypes.SystemVolumeSelectionBlocklist && !selected)
		if matches && (!config.IgnoreAnonymous || !option.Anonymous) {
			result = append(result, option)
		}
	}
	return result
}

func systemVolumePolicyIDInternal(volumeName string) string {
	sum := sha256.Sum256([]byte(volumeName))
	return backuptypes.SystemVolumePolicyPrefix + hex.EncodeToString(sum[:8])
}

// RunSystemVolumeBackups executes the saved centralized policy, even when its schedule is disabled.
func (s *SystemBackupService) RunSystemVolumeBackups(ctx context.Context) (*backuptypes.SystemVolumeBackupRunResult, error) {
	config, err := s.loadSystemVolumeBackupConfigInternal()
	if err != nil {
		return nil, err
	}
	if s.volumeService == nil {
		return nil, errors.New("volume service is unavailable")
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
	candidates := selectSystemVolumeBackupCandidatesInternal(*config, options)
	result := &backuptypes.SystemVolumeBackupRunResult{
		Matched: len(candidates), Failures: make([]backuptypes.SystemVolumeBackupFailure, 0),
	}
	policy := backuptypes.UpdateBackupPolicy{
		Enabled: true, Schedule: config.Schedule, RetentionCount: config.RetentionCount,
		StopContainers: config.StopContainers, LocalEnabled: config.LocalEnabled, S3Enabled: config.S3Enabled,
		S3DestinationID: config.S3DestinationID,
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
		_, backupErr := s.volumeService.CreateSystemManagedBackup(ctx, candidate.Name, common.SystemUser, systemVolumePolicyIDInternal(candidate.Name), policy)
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

func (s *SystemBackupService) runScheduledSystemVolumeBackupInternal(ctx context.Context) {
	config, err := s.loadSystemVolumeBackupConfigInternal()
	if err != nil || !config.Enabled {
		return
	}
	result, err := s.RunSystemVolumeBackups(ctx)
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled system-managed volume backups skipped; a system backup is running")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled system-managed volume backups failed", "error", err)
		return
	}
	slog.InfoContext(ctx, "Scheduled system-managed volume backups completed", "matched", result.Matched, "succeeded", result.Succeeded, "failed", result.Failed, "skipped", result.Skipped)
}

func (s *SystemBackupService) rescheduleSystemVolumeBackupInternal(ctx context.Context, config *backuptypes.SystemVolumeBackupConfig) {
	if config == nil || !config.Enabled {
		s.jobs.Unregister(ctx, systemVolumeBackupJobID)
		return
	}
	s.jobs.Register(ctx, systemVolumeBackupJobID, func(context.Context) string {
		current, err := s.loadSystemVolumeBackupConfigInternal()
		if err != nil {
			return defaultSystemVolumeSchedule
		}
		return current.Schedule
	}, s.runScheduledSystemVolumeBackupInternal)
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
