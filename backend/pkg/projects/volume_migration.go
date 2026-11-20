package projects

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"

	composetemplate "github.com/compose-spec/compose-go/v2/template"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app/acfs"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/updater/labels"

	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	ResolveProjectVolumesOptions struct {
		// Inspect returns the named Docker volume and whether it exists.
		Inspect func(ctx context.Context, name string) (types.Volume, bool, error)
		// InUse names the volumes containers outside the project mount.
		InUse   map[string]bool
		Project types.Project
	}

	ComposeVolumeTargetNameOptions struct {
		SourceProject string
		Key           string
		SourceName    string
		TargetProject string
	}
)

// unnamedVolumeName matches the names Docker gives volumes created without one.
var unnamedVolumeName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func PlanVolumeMigration(ctx context.Context, dockerClient *client.Client, composeProject *composetypes.Project, oldComposeName, newComposeName, toolsImage string) (types.VolumeMigration, error) {
	if composeProject == nil || len(composeProject.Volumes) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(oldComposeName) == "" || strings.TrimSpace(newComposeName) == "" || oldComposeName == newComposeName {
		return nil, nil
	}

	explicitVolumeNames, err := composeVolumeKeysWithExplicitName(ctx, composeProject.ComposeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose volume names: %w", err)
	}

	entries, err := collectProjectRenameVolumeEntries(ctx, dockerClient, composeProject.Volumes, explicitVolumeNames, oldComposeName, newComposeName)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	return volumes.NewMigration(volumes.MigrationConfig{
		DockerClient: dockerClient,
		Entries:      entries,
		OldProject:   oldComposeName,
		NewProject:   newComposeName,
		ToolsImage:   toolsImage,
	}), nil
}

