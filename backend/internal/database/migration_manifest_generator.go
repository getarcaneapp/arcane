package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var releaseTagPatternInternal = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

func GenerateAppMigrationVersionsFromGit(ctx context.Context, repoRoot string, includeVersion string) ([]AppMigrationVersion, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}

	tagsOutput, err := runGitInternal(ctx, repoRoot, "tag", "--list", "v*")
	if err != nil {
		return nil, err
	}

	versionsByAppVersion := make(map[string]uint)
	for _, tag := range strings.Fields(tagsOutput) {
		if !releaseTagPatternInternal.MatchString(tag) {
			continue
		}

		migrationVersion, err := highestMigrationVersionForGitRefInternal(ctx, repoRoot, tag)
		if err != nil {
			return nil, err
		}
		if migrationVersion == 0 {
			continue
		}

		versionsByAppVersion[normalizeAppVersionInternal(tag)] = migrationVersion
	}

	includeVersion = normalizeAppVersionInternal(includeVersion)
	if includeVersion != "" {
		migrationVersion, err := highestWorkingTreeMigrationVersionInternal(repoRoot)
		if err != nil {
			return nil, err
		}
		versionsByAppVersion[includeVersion] = migrationVersion
	}

	versions := make([]AppMigrationVersion, 0, len(versionsByAppVersion))
	for appVersion, migrationVersion := range versionsByAppVersion {
		versions = append(versions, AppMigrationVersion{
			AppVersion:       appVersion,
			MigrationVersion: migrationVersion,
		})
	}

	slices.SortFunc(versions, func(a, b AppMigrationVersion) int {
		return compareAppVersionsInternal(a.AppVersion, b.AppVersion)
	})

	return versions, nil
}

func MarshalAppMigrationVersionManifest(versions []AppMigrationVersion) ([]byte, error) {
	manifestBytes, err := json.MarshalIndent(migrationVersionManifest{Versions: versions}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal migration version manifest: %w", err)
	}

	return append(manifestBytes, '\n'), nil
}

func runGitInternal(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return string(output), nil
}

func highestMigrationVersionForGitRefInternal(ctx context.Context, repoRoot, ref string) (uint, error) {
	output, err := runGitInternal(ctx, repoRoot, "ls-tree", "-r", "--name-only", ref, "--", "backend/resources/migrations/sqlite")
	if err != nil {
		return 0, err
	}

	return highestMigrationVersionFromPathsInternal(strings.Fields(output)), nil
}

func highestWorkingTreeMigrationVersionInternal(repoRoot string) (uint, error) {
	migrationDir := filepath.Join(repoRoot, "backend", "resources", "migrations", "sqlite")
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read working tree migrations from %s: %w", migrationDir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		paths = append(paths, entry.Name())
	}

	version := highestMigrationVersionFromPathsInternal(paths)
	if version == 0 {
		return 0, fmt.Errorf("no working tree migrations found in %s", migrationDir)
	}

	return version, nil
}

func highestMigrationVersionFromPathsInternal(paths []string) uint {
	var highest uint
	for _, path := range paths {
		name := filepath.Base(path)
		versionPart, _, found := strings.Cut(name, "_")
		if !found || !strings.HasSuffix(name, ".up.sql") {
			continue
		}

		version, err := strconv.ParseUint(versionPart, 10, 0)
		if err != nil {
			continue
		}
		if uint(version) > highest {
			highest = uint(version)
		}
	}

	return highest
}

func compareAppVersionsInternal(a, b string) int {
	aParts := appVersionPartsInternal(a)
	bParts := appVersionPartsInternal(b)

	for i := range aParts {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}

	return 0
}

func appVersionPartsInternal(version string) [3]int {
	matches := releaseTagPatternInternal.FindStringSubmatch("v" + normalizeAppVersionInternal(version))
	if len(matches) != 4 {
		return [3]int{}
	}

	var parts [3]int
	for i := range parts {
		part, err := strconv.Atoi(matches[i+1])
		if err != nil {
			return [3]int{}
		}
		parts[i] = part
	}

	return parts
}
