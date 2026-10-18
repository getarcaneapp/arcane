package rustic

import (
	"slices"
	"strings"

	"github.com/moby/moby/api/types/mount"

	"getarcane.dev/arcane/v3/pkg/volumes"
)

// windowsContainerPathsInternal moves the Linux paths Arcane uses inside the Rustic
// container to the C: drive of a Windows container: the mount targets, and the
// command arguments and environment values that lie under one of them. Snapshot
// paths such as "<id>:/dir" or "--as-path /" are not container paths and are kept.
func windowsContainerPathsInternal(command, env []string, mounts []mount.Mount) ([]string, []string, []mount.Mount) {
	translated := make([]mount.Mount, len(mounts))
	targets := make([]string, 0, len(mounts))
	for i, m := range mounts {
		if !strings.HasPrefix(m.Target, "/") {
			translated[i] = m
			continue
		}
		targets = append(targets, m.Target)
		translated[i] = mount.Mount{
			Type:           m.Type,
			Source:         m.Source,
			Target:         volumes.WindowsContainerPath(m.Target),
			ReadOnly:       m.ReadOnly,
			Consistency:    m.Consistency,
			BindOptions:    m.BindOptions,
			VolumeOptions:  m.VolumeOptions,
			ImageOptions:   m.ImageOptions,
			TmpfsOptions:   m.TmpfsOptions,
			ClusterOptions: m.ClusterOptions,
		}
	}
	localize := func(value string) string {
		for _, target := range targets {
			if value == target || strings.HasPrefix(value, target+"/") {
				return volumes.WindowsContainerPath(value)
			}
		}
		return value
	}

	command = slices.Clone(command)
	for i := range command {
		command[i] = localize(command[i])
	}
	env = slices.Clone(env)
	for i, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		// RUSTIC_REPO_OPT_* are opendal repository options (bucket, root, endpoint, region...),
		// not local container paths. An S3 root such as "/volume/..." looks like a mounted path
		// but must stay verbatim, or the repository would differ between mounting and non-mounting
		// commands (backup vs list/browse).
		if strings.HasPrefix(key, "RUSTIC_REPO_OPT_") {
			continue
		}
		env[i] = key + "=" + localize(value)
	}
	return command, env, translated
}
