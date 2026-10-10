package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"

	composetemplate "github.com/compose-spec/compose-go/v2/template"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	composepkg "github.com/docker/compose/v5/pkg/compose"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
)

func PlanVolumeMigration(ctx context.Context, dockerClient *client.Client, composeProject *composetypes.Project, oldComposeName, newComposeName, toolsImage string) (volumetypes.Migration, error) {
	if dockerClient == nil {
		return nil, errors.New("docker service unavailable")
	}
	if composeProject == nil || len(composeProject.Volumes) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(oldComposeName) == "" || strings.TrimSpace(newComposeName) == "" || oldComposeName == newComposeName {
		return nil, nil
	}

	explicitVolumeNames, err := composeVolumeKeysWithExplicitNameInternal(composeProject.ComposeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose volume names: %w", err)
	}

	entries, err := collectProjectRenameVolumeEntriesInternal(ctx, dockerClient, composeProject.Volumes, explicitVolumeNames, oldComposeName, newComposeName)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	return volumes.NewMigration(dockerClient, entries, oldComposeName, newComposeName, toolsImage), nil
}

func collectProjectRenameVolumeEntriesInternal(ctx context.Context,
	dockerClient *client.Client,
	localVolumes map[string]composetypes.VolumeConfig,
	explicitVolumeNames map[string]struct{},
	oldComposeName,
	newComposeName string) ([]volumetypes.RenameEntry,
	error,
) {
	entries := make([]volumetypes.RenameEntry, 0, len(localVolumes))
	for key, volumeConfig := range localVolumes {
		entry, ok, err := inspectProjectRenameVolumeEntryInternal(ctx, dockerClient, key, volumeConfig, explicitVolumeNames, oldComposeName, newComposeName)
		if err != nil {
			return nil, err
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func inspectProjectRenameVolumeEntryInternal(ctx context.Context,
	dockerClient *client.Client,
	key string,
	volumeConfig composetypes.VolumeConfig,
	explicitVolumeNames map[string]struct{},
	oldComposeName,
	newComposeName string) (volumetypes.RenameEntry,
	bool,
	error,
) {
	if _, explicit := explicitVolumeNames[key]; explicit || bool(volumeConfig.External) {
		return volumetypes.RenameEntry{}, false, nil
	}

	oldName := oldComposeName + "_" + key
	newName := newComposeName + "_" + key
	if oldName == newName || (volumeConfig.Name != oldName && volumeConfig.Name != newName) {
		return volumetypes.RenameEntry{}, false, nil
	}

	if err := volumes.EnsureRenameTargetAbsent(ctx, dockerClient, newName); err != nil {
		return volumetypes.RenameEntry{}, false, err
	}

	oldVolume, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return volumetypes.RenameEntry{}, false, nil
		}
		return volumetypes.RenameEntry{}, false, fmt.Errorf("inspect source volume %s: %w", oldName, err)
	}
	if ensureRenameSourceDetachedErr := volumes.EnsureRenameSourceDetached(ctx, dockerClient, oldName); ensureRenameSourceDetachedErr != nil {
		return volumetypes.RenameEntry{}, false, ensureRenameSourceDetachedErr
	}

	newConfig := buildProjectRenamedVolumeConfigInternal(volumeConfig, key, newName, newComposeName)
	createOptions, err := renamedVolumeCreateOptionsInternal(newConfig, oldVolume.Volume.Labels)
	if err != nil {
		return volumetypes.RenameEntry{}, false, err
	}
	return volumetypes.RenameEntry{
		Key:           key,
		OldName:       oldName,
		NewName:       newName,
		OldVolume:     oldVolume.Volume,
		CreateOptions: createOptions,
	}, true, nil
}

func buildProjectRenamedVolumeConfigInternal(volumeConfig composetypes.VolumeConfig, key, newName, newComposeName string) composetypes.VolumeConfig {
	newConfig := volumeConfig
	newConfig.Name = newName
	newConfig.CustomLabels = composetypes.Labels{
		api.VolumeLabel:  key,
		api.ProjectLabel: newComposeName,
		api.VersionLabel: api.ComposeVersion,
	}
	return newConfig
}

func renamedVolumeCreateOptionsInternal(config composetypes.VolumeConfig, oldLabels map[string]string) (client.VolumeCreateOptions, error) {
	labels := map[string]string{}
	maps.Copy(labels, oldLabels)
	maps.Copy(labels, config.Labels)
	maps.Copy(labels, config.CustomLabels)
	hash, err := composepkg.VolumeHash(config)
	if err != nil {
		return client.VolumeCreateOptions{}, fmt.Errorf("hash target volume %s: %w", config.Name, err)
	}
	labels[api.ConfigHashLabel] = hash
	return client.VolumeCreateOptions{Name: config.Name, Driver: config.Driver, DriverOpts: config.DriverOpts, Labels: labels}, nil
}

func composeVolumeKeysWithExplicitNameInternal(composeFiles []string) (map[string]struct{}, error) {
	explicit := make(map[string]struct{})
	for _, composeFile := range composeFiles {
		composeFile = strings.TrimSpace(composeFile)
		if composeFile == "" {
			continue
		}
		keys, err := composeVolumeKeysWithExplicitNameInFileInternal(composeFile)
		if err != nil {
			return nil, err
		}
		for key := range keys {
			explicit[key] = struct{}{}
		}
	}
	return explicit, nil
}

func composeVolumeKeysWithExplicitNameInFileInternal(path string) (map[string]struct{}, error) {
	// os.* rather than acfs: compose config files may live outside the project
	// directory (e.g. via include), so no single confinement root covers them.
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	composeData := map[string]any{}
	if unmarshalErr := yaml.Unmarshal(content, &composeData); unmarshalErr != nil {
		return nil, fmt.Errorf("parse compose file: %w", unmarshalErr)
	}

	rawVolumes, ok := composeData["volumes"]
	if !ok || rawVolumes == nil {
		return map[string]struct{}{}, nil
	}

	localVolumes, ok := rawVolumes.(map[string]any)
	if !ok {
		return nil, errors.New("parse compose file: volumes must be a mapping")
	}

	explicit := make(map[string]struct{})
	for key, rawVolume := range localVolumes {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		volumeConfig, localOk := rawVolume.(map[string]any)
		if !localOk {
			continue
		}
		rawName, hasName := volumeConfig["name"]
		if !hasName {
			continue
		}
		name, localOk := rawName.(string)
		if localOk && len(composetemplate.ExtractVariables(map[string]any{"name": name}, nil)) == 0 {
			explicit[key] = struct{}{}
		}
	}

	return explicit, nil
}

func ApplyRenameVolumeMigration(ctx context.Context,
	operations projecttypes.RenameRecoveryOperations,
	volumeMigration volumetypes.Migration,
	renameJournal *projecttypes.RenameJournal,
	applied *bool,
) error {
	if volumeMigration == nil {
		return nil
	}
	if err := volumeMigration.Apply(ctx); err != nil {
		return fmt.Errorf("failed to rename project volumes: %w", err)
	}
	*applied = true
	return operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseTargetsCopied)
}

