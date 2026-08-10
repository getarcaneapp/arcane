//go:build unix

package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeLifecyclePathAccess_ReportsPathMetadata(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	scriptPath := filepath.Join(scriptsDir, "pre-deploy.sh")

	require.NoError(t, os.MkdirAll(scriptsDir, 0o750))
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755))

	got := describeLifecyclePathAccessInternal(dir, scriptPath)

	assert.Contains(t, got, "Arcane process identity: uid=")
	assert.Contains(t, got, "Path inspection:")
	assert.Contains(t, got, scriptsDir)
	assert.Contains(t, got, scriptPath)
	assert.Contains(t, got, "permissions=0750")
	assert.Contains(t, got, "permissions=0755")
	assert.Contains(t, got, "type=directory")
	assert.Contains(t, got, "type=regular")
	assert.Contains(t, got, "A script mode of 0755 is not sufficient")
}

func TestDescribeLifecyclePathAccess_StopsAtFirstInaccessibleComponent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses ordinary filesystem permission checks")
	}

	dir := t.TempDir()
	blockedDir := filepath.Join(dir, "blocked")
	scriptPath := filepath.Join(blockedDir, "pre-deploy.sh")

	require.NoError(t, os.MkdirAll(blockedDir, 0o700))
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(blockedDir, 0o000))
	t.Cleanup(func() {
		_ = os.Chmod(blockedDir, 0o700)
	})

	got := describeLifecyclePathAccessInternal(dir, scriptPath)

	assert.Contains(t, got, fmt.Sprintf("%q: mode=", blockedDir))
	assert.Contains(t, got, fmt.Sprintf("%q: lstat ", scriptPath))
	assert.Contains(t, got, "permission denied")
	assert.NotContains(t, got, fmt.Sprintf("%q: mode=", scriptPath))
}

func TestLifecycleDiagnosticPaths_RemainsInsideProject(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "scripts", "pre-deploy.sh")

	got := lifecycleDiagnosticPathsInternal(dir, scriptPath)

	require.Len(t, got, 3)
	assert.Equal(t, dir, got[0])
	assert.Equal(t, filepath.Join(dir, "scripts"), got[1])
	assert.Equal(t, scriptPath, got[2])
}

func TestValidateScriptPath_PermissionDeniedIncludesDiagnostics(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses ordinary filesystem permission checks")
	}

	dir := t.TempDir()
	blockedDir := filepath.Join(dir, "blocked")
	scriptPath := filepath.Join(blockedDir, "pre-deploy.sh")

	require.NoError(t, os.MkdirAll(blockedDir, 0o700))
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(blockedDir, 0o000))
	t.Cleanup(func() {
		_ = os.Chmod(blockedDir, 0o700)
	})

	err := validateScriptPathInternal(t.Context(), dir, "blocked/pre-deploy.sh")

	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Contains(t, err.Error(), "Arcane pre-deploy validation")
	assert.Contains(t, err.Error(), "Arcane process identity: uid=")
	assert.Contains(t, err.Error(), blockedDir)
	assert.Contains(t, err.Error(), scriptPath)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Contains(t, err.Error(), "A script mode of 0755 is not sufficient")
}
