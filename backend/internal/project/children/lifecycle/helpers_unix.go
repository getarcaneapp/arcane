//go:build unix

package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

func describeLifecyclePathAccess(projectPath, resolvedScriptPath string) string {
	lines := []string{
		fmt.Sprintf("Arcane process identity: uid=%d gid=%d.", os.Geteuid(), os.Getegid()),
		"Path inspection:",
	}

	// Walk from the project root down to the script; fall back to the endpoints when it resolves outside the project.
	absProject, projectErr := filepath.Abs(projectPath)
	absScript, scriptErr := filepath.Abs(resolvedScriptPath)
	relative, relErr := filepath.Rel(absProject, absScript)
	for _, candidate := range func() []string {
		switch {
		case projectErr != nil:
			return []string{projectPath, resolvedScriptPath}
		case scriptErr != nil:
			return []string{absProject, resolvedScriptPath}
		case relErr != nil || !projects.IsSafeSubdirectory(absProject, absScript):
			return []string{absProject, absScript}
		}
		paths := []string{absProject}
		for component := range strings.SplitSeq(relative, string(filepath.Separator)) {
			if component != "" && component != "." {
				paths = append(paths, filepath.Join(paths[len(paths)-1], component))
			}
		}
		return paths
	}() {
		// os.* rather than acfs: diagnostics walk arbitrary ancestors of a user-configured path with no confinement root.
		info, err := os.Lstat(candidate)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %q: %v", candidate, err))
			break
		}
		lines = append(lines, fmt.Sprintf(
			"  %q: mode=%s permissions=%04o %s type=%s",
			candidate,
			info.Mode(),
			info.Mode().Perm(),
			func() string {
				if stat, ok := info.Sys().(*syscall.Stat_t); ok {
					return fmt.Sprintf("uid=%d gid=%d", stat.Uid, stat.Gid)
				}
				return "uid=unknown gid=unknown"
			}(),
			func() string {
				switch {
				case info.Mode()&os.ModeSymlink != 0:
					return "symlink"
				case info.IsDir():
					return "directory"
				case info.Mode().IsRegular():
					return "regular"
				}
				return "other"
			}(),
		))
	}

	return strings.Join(append(lines,
		"Ensure the Arcane process can traverse every parent directory and inspect the script.",
		"A script mode of 0755 is not sufficient when a parent directory lacks execute permission or ownership, ACLs, or mount permissions block access.",
	), " ")
}
