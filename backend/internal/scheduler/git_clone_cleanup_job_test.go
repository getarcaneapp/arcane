package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/gitrepo"
	git "getarcane.dev/arcane/v3/pkg/gitutil"
)

func TestGitCloneCleanupJob_RunPurgesStaleCloneDirs(t *testing.T) {
	workDir := t.TempDir()
	_, settingsService, _ := setupAnalyticsStateServices(t)
	job := NewGitCloneCleanupJob(&gitrepo.GitRepositoryService{Client: git.NewClient(workDir)}, settingsService)

	stale := filepath.Join(workDir, "gitops-stale")
	fresh := filepath.Join(workDir, "gitops-fresh")
	require.NoError(t, os.MkdirAll(stale, 0o755))
	require.NoError(t, os.MkdirAll(fresh, 0o755))
	staleTime := time.Now().Add(-3 * time.Hour)
	require.NoError(t, os.Chtimes(stale, staleTime, staleTime))

	_, runErr1 := job.Run(t.Context())
	require.NoError(t, runErr1)

	_, err := os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist, "stale clone dir should be removed")
	_, err = os.Stat(fresh)
	assert.NoError(t, err, "fresh clone dir must be kept")
}
