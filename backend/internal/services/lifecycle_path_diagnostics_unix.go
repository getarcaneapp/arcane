//go:build unix

package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func describeLifecyclePathAccessInternal(projectPath, resolvedScriptPath string) string {
	var lines []string

	lines = append(lines,
		fmt.Sprintf(
			"Arcane process identity: uid=%d gid=%d.",
			os.Geteuid(),
			os.Getegid(),
		),
		"Path inspection:",
	)

	for _, candidate := range lifecycleDiagnosticPathsInternal(projectPath, resolvedScriptPath) {
		info, err := os.Lstat(candidate)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %q: %v", candidate, err))
			break
		}

		owner := "uid=unknown gid=unknown"
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner = fmt.Sprintf("uid=%d gid=%d", stat.Uid, stat.Gid)
		}

		lines = append(lines, fmt.Sprintf(
			"  %q: mode=%s permissions=%04o %s type=%s",
			candidate,
			info.Mode(),
			info.Mode().Perm(),
			owner,
			lifecycleFileTypeInternal(info),
		))
	}

	lines = append(lines,
		"Ensure the Arcane process can traverse every parent directory and inspect the script.",
		"A script mode of 0755 is not sufficient when a parent directory lacks execute permission or ownership, ACLs, or mount permissions block access.",
	)

	return strings.Join(lines, " ")
}

func lifecycleDiagnosticPathsInternal(projectPath, resolvedScriptPath string) []string {
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return []string{projectPath, resolvedScriptPath}
	}

	absScript, err := filepath.Abs(resolvedScriptPath)
	if err != nil {
		return []string{absProject, resolvedScriptPath}
	}

	relative, err := filepath.Rel(absProject, absScript)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return []string{absProject, absScript}
	}

	paths := []string{absProject}
	current := absProject

	for component := range strings.SplitSeq(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		paths = append(paths, current)
	}

	return paths
}

func lifecycleFileTypeInternal(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	case info.IsDir():
		return "directory"
	case info.Mode().IsRegular():
		return "regular"
	default:
		return "other"
	}
}
