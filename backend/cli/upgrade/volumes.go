package upgrade

import (
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

// preserveWindowsVolumeBinds pins the volumes of a Windows container like
// docker.PreserveVolumeMounts does on Linux. That helper splits binds on ':', which
// breaks drive letters, so it misses name:C:\path binds and adds a second mount on
// the same path, which Windows refuses. Volumes are kept as -v binds here, which
// Docker also matches against VOLUME paths declared by the image.
func preserveWindowsVolumeBinds(binds []string, mounts []mount.Mount, mountPoints []container.MountPoint) ([]string, []mount.Mount) {
	binds = slices.Clone(binds)
	mounts = slices.Clone(mounts)

	for _, mp := range mountPoints {
		volumeName := strings.TrimSpace(mp.Name)
		if mp.Type != mount.TypeVolume || volumeName == "" {
			continue
		}
		if i := slices.IndexFunc(mounts, func(m mount.Mount) bool {
			return strings.EqualFold(strings.TrimRight(m.Target, `\/`), strings.TrimRight(mp.Destination, `\/`))
		}); i >= 0 {
			if mounts[i].Type == mount.TypeVolume {
				mounts[i] = mount.Mount{
					Type:           mounts[i].Type,
					Source:         volumeName,
					Target:         mounts[i].Target,
					ReadOnly:       mounts[i].ReadOnly,
					Consistency:    mounts[i].Consistency,
					BindOptions:    mounts[i].BindOptions,
					VolumeOptions:  mounts[i].VolumeOptions,
					ImageOptions:   mounts[i].ImageOptions,
					TmpfsOptions:   mounts[i].TmpfsOptions,
					ClusterOptions: mounts[i].ClusterOptions,
				}
			}
			continue
		}
		if i := slices.IndexFunc(binds, func(bind string) bool {
			parts := splitWindowsBind(bind)
			return strings.EqualFold(strings.TrimRight(parts[min(1, len(parts)-1)], `\/`), strings.TrimRight(mp.Destination, `\/`))
		}); i >= 0 {
			// An anonymous volume bind holds only the destination.
			if len(splitWindowsBind(binds[i])) == 1 {
				binds[i] = volumeName + ":" + binds[i]
			}
			continue
		}
		// Image-declared volume without a bind: name it explicitly.
		binds = append(binds, volumeName+":"+mp.Destination)
	}
	return binds, mounts
}

// splitWindowsBind splits source:destination[:mode], keeping drive letters (C:\...) whole.
func splitWindowsBind(bind string) []string {
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
