//go:build unix

package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func describeLifecyclePathAccess(projectPath, resolvedScriptPath string) string {
	var lines []string

	lines = append(lines,
		fmt.Sprintf(
			"Arcane process identity: uid=%d gid=%d.",
			os.Geteuid(),
			os.Getegid(),
		),
		"Path inspection:",
	)

	// Walk from the project root down to the script; fall back to the two
	// endpoints when the script resolves outside the project.
	paths := []string{projectPath, resolvedScriptPath}
	absProject, projectErr := filepath.Abs(projectPath)
	absScript, scriptErr := filepath.Abs(resolvedScriptPath)
	relative, relErr := filepath.Rel(absProject, absScript)
	switch {
	case projectErr != nil:
	case scriptErr != nil:
		paths = []string{absProject, resolvedScriptPath}
	case relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)):
		paths = []string{absProject, absScript}
	default:
		paths = []string{absProject}
		current := absProject
		for component := range strings.SplitSeq(relative, string(filepath.Separator)) {
			if component == "" || component == "." {
				continue
			}
			current = filepath.Join(current, component)
			paths = append(paths, current)
		}
	}

	for _, candidate := range paths {
		// os.* rather than acfs: diagnostics walk arbitrary ancestor directories
		// of a user-configured path, so no confinement root exists for them.
		info, err := os.Lstat(candidate)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %q: %v", candidate, err))
			break
		}

		owner := "uid=unknown gid=unknown"
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner = fmt.Sprintf("uid=%d gid=%d", stat.Uid, stat.Gid)
		}

		fileType := "other"
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			fileType = "symlink"
		case info.IsDir():
			fileType = "directory"
		case info.Mode().IsRegular():
			fileType = "regular"
		}

		lines = append(lines, fmt.Sprintf(
			"  %q: mode=%s permissions=%04o %s type=%s",
			candidate,
			info.Mode(),
			info.Mode().Perm(),
			owner,
			fileType,
		))
	}

	lines = append(lines,
		"Ensure the Arcane process can traverse every parent directory and inspect the script.",
		"A script mode of 0755 is not sufficient when a parent directory lacks execute permission or ownership, ACLs, or mount permissions block access.",
	)

	return strings.Join(lines, " ")
}
