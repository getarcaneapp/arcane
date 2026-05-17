package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func GenerateAppMigrationVersionsFromGit(ctx context.Context, repoRoot string, includeVersion string) ([]AppMigrationVersion, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}

	tagsOutput, err := runGitInternal(ctx, repoRoot, "tag", "--list", "v*", "--sort=v:refname")
	if err != nil {
		return nil, err
	}

	versions := make([]AppMigrationVersion, 0)
	for _, tag := range strings.Fields(tagsOutput) {
		appVersion := normalizeAppVersionInternal(tag)
		if !isReleaseVersionInternal(appVersion) {
			continue
		}

		migrationVersion, err := highestMigrationVersionForGitRefInternal(ctx, repoRoot, tag)
		if err != nil {
			return nil, err
		}
		if migrationVersion == 0 {
			continue
		}

		versions = append(versions, AppMigrationVersion{
			AppVersion:       appVersion,
			MigrationVersion: migrationVersion,
		})
	}

	includeVersion = normalizeAppVersionInternal(includeVersion)
	if includeVersion != "" {
		migrationVersion, err := highestWorkingTreeMigrationVersionInternal(repoRoot)
		if err != nil {
			return nil, err
		}

		versions = upsertAppMigrationVersionInternal(versions, AppMigrationVersion{
			AppVersion:       includeVersion,
			MigrationVersion: migrationVersion,
		})
	}
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
	paths, err := filepath.Glob(filepath.Join(migrationDir, "*.up.sql"))
	if err != nil {
		return 0, fmt.Errorf("failed to list working tree migrations from %s: %w", migrationDir, err)
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

func isReleaseVersionInternal(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func upsertAppMigrationVersionInternal(versions []AppMigrationVersion, version AppMigrationVersion) []AppMigrationVersion {
	for i := range versions {
		if versions[i].AppVersion == version.AppVersion {
			versions[i] = version
			return versions
		}
	}
	return append(versions, version)
}