func collectProjectRenameVolumeEntries(ctx context.Context,
	dockerClient *client.Client,
	localVolumes map[string]composetypes.VolumeConfig,
	explicitVolumeNames map[string]struct{},
	oldComposeName,
	newComposeName string) ([]types.VolumeRenameEntry,
	error,
) {
	entries := make([]types.VolumeRenameEntry, 0, len(localVolumes))
	for key, volumeConfig := range localVolumes {
		entry, ok, err := inspectProjectRenameVolumeEntry(ctx, dockerClient, key, volumeConfig, explicitVolumeNames, oldComposeName, newComposeName)
		if err != nil {
			return nil, err
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func inspectProjectRenameVolumeEntry(ctx context.Context,
	dockerClient *client.Client,
	key string,
	volumeConfig composetypes.VolumeConfig,
	explicitVolumeNames map[string]struct{},
	oldComposeName,
	newComposeName string) (types.VolumeRenameEntry,
	bool,
	error,
) {
	if _, explicit := explicitVolumeNames[key]; explicit || bool(volumeConfig.External) {
		return types.VolumeRenameEntry{}, false, nil
	}

	oldName := oldComposeName + "_" + key
	newName := newComposeName + "_" + key
	if oldName == newName || (volumeConfig.Name != oldName && volumeConfig.Name != newName) {
		return types.VolumeRenameEntry{}, false, nil
	}

	if err := volumes.EnsureRenameTargetAbsent(ctx, dockerClient, newName); err != nil {
		return types.VolumeRenameEntry{}, false, err
	}

	oldVolume, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return types.VolumeRenameEntry{}, false, nil
		}
		return types.VolumeRenameEntry{}, false, fmt.Errorf("inspect source volume %s: %w", oldName, err)
	}
	if ensureRenameSourceDetachedErr := volumes.EnsureRenameSourceDetached(ctx, dockerClient, oldName); ensureRenameSourceDetachedErr != nil {
		return types.VolumeRenameEntry{}, false, ensureRenameSourceDetachedErr
	}

	newConfig := ComposeVolumeConfig(volumeConfig, key, newName, newComposeName)
	createOptions, err := ComposeVolumeCreateOptions(newConfig, oldVolume.Volume.Labels)
	if err != nil {
		return types.VolumeRenameEntry{}, false, err
	}
	return types.VolumeRenameEntry{
		Key:           key,
		OldName:       oldName,
		NewName:       newName,
		OldVolume:     oldVolume.Volume,
		CreateOptions: createOptions,
	}, true, nil
}

func LocalVolume(driver string, options map[string]string) bool {
	return cmp.Or(driver, "local") == "local" && len(options) == 0
}

func ClassifyProjectVolumes(project types.Project) []types.ProjectVolume {
	classified := make([]types.ProjectVolume, 0, len(project.Volumes))
	for _, key := range slices.Sorted(maps.Keys(project.Volumes)) {
		config := project.Volumes[key]
		kind := types.ProjectVolumeNamed
		switch {
		case bool(config.External):
			kind = types.ProjectVolumeExternal
		case len(config.DriverOpts) > 0:
			kind = types.ProjectVolumeDriverOpts
		}
		classified = append(classified, types.ProjectVolume{
			Labels:     config.Labels,
			DriverOpts: config.DriverOpts,
			Key:        key,
			Name:       config.Name,
			Kind:       kind,
			Driver:     config.Driver,
			Included:   kind == types.ProjectVolumeNamed || (kind == types.ProjectVolumeExternal && LocalVolume(config.Driver, config.DriverOpts)),
		})
	}
	return classified
}

// ResolveProjectVolumes classifies the project's volumes and includes each one that exists and keeps its data in Docker.
// It also returns the Docker volumes it inspected, by name.
func ResolveProjectVolumes(ctx context.Context, opts ResolveProjectVolumesOptions) ([]types.ProjectVolume, map[string]types.Volume, error) {
	classified := ClassifyProjectVolumes(opts.Project)
	resolved := make([]types.ProjectVolume, 0, len(classified))
	inspected := map[string]types.Volume{}
	for _, volume := range classified {
		if !volume.Included {
			resolved = append(resolved, volume)
			continue
		}
		existing, exists, err := opts.Inspect(ctx, volume.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("inspect volume %s: %w", volume.Name, err)
		}
		included := exists && (volume.Kind == types.ProjectVolumeNamed || LocalVolume(existing.Driver, existing.Options))
		inspected[volume.Name] = existing
		resolved = append(resolved, types.ProjectVolume{
			Labels:     volume.Labels,
			DriverOpts: volume.DriverOpts,
			Key:        volume.Key,
			Name:       volume.Name,
			Kind:       volume.Kind,
			Driver:     volume.Driver,
			Included:   included,
			Missing:    !exists,
			InUse:      included && opts.InUse[volume.Name],
		})
	}
	return resolved, inspected, nil
}

// ProjectContainerUse returns the Compose project's running and restarting containers, the ones a stop stops, and the
// volumes every other container mounts. Arcane's own containers are never stopped, so they count as outside.
func ProjectContainerUse(composeName string, containers []container.Summary) ([]container.Summary, map[string]bool) {
	var active []container.Summary
	inUse := map[string]bool{}
	for _, c := range containers {
		if composeName != "" && docker.ComposeProjectLabel(c.Labels) == composeName && !labels.IsArcaneContainer(c.Labels) {
			if c.State == container.StateRunning || c.State == container.StateRestarting {
				active = append(active, c)
			}
			continue
		}
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume {
				inUse[m.Name] = true
			}
		}
	}
	return active, inUse
}

// UnnamedVolumeMount finds a container mount that keeps data in an unnamed volume the project doesn't declare: only that
// container links the volume to its service, so removing the container, as taking the project down does, leaves the
// data behind. A volume the project declares by name, even a generated one, is attached again by Compose.
func UnnamedVolumeMount(project types.Project, containers []container.Summary) (container.Summary, container.MountPoint, bool) {
	declared := map[string]bool{}
	for _, volume := range ClassifyProjectVolumes(project) {
		declared[volume.Name] = true
	}
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && unnamedVolumeName.MatchString(m.Name) && !declared[m.Name] {
				return c, m, true
			}
		}
	}
	return container.Summary{}, container.MountPoint{}, false
}

func ComposeVolumeTargetName(opts ComposeVolumeTargetNameOptions) string {
	if opts.SourceName == NormalizeProjectName(opts.SourceProject)+"_"+opts.Key {
		return NormalizeProjectName(opts.TargetProject) + "_" + opts.Key
	}
	return opts.SourceName
}

func ComposeVolumeTargetCreateOptions(name string, target types.ComposeVolumeTarget) (client.VolumeCreateOptions, error) {
	if target.External {
		return client.VolumeCreateOptions{
			Name:   name,
			Driver: target.Driver,
			Labels: target.Labels,
		}, nil
	}
	return ComposeVolumeCreateOptions(ComposeVolumeConfig(composetypes.VolumeConfig{
		Driver: target.Driver,
		Labels: target.Labels,
	}, target.Key, name, target.Project), nil)
}

