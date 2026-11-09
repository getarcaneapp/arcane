//go:build unix

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/arcane/v3/pkg/projects"
)

func describeLifecyclePathAccess(ctx context.Context, projectPath, resolvedScriptPath string) string {
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
		entry, err := acfs.Host{}.Stat(ctx, candidate, acfstypes.StatOptions{})
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %q: %v", candidate, err))
			break
		}
		lines = append(lines, fmt.Sprintf(
			"  %q: mode=%s permissions=%04o uid=%d gid=%d type=%s",
			candidate,
			os.FileMode(entry.UnixMode),
			os.FileMode(entry.UnixMode).Perm(),
			entry.UID,
			entry.GID,
			func() string {
				switch {
				case entry.IsSymlink:
					return "symlink"
				case entry.IsDirectory:
					return "directory"
				case os.FileMode(entry.UnixMode).IsRegular():
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