func FinalizeRenameAfterCommit(ctx context.Context,
	operations projecttypes.RenameRecoveryOperations,
	projectID string,
	volumeMigration volumetypes.Migration,
	renameJournal *projecttypes.RenameJournal,
	journalActive *bool,
) {
	if renameJournal != nil {
		if err := operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseProjectStateCommitted); err != nil {
			slog.WarnContext(ctx, "failed to mark project rename journal committed", "projectId", projectID, "error", err)
		}
	}

	if committer, ok := volumeMigration.(volumetypes.Committer); ok {
		if err := committer.Commit(ctx); err != nil {
			slog.WarnContext(ctx, "failed to clean up project source volumes after committed rename", "projectId", projectID, "error", err)
			if _, localOk := errors.AsType[*volumetypes.SourceCleanupError](err); localOk {
				if writeErr := operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseSourceCleanupPending); writeErr != nil {
					slog.WarnContext(ctx, "failed to mark project rename source cleanup pending", "projectId", projectID, "error", writeErr)
				}
			}
			return
		} else if writeJournalErr := operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseOldVolumesRemoved); writeJournalErr != nil {
			slog.WarnContext(ctx, "failed to mark old project rename volumes removed", "projectId", projectID, "error", writeJournalErr)
		}
	}

	completeRenameJournalInternal(ctx, operations, renameJournal, projectID, journalActive)
}

