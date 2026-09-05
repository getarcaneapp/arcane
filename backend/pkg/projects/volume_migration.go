package projects

import (
	"context"
	stderrors "errors"
	"log/slog"
	"maps"
	"os"
	"strings"

	"emperror.dev/errors"
	composetemplate "github.com/compose-spec/compose-go/v2/template"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	composepkg "github.com/docker/compose/v5/pkg/compose"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v4"
)

func PlanVolumeMigration(ctx context.Context, dockerClient *client.Client, composeProject *composetypes.Project, oldComposeName, newComposeName string, toolsImage string) (volumetypes.Migration, error) {
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
		return nil, errors.WrapIf(err, "failed to parse compose volume names")
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

func collectProjectRenameVolumeEntriesInternal(ctx context.Context, dockerClient *client.Client, volumes map[string]composetypes.VolumeConfig, explicitVolumeNames map[string]struct{}, oldComposeName, newComposeName string) ([]volumetypes.RenameEntry, error) {
	entries := make([]volumetypes.RenameEntry, 0, len(volumes))
	for key, volumeConfig := range volumes {
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

func inspectProjectRenameVolumeEntryInternal(ctx context.Context, dockerClient *client.Client, key string, volumeConfig composetypes.VolumeConfig, explicitVolumeNames map[string]struct{}, oldComposeName, newComposeName string) (volumetypes.RenameEntry, bool, error) {
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
		return volumetypes.RenameEntry{}, false, errors.WrapIff(err, "inspect source volume %s", oldName)
	}
	if err := volumes.EnsureRenameSourceDetached(ctx, dockerClient, oldName); err != nil {
		return volumetypes.RenameEntry{}, false, err
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
		return client.VolumeCreateOptions{}, errors.WrapIff(err, "hash target volume %s", config.Name)
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
		return nil, errors.WrapIf(err, "read compose file")
	}

	composeData := map[string]any{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return nil, errors.WrapIf(err, "parse compose file")
	}

	rawVolumes, ok := composeData["volumes"]
	if !ok || rawVolumes == nil {
		return map[string]struct{}{}, nil
	}

	volumes, ok := rawVolumes.(map[string]any)
	if !ok {
		return nil, errors.New("parse compose file: volumes must be a mapping")
	}

	explicit := make(map[string]struct{})
	for key, rawVolume := range volumes {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		volumeConfig, ok := rawVolume.(map[string]any)
		if !ok {
			continue
		}
		rawName, hasName := volumeConfig["name"]
		if !hasName {
			continue
		}
		name, ok := rawName.(string)
		if ok && len(composetemplate.ExtractVariables(map[string]any{"name": name}, nil)) == 0 {
			explicit[key] = struct{}{}
		}
	}

	return explicit, nil
}

func ApplyRenameVolumeMigration(ctx context.Context, operations projecttypes.RenameRecoveryOperations, volumeMigration volumetypes.Migration, renameJournal *projecttypes.RenameJournal, applied *bool) error {
	if volumeMigration == nil {
		return nil
	}
	if err := volumeMigration.Apply(ctx); err != nil {
		return errors.WrapIf(err, "failed to rename project volumes")
	}
	*applied = true
	return operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseTargetsCopied)
}

func FinalizeRenameAfterCommit(ctx context.Context, operations projecttypes.RenameRecoveryOperations, projectID string, volumeMigration volumetypes.Migration, renameJournal *projecttypes.RenameJournal, journalActive *bool) {
	if renameJournal != nil {
		if err := operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseProjectStateCommitted); err != nil {
			slog.WarnContext(ctx, "failed to mark project rename journal committed", "projectID", projectID, "error", err)
		}
	}

	if committer, ok := volumeMigration.(volumetypes.Committer); ok {
		if err := committer.Commit(ctx); err != nil {
			slog.WarnContext(ctx, "failed to clean up project source volumes after committed rename", "projectID", projectID, "error", err)
			var cleanupErr *volumetypes.SourceCleanupError
			if errors.As(err, &cleanupErr) {
				if writeErr := operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseSourceCleanupPending); writeErr != nil {
					slog.WarnContext(ctx, "failed to mark project rename source cleanup pending", "projectID", projectID, "error", writeErr)
				}
			}
			return
		} else if err := operations.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseOldVolumesRemoved); err != nil {
			slog.WarnContext(ctx, "failed to mark old project rename volumes removed", "projectID", projectID, "error", err)
		}
	}

	completeRenameJournalInternal(ctx, operations, renameJournal, projectID, journalActive)
}