func ComposeVolumeConfig(volumeConfig composetypes.VolumeConfig, key, newName, newComposeName string) composetypes.VolumeConfig {
	newConfig := volumeConfig
	newConfig.Name = newName
	newConfig.CustomLabels = composetypes.Labels{
		api.VolumeLabel:  key,
		api.ProjectLabel: newComposeName,
		api.VersionLabel: api.ComposeVersion,
	}
	return newConfig
}

func ComposeVolumeCreateOptions(config composetypes.VolumeConfig, oldLabels map[string]string) (client.VolumeCreateOptions, error) {
	volumeLabels := map[string]string{}
	maps.Copy(volumeLabels, oldLabels)
	maps.Copy(volumeLabels, config.Labels)
	maps.Copy(volumeLabels, config.CustomLabels)
	raw, err := json.Marshal(composetypes.VolumeConfig{
		Name:       config.Name,
		Driver:     cmp.Or(config.Driver, "local"),
		DriverOpts: config.DriverOpts,
		External:   config.External,
		Labels:     config.Labels,
	}, json.Deterministic(true), json.OmitZeroStructFields(true), jsontext.EscapeForHTML(true))
	if err != nil {
		return client.VolumeCreateOptions{}, fmt.Errorf("hash target volume %s: %w", config.Name, err)
	}
	volumeLabels[api.ConfigHashLabel] = digest.SHA256.FromBytes(raw).Encoded()
	return client.VolumeCreateOptions{Name: config.Name, Driver: config.Driver, DriverOpts: config.DriverOpts, Labels: volumeLabels}, nil
}

func composeVolumeKeysWithExplicitName(ctx context.Context, composeFiles []string) (map[string]struct{}, error) {
	explicit := make(map[string]struct{})
	for _, composeFile := range composeFiles {
		composeFile = strings.TrimSpace(composeFile)
		if composeFile == "" {
			continue
		}
		keys, err := composeVolumeKeysWithExplicitNameInFile(ctx, composeFile)
		if err != nil {
			return nil, err
		}
		for key := range keys {
			explicit[key] = struct{}{}
		}
	}
	return explicit, nil
}

func composeVolumeKeysWithExplicitNameInFile(ctx context.Context, path string) (map[string]struct{}, error) {
	content, err := acfs.Host{}.ReadFile(ctx, path)
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
	operations types.ProjectRenameRecoveryOperations,
	volumeMigration types.VolumeMigration,
	renameJournal *types.ProjectRenameJournal,
	applied *bool,
) error {
	if volumeMigration == nil {
		return nil
	}
	if err := volumeMigration.Apply(ctx); err != nil {
		return fmt.Errorf("failed to rename project volumes: %w", err)
	}
	*applied = true
	return operations.WriteJournal(ctx, renameJournal, types.ProjectRenameJournalPhaseTargetsCopied)
}

func FinalizeRenameAfterCommit(ctx context.Context,
	operations types.ProjectRenameRecoveryOperations,
	projectID string,
	volumeMigration types.VolumeMigration,
	renameJournal *types.ProjectRenameJournal,
	journalActive *bool,
) {
	if renameJournal != nil {
		if err := operations.WriteJournal(ctx, renameJournal, types.ProjectRenameJournalPhaseProjectStateCommitted); err != nil {
			slog.WarnContext(ctx, "failed to mark project rename journal committed", "projectId", projectID, "error", err)
		}
	}

	if committer, ok := volumeMigration.(types.VolumeRenameCommitter); ok {
		if err := committer.Commit(ctx); err != nil {
			slog.WarnContext(ctx, "failed to clean up project source volumes after committed rename", "projectId", projectID, "error", err)
			if _, localOk := errors.AsType[*types.SourceCleanupError](err); localOk {
				if writeErr := operations.WriteJournal(ctx, renameJournal, types.ProjectRenameJournalPhaseSourceCleanupPending); writeErr != nil {
					slog.WarnContext(ctx, "failed to mark project rename source cleanup pending", "projectId", projectID, "error", writeErr)
				}
			}
			return
		} else if writeJournalErr := operations.WriteJournal(ctx, renameJournal, types.ProjectRenameJournalPhaseOldVolumesRemoved); writeJournalErr != nil {
			slog.WarnContext(ctx, "failed to mark old project rename volumes removed", "projectId", projectID, "error", writeJournalErr)
		}
	}

	completeRenameJournal(ctx, operations, renameJournal, projectID, journalActive)
}

