package upgrade

import (
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

// preserveWindowsVolumeBindsInternal pins the volumes of a Windows container like
// docker.PreserveVolumeMounts does on Linux. That helper splits binds on ':', which
// breaks drive letters, so it misses name:C:\path binds and adds a second mount on
// the same path, which Windows refuses. Volumes are kept as -v binds here, which
// Docker also matches against VOLUME paths declared by the image.
func preserveWindowsVolumeBindsInternal(binds []string, mounts []mount.Mount, mountPoints []container.MountPoint) ([]string, []mount.Mount) {
	binds = slices.Clone(binds)
	mounts = slices.Clone(mounts)

	for _, mp := range mountPoints {
		volumeName := strings.TrimSpace(mp.Name)
		if mp.Type != mount.TypeVolume || volumeName == "" {
			continue
		}
		if i := slices.IndexFunc(mounts, func(m mount.Mount) bool { return sameWindowsPathInternal(m.Target, mp.Destination) }); i >= 0 {
			if mounts[i].Type == mount.TypeVolume {
				mounts[i].Source = volumeName
			}
			continue
		}
		if i := slices.IndexFunc(binds, func(bind string) bool {
			return sameWindowsPathInternal(windowsBindDestinationInternal(bind), mp.Destination)
		}); i >= 0 {
			// An anonymous volume bind holds only the destination.
			if len(splitWindowsBindInternal(binds[i])) == 1 {
				binds[i] = volumeName + ":" + binds[i]
			}
			continue
		}
		// Image-declared volume without a bind: name it explicitly.
		binds = append(binds, volumeName+":"+mp.Destination)
	}
	return binds, mounts
}

// splitWindowsBindInternal splits source:destination[:mode], keeping drive letters (C:\...) whole.
func splitWindowsBindInternal(bind string) []string {
	raw := strings.Split(bind, ":")
	parts := make([]string, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if len(raw[i]) == 1 && i+1 < len(raw) && strings.HasPrefix(raw[i+1], `\`) {
			parts = append(parts, raw[i]+":"+raw[i+1])
			i++
			continue
		}
		parts = append(parts, raw[i])
	}
	return parts
}

func windowsBindDestinationInternal(bind string) string {
	parts := splitWindowsBindInternal(bind)
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[1]
}

func sameWindowsPathInternal(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(a, `\/`), strings.TrimRight(b, `\/`))
}
