package rustic

import (
	"slices"
	"strings"

	"github.com/moby/moby/api/types/mount"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
)

// windowsContainerPathsInternal moves the Linux paths Arcane uses inside the Rustic
// container to the C: drive of a Windows container: the mount targets, and the
// command arguments and environment values that lie under one of them. Snapshot
// paths such as "<id>:/dir" or "--as-path /" are not container paths and are kept.
func windowsContainerPathsInternal(command, env []string, mounts []mount.Mount) ([]string, []string, []mount.Mount) {
	mounts = slices.Clone(mounts)
	targets := make([]string, 0, len(mounts))
	for i := range mounts {
		if strings.HasPrefix(mounts[i].Target, "/") {
			targets = append(targets, mounts[i].Target)
			mounts[i].Target = volumehelper.WindowsContainerPath(mounts[i].Target)
		}
	}
	localize := func(value string) string {
		for _, target := range targets {
			if value == target || strings.HasPrefix(value, target+"/") {
				return volumehelper.WindowsContainerPath(value)
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
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[i] = key + "=" + localize(value)
		}
	}
	return command, env, mounts
}
