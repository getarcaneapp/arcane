package backup

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"time"

	mobyMount "github.com/moby/moby/api/types/mount"
	mobyClient "github.com/moby/moby/client"

	kit "kit.getarcane.app"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

func (p plan) mountedVolumeNames() []string {
	var names []string
	for _, volume := range p.Volumes {
		if volume.Name != "" {
			names = append(names, volume.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func (p plan) volumeNames() []string {
	var names []string
	for _, volume := range p.Volumes {
		if volume.Included {
			names = append(names, volume.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func admissions(projectID string, volumeNames []string) []types.SchedulerAdmissionKey {
	return append([]types.SchedulerAdmissionKey{{Scope: admissionScope, ID: projectID}}, volumeAdmissions(volumeNames)...)
}

func volumeAdmissions(names []string) []types.SchedulerAdmissionKey {
	keys := make([]types.SchedulerAdmissionKey, 0, len(names))
	for _, name := range names {
		keys = append(keys, types.SchedulerAdmissionKey{Scope: backup.VolumeAdmissionScope, ID: name})
	}
	return keys
}

func release(ctx context.Context, leases []*runs.Lease) {
	for _, lease := range leases {
		lease.Release(ctx)
	}
}

func markInUse(planned []types.ProjectVolume, inUse map[string]bool) []types.ProjectVolume {
	marked := make([]types.ProjectVolume, 0, len(planned))
	for _, volume := range planned {
		marked = append(marked, types.ProjectVolume{ //exhaustruct:enforce
			Labels:       volume.Labels,
			DockerLabels: volume.DockerLabels,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      volume.Archive,
			Included:     volume.Included,
			Missing:      volume.Missing,
			InUse:        volume.Included && inUse[volume.Name],
			BackupStore:  volume.BackupStore,
		})
	}
	return marked
}

func snapshotTags(entry *ProjectBackup) []string {
	tags := []string{backup.ProjectSnapshotTag, projectTagPrefix + entry.ProjectID, triggerTagPrefix + entry.Trigger}
	if entry.PolicyID != "" {
		tags = append(tags, policyTagPrefix+entry.PolicyID)
	}
	if !entry.Files {
		tags = append(tags, volumesOnlyTag)
	}
	return tags
}

func snapshotRun(entry *ProjectBackup, p plan) backup.SnapshotRun {
	return backup.SnapshotRun{
		Snapshots:          entry.snapshots(),
		RunID:              entry.ID,
		Label:              entry.ProjectName,
		Tags:               append(snapshotTags(entry), backup.RunSnapshotTag(entry.ID)),
		DestinationOptions: backup.DestinationOptions{Local: p.LocalEnabled, Remote: p.S3Enabled},
	}
}

func snapshotInput(projectMounts []mobyMount.Mount, metaVolume string, entry *ProjectBackup, p plan) backup.CreateSnapshotInput {
	mounts := slices.Clone(projectMounts)
	for _, name := range p.volumeNames() {
		mounts = append(mounts, backup.VolumePart(name))
	}
	mounts = append(mounts, mobyMount.Mount{Type: mobyMount.TypeVolume, Source: metaVolume, Target: path.Join(backup.SnapshotRoot, "meta"), ReadOnly: true})
	return backup.CreateSnapshotInput{Mounts: mounts, Sources: []string{backup.SnapshotRoot}, AsPath: "/", Tags: snapshotTags(entry)}
}

func nestedTargets(mounts []mobyMount.Mount) []string {
	targets := make([]string, 0, len(mounts)-1)
	for _, nested := range mounts[1:] {
		targets = append(targets, nested.Target)
	}
	return targets
}

func projectSourceMounts(ctx context.Context, dockerClient *mobyClient.Client, source backup.SourceMountOptions) ([]mobyMount.Mount, error) {
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return nil, err
	}
	containerPath, err := filepath.EvalSymlinks(source.ContainerPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project directory: %w", err)
	}
	resolved, err := backup.SourceMounts(backup.SourceMountOptions{
		Mounts:        mounts,
		ContainerPath: containerPath,
		Target:        source.Target,
		InContainer:   inContainer,
		Writable:      source.Writable,
	})
	if err != nil {
		return nil, err
	}
	if store := docker.MountForDestination(mounts, "/backups", "/backups"); store != nil && store.Type == mobyMount.TypeBind && resolved[0].Type == mobyMount.TypeBind &&
		(kit.FilePathMatches(store.Source, resolved[0].Source) || kit.FilePathMatches(resolved[0].Source, store.Source)) {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf(
			"the project folder %s and Arcane's backup folder %s overlap on the host, so the project cannot be backed up or restored", containerPath, store.Source,
		))
	}
	return volumes.BindSubpaths(ctx, dockerClient, resolved)
}

func backupManifest(project types.Project, p plan, externalEnv bool) types.ProjectManifest {
	resolved, _ := filepath.EvalSymlinks(project.Path)
	return types.ProjectManifest{ //exhaustruct:enforce
		CreatedAt:     time.Now().UTC(),
		Kind:          types.ProjectManifestKindBackup,
		ArcaneVersion: config.Version,
		Project: types.ProjectManifestProject{ //exhaustruct:enforce
			ID:          project.ID,
			Name:        project.Name,
			ComposeName: project.ComposeName,
		},
		ProjectDirs:   slices.Compact([]string{project.Path, cmp.Or(resolved, project.Path)}),
		Volumes:       p.Volumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    p.StopContainers && !slices.ContainsFunc(p.Volumes, func(volume types.ProjectVolume) bool { return volume.InUse }),
		ExternalEnv:   externalEnv,
	}
}

func backupVolumes(classified []types.ProjectVolume) []types.ProjectBackupVolume {
	captured := make([]types.ProjectBackupVolume, 0, len(classified))
	for _, volume := range classified {
		captured = append(captured, types.ProjectBackupVolume{ //exhaustruct:enforce
			Key:      volume.Key,
			Name:     volume.Name,
			Kind:     volume.Kind,
			Included: volume.Included,
		})
	}
	return captured
}