func completeRenameJournal(ctx context.Context, operations types.ProjectRenameRecoveryOperations, renameJournal *types.ProjectRenameJournal, projectID string, journalActive *bool) {
	if renameJournal == nil {
		return
	}
	if clearErr := operations.ClearJournal(ctx, projectID); clearErr != nil {
		slog.WarnContext(ctx, "failed to clear project rename journal", "projectId", projectID, "error", clearErr)
		return
	}
	*journalActive = false
}

func renameJournalTargetsCopied(phase string) bool {
	switch phase {
	case types.ProjectRenameJournalPhaseTargetsCopied,
		types.ProjectRenameJournalPhaseOldVolumesRemoved,
		types.ProjectRenameJournalPhaseProjectStateCommitted,
		types.ProjectRenameJournalPhaseSourceCleanupPending:
		return true
	default:
		return false
	}
}

func RenameJournalFilesystemSyncPending(phase string) bool {
	switch phase {
	case types.ProjectRenameJournalPhaseStarted,
		types.ProjectRenameJournalPhaseTargetsCopied:
		return true
	default:
		return false
	}
}

func cleanupRenameJournalSources(ctx context.Context, operations types.ProjectRenameRecoveryOperations, journal *types.ProjectRenameJournal) error {
	dockerClient, err := operations.Docker(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	if ensureTargetsReadyForCleanupErr := volumes.EnsureTargetsReadyForCleanup(ctx, dockerClient, journal.Volumes); ensureTargetsReadyForCleanupErr != nil {
		if missingWithSource, ok := errors.AsType[*types.TargetMissingWithSourceError](ensureTargetsReadyForCleanupErr); ok {
			slog.WarnContext(ctx,
				"rolling back project rename because target volume is missing and source volume remains",
				"projectId",
				journal.ProjectID,
				"sourceVolume",
				missingWithSource.SourceVolume,
				"targetVolume",
				missingWithSource.TargetVolume)
			return rollbackRenameJournal(ctx, operations, journal)
		}
		if externallyRemoved, ok := errors.AsType[*types.VolumesExternallyRemovedError](ensureTargetsReadyForCleanupErr); ok {
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

func rollbackRenameJournal(ctx context.Context, operations types.ProjectRenameRecoveryOperations, journal *types.ProjectRenameJournal) error {
	pathsMissing, directoryErr := RollbackRenamedProjectDirectory(ctx, journal.OldPath, journal.NewPath)

	volumeErr := rollbackRenameJournalVolumes(ctx, operations, journal)

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

func rollbackRenameJournalVolumes(ctx context.Context, operations types.ProjectRenameRecoveryOperations, journal *types.ProjectRenameJournal) error {
	if !renameJournalTargetsCopied(journal.Phase) {
		return nil
	}

	dockerClient, err := operations.Docker(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	return volumes.RollbackVolumes(ctx, dockerClient, journal.Volumes)
}

func RecoverRenameJournal(ctx context.Context, journal *types.ProjectRenameJournal, projectCommitted bool, operations types.ProjectRenameRecoveryOperations) error {
	if projectCommitted {
		if journal.Phase == types.ProjectRenameJournalPhaseSourceCleanupPending {
			if err := cleanupRenameJournalSources(ctx, operations, journal); err != nil {
				return err
			}
			return operations.ClearJournal(ctx, journal.ProjectID)
		}
		if err := cleanupRenameJournalSources(ctx, operations, journal); err != nil {
			if _, ok := errors.AsType[*types.SourceCleanupError](err); ok {
				if writeErr := operations.WriteJournal(ctx, journal, types.ProjectRenameJournalPhaseSourceCleanupPending); writeErr != nil {
					return errors.Join(err, writeErr)
				}
			}
			return err
		}
		return operations.ClearJournal(ctx, journal.ProjectID)
	}

	if err := rollbackRenameJournal(ctx, operations, journal); err != nil {
		return err
	}
	if err := operations.WriteJournal(ctx, journal, types.ProjectRenameJournalPhaseProjectStateRolledBack); err != nil {
		return err
	}
	return operations.ClearJournal(ctx, journal.ProjectID)
}

func CleanupRenameRollbackTargets(ctx context.Context, cleanup *types.ProjectRenameRollbackCleanup, operations types.ProjectRenameRecoveryOperations) error {
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
