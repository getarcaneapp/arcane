package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/gitrepo"
	git "github.com/getarcaneapp/arcane/backend/v3/pkg/gitutil"
)

func TestGitCloneCleanupJob_NameAndSchedule(t *testing.T) {
	job := NewGitCloneCleanupJob(nil, nil)

	assert.Equal(t, GitCloneCleanupJobName, job.Name())
	assert.Equal(t, "0 30 * * * *", job.Schedule(t.Context()))
}

// TestGitCloneCleanupJob_RunPurgesStaleCloneDirs pins the nil-settings guard.
func TestGitCloneCleanupJob_RunPurgesStaleCloneDirs(t *testing.T) {
	workDir := t.TempDir()
	job := NewGitCloneCleanupJob(&gitrepo.GitRepositoryService{Client: git.NewClient(workDir)}, nil)

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

func TestGitCloneCleanupJob_RunNilRepoServiceIsNoop(t *testing.T) {
	job := NewGitCloneCleanupJob(nil, nil)

	assert.NotPanics(t, func() {
		_, runErr2 := job.Run(t.Context())
		require.NoError(t, runErr2)
	})
}
