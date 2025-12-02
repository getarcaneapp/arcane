//go:build !windows
// +build !windows

package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/moby/sys/mountinfo"
)

func gitPath(wd string) (string, error) {
	// On WSL2 we need to check if the current working directory is mounted on
	// a Windows drive and if so, we need to use the Windows git executable.
	if os.Getenv("WSL_DISTRO_NAME") != "" && wd != "" {
		// ensure any symlinks are resolved
		wdPath, err := filepath.EvalSymlinks(wd)
		if err != nil {
			return "", err
		}
		mi, err := mountinfo.GetMounts(mountinfo.ParentsFilter(wdPath))
		if err != nil {
			return "", err
		}
		// find the longest mount point
		var idx, maxlen int
		for i := range mi {
			if len(mi[i].Mountpoint) > maxlen {
				maxlen = len(mi[i].Mountpoint)
				idx = i
			}
		}
		if mi[idx].FSType == "9p" {
			if p, err := exec.LookPath("git.exe"); err == nil {
				return p, nil
			}
		}
	}
	return exec.LookPath("git")
}