func completeRenameJournalInternal(ctx context.Context, operations projecttypes.RenameRecoveryOperations, renameJournal *projecttypes.RenameJournal, projectID string, journalActive *bool) {
	if renameJournal == nil {
		return
	}
	if clearErr := operations.ClearJournal(ctx, projectID); clearErr != nil {
		slog.WarnContext(ctx, "failed to clear project rename journal", "projectId", projectID, "error", clearErr)
		return
	}
	*journalActive = false
}

func renameJournalTargetsCopiedInternal(phase string) bool {
	switch phase {
	case projecttypes.RenameJournalPhaseTargetsCopied,
		projecttypes.RenameJournalPhaseOldVolumesRemoved,
		projecttypes.RenameJournalPhaseProjectStateCommitted,
		projecttypes.RenameJournalPhaseSourceCleanupPending:
		return true
	default:
		return false
	}
}

func RenameJournalFilesystemSyncPending(phase string) bool {
	switch phase {
	case projecttypes.RenameJournalPhaseStarted,
		projecttypes.RenameJournalPhaseTargetsCopied:
		return true
	default:
		return false
	}
}

func cleanupRenameJournalSourcesInternal(ctx context.Context, operations projecttypes.RenameRecoveryOperations, journal *projecttypes.RenameJournal) error {
	dockerClient, err := operations.Docker(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	if ensureTargetsReadyForCleanupErr := volumes.EnsureTargetsReadyForCleanup(ctx, dockerClient, journal.Volumes); ensureTargetsReadyForCleanupErr != nil {
		if missingWithSource, ok := errors.AsType[*volumetypes.TargetMissingWithSourceError](ensureTargetsReadyForCleanupErr); ok {
			slog.WarnContext(ctx,
				"rolling back project rename because target volume is missing and source volume remains",
				"projectId",
				journal.ProjectID,
				"sourceVolume",
				missingWithSource.SourceVolume,
				"targetVolume",
				missingWithSource.TargetVolume)
			return rollbackRenameJournalInternal(ctx, operations, journal)
		}
		if externallyRemoved, ok := errors.AsType[*volumetypes.VolumesExternallyRemovedError](ensureTargetsReadyForCleanupErr); ok {
			slog.WarnContext(ctx,
				"project rename cleanup found source and target volumes externally removed",
				"projectId",
				journal.ProjectID,
				"volumeCount",
				len(externallyRemoved.Volumes),
				"error",
				externallyRemoved)
		} else {
			return ensureTargetsReadyForCleanupErr
		}
	}

	return volumes.RemoveSourceVolumes(ctx, dockerClient, journal.Volumes)
}

func rollbackRenameJournalInternal(ctx context.Context, operations projecttypes.RenameRecoveryOperations, journal *projecttypes.RenameJournal) error {
	pathsMissing, directoryErr := RollbackRenamedProjectDirectory(ctx, journal.OldPath, journal.NewPath)

	volumeErr := rollbackRenameJournalVolumesInternal(ctx, operations, journal)

	if err := operations.RestoreState(ctx, journal); err != nil {
		return errors.Join(directoryErr, volumeErr, fmt.Errorf("restore project database state: %w", err))
	}

	if directoryErr != nil {
		slog.WarnContext(ctx,
			"keeping project rename journal after restoring database state because directory rollback failed",
			"projectId",
			journal.ProjectID,
			"pathsMissing",
			pathsMissing,
			"error",
			directoryErr)
	}

	if volumeErr != nil {
		if volumes.OnlyPreservedTargetErrors(volumeErr) {
			slog.WarnContext(ctx, "clearing project rename journal after preserving target volume data", "projectId", journal.ProjectID, "pathsMissing", pathsMissing, "error", volumeErr)
		} else {
			if cleanupErr := operations.WriteRollbackCleanup(ctx, journal); cleanupErr != nil {
				return errors.Join(directoryErr, volumeErr, cleanupErr)
			}
			slog.WarnContext(ctx,
				"queued project rename target volume cleanup after restoring database state despite volume rollback failure",
				"projectId",
				journal.ProjectID,
				"pathsMissing",
				pathsMissing,
				"error",
				volumeErr)
		}
	}

	return directoryErr
}

func rollbackRenameJournalVolumesInternal(ctx context.Context, operations projecttypes.RenameRecoveryOperations, journal *projecttypes.RenameJournal) error {
	if !renameJournalTargetsCopiedInternal(journal.Phase) {
		return nil
	}

	dockerClient, err := operations.Docker(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	return volumes.RollbackVolumes(ctx, dockerClient, journal.Volumes)
}

func RecoverRenameJournal(ctx context.Context, journal *projecttypes.RenameJournal, projectCommitted bool, operations projecttypes.RenameRecoveryOperations) error {
	if projectCommitted {
		if journal.Phase == projecttypes.RenameJournalPhaseSourceCleanupPending {
			if err := cleanupRenameJournalSourcesInternal(ctx, operations, journal); err != nil {
				return err
			}
			return operations.ClearJournal(ctx, journal.ProjectID)
		}
		if err := cleanupRenameJournalSourcesInternal(ctx, operations, journal); err != nil {
			if _, ok := errors.AsType[*volumetypes.SourceCleanupError](err); ok {
				if writeErr := operations.WriteJournal(ctx, journal, projecttypes.RenameJournalPhaseSourceCleanupPending); writeErr != nil {
					return errors.Join(err, writeErr)
				}
			}
			return err
		}
		return operations.ClearJournal(ctx, journal.ProjectID)
	}

	if err := rollbackRenameJournalInternal(ctx, operations, journal); err != nil {
		return err
	}
	if err := operations.WriteJournal(ctx, journal, projecttypes.RenameJournalPhaseProjectStateRolledBack); err != nil {
		return err
	}
	return operations.ClearJournal(ctx, journal.ProjectID)
}

func CleanupRenameRollbackTargets(ctx context.Context, cleanup *projecttypes.RenameRollbackCleanup, operations projecttypes.RenameRecoveryOperations) error {
	dockerClient, err := operations.Docker(ctx, len(cleanup.Volumes) > 0)
	if err != nil {
		return err
	}

	if cleanupRollbackTargetVolumesErr := volumes.CleanupRollbackTargetVolumes(ctx, dockerClient, cleanup.Volumes); cleanupRollbackTargetVolumesErr != nil {
		if volumes.OnlyPreservedTargetErrors(cleanupRollbackTargetVolumesErr) {
			slog.WarnContext(ctx, "clearing project rename rollback cleanup after preserving target volume data", "projectId", cleanup.ProjectID, "error", cleanupRollbackTargetVolumesErr)
			return operations.ClearRollbackCleanup(ctx, cleanup.ProjectID)
		}
		return cleanupRollbackTargetVolumesErr
	}

	docker.InvalidateVolumeUsageCache(dockerClient)
	return operations.ClearRollbackCleanup(ctx, cleanup.ProjectID)
}
