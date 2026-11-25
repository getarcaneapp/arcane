package backup

import (
	"cmp"
	"context"
	"path"
	"path/filepath"
	"slices"
	"time"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"getarcane.dev/arcane/v3/internal/backup"
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

func snapshotTags(projectID, backupID string) []string {
	return []string{backup.ProjectSnapshotTag, "arcane-project:" + projectID, backup.RunSnapshotTag(backupID)}
}

func snapshotRun(entry *ProjectBackup, p plan) backup.SnapshotRun {
	return backup.SnapshotRun{
		Snapshots:          entry.snapshots(),
		RunID:              entry.ID,
		Label:              entry.ProjectName,
		Tags:               snapshotTags(entry.ProjectID, entry.ID),
		DestinationOptions: backup.DestinationOptions{Local: p.LocalEnabled, Remote: p.S3Enabled},
	}
}

func snapshotInput(projectMounts []mount.Mount, metaVolume, backupID string, p plan) backup.CreateSnapshotInput {
	mounts := slices.Clone(projectMounts)
	for _, name := range p.volumeNames() {
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: name, Target: path.Join(snapshotRoot, "volumes", name), ReadOnly: true})
	}
	mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: metaVolume, Target: path.Join(snapshotRoot, "meta"), ReadOnly: true})
	return backup.CreateSnapshotInput{Mounts: mounts, Sources: []string{snapshotRoot}, AsPath: "/", Tags: snapshotTags(p.ProjectID, backupID)}
}

func nestedTargets(mounts []mount.Mount) []string {
	targets := make([]string, 0, len(mounts)-1)
	for _, nested := range mounts[1:] {
		targets = append(targets, nested.Target)
	}
	return targets
}

func projectSourceMounts(ctx context.Context, dockerClient *client.Client, source backup.SourceMountOptions) ([]mount.Mount, error) {
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return nil, err
	}
	resolved, err := backup.SourceMounts(backup.SourceMountOptions{
		Mounts:        mounts,
		ContainerPath: source.ContainerPath,
		Target:        source.Target,
		InContainer:   inContainer,
		Writable:      source.Writable,
	})
	if err != nil {
		return nil, err
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
