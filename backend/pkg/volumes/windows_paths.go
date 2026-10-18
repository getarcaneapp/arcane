package volumes

import (
	"slices"
	"strings"

	"github.com/moby/moby/api/types/mount"
)

// WindowsContainerPath returns the Windows container path for a Linux path used
// inside helper containers ("/volume" is "C:\volume"). Paths that are not absolute
// Linux paths are returned unchanged. Inside a Windows container, programs resolve
// "/volume" against the current drive, so commands can keep the Linux spelling;
// only Docker mount targets need the Windows one.
func WindowsContainerPath(linuxPath string) string {
	if !strings.HasPrefix(linuxPath, "/") {
		return linuxPath
	}
	return `C:` + strings.ReplaceAll(linuxPath, "/", `\`)
}

// windowsMountsInternal moves bind ("source:/target[:mode]") and mount targets to the C: drive.
func windowsMountsInternal(binds []string, mounts []mount.Mount) ([]string, []mount.Mount) {
	binds = slices.Clone(binds)
	for i, bind := range binds {
		parts := strings.SplitN(bind, ":", 3)
		if len(parts) >= 2 {
			parts[1] = WindowsContainerPath(parts[1])
			binds[i] = strings.Join(parts, ":")
		}
	}
	translated := make([]mount.Mount, len(mounts))
	for i, m := range mounts {
		translated[i] = mount.Mount{
			Type:           m.Type,
			Source:         m.Source,
			Target:         WindowsContainerPath(m.Target),
			ReadOnly:       m.ReadOnly,
			Consistency:    m.Consistency,
			BindOptions:    m.BindOptions,
			VolumeOptions:  m.VolumeOptions,
			ImageOptions:   m.ImageOptions,
			TmpfsOptions:   m.TmpfsOptions,
			ClusterOptions: m.ClusterOptions,
		}
	}
	return binds, translated
}