func completeRenameJournalInternal(ctx context.Context, operations projecttypes.RenameRecoveryOperations, renameJournal *projecttypes.RenameJournal, projectID string, journalActive *bool) {
	if renameJournal == nil {
		return
	}
	if clearErr := operations.ClearJournal(ctx, projectID); clearErr != nil {
		slog.WarnContext(ctx, "failed to clear project rename journal", "projectID", projectID, "error", clearErr)
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

	if err := volumes.EnsureTargetsReadyForCleanup(ctx, dockerClient, journal.Volumes); err != nil {
		var missingWithSource *volumetypes.TargetMissingWithSourceError
		if errors.As(err, &missingWithSource) {
			slog.WarnContext(ctx, "rolling back project rename because target volume is missing and source volume remains", "projectID", journal.ProjectID, "sourceVolume", missingWithSource.SourceVolume, "targetVolume", missingWithSource.TargetVolume)
			return rollbackRenameJournalInternal(ctx, operations, journal)
		}
		var externallyRemoved *volumetypes.VolumesExternallyRemovedError
		if errors.As(err, &externallyRemoved) {
			slog.WarnContext(ctx, "project rename cleanup found source and target volumes externally removed", "projectID", journal.ProjectID, "volumeCount", len(externallyRemoved.Volumes), "error", externallyRemoved)
		} else {
			return err
		}
	}

	return volumes.RemoveSourceVolumes(ctx, dockerClient, journal.Volumes)
}

func rollbackRenameJournalInternal(ctx context.Context, operations projecttypes.RenameRecoveryOperations, journal *projecttypes.RenameJournal) error {
	pathsMissing, directoryErr := RollbackRenamedProjectDirectory(ctx, journal.OldPath, journal.NewPath)

	volumeErr := rollbackRenameJournalVolumesInternal(ctx, operations, journal)

	if err := operations.RestoreState(ctx, journal); err != nil {
		return stderrors.Join(directoryErr, volumeErr, errors.WrapIf(err, "restore project database state"))
	}

	if directoryErr != nil {
		slog.WarnContext(ctx, "keeping project rename journal after restoring database state because directory rollback failed", "projectID", journal.ProjectID, "pathsMissing", pathsMissing, "error", directoryErr)
	}

	if volumeErr != nil {
		if volumes.OnlyPreservedTargetErrors(volumeErr) {
			slog.WarnContext(ctx, "clearing project rename journal after preserving target volume data", "projectID", journal.ProjectID, "pathsMissing", pathsMissing, "error", volumeErr)
		} else {
			if cleanupErr := operations.WriteRollbackCleanup(ctx, journal); cleanupErr != nil {
				return stderrors.Join(directoryErr, volumeErr, cleanupErr)
			}
			slog.WarnContext(ctx, "queued project rename target volume cleanup after restoring database state despite volume rollback failure", "projectID", journal.ProjectID, "pathsMissing", pathsMissing, "error", volumeErr)
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
			var cleanupErr *volumetypes.SourceCleanupError
			if errors.As(err, &cleanupErr) {
				if writeErr := operations.WriteJournal(ctx, journal, projecttypes.RenameJournalPhaseSourceCleanupPending); writeErr != nil {
					return stderrors.Join(err, writeErr)
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

	if err := volumes.CleanupRollbackTargetVolumes(ctx, dockerClient, cleanup.Volumes); err != nil {
		if volumes.OnlyPreservedTargetErrors(err) {
			slog.WarnContext(ctx, "clearing project rename rollback cleanup after preserving target volume data", "projectID", cleanup.ProjectID, "error", err)
			return operations.ClearRollbackCleanup(ctx, cleanup.ProjectID)
		}
		return err
	}

	dockerutil.InvalidateVolumeUsageCache(dockerClient)
	return operations.ClearRollbackCleanup(ctx, cleanup.ProjectID)
}
