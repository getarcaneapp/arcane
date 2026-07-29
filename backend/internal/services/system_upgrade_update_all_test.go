package services

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateAllResolveResumeAction(t *testing.T) {
	now := time.Now()

	newJob := func(createdAt time.Time, versionAtStart, digestAtStart string) *models.EnvironmentUpdateJob {
		job := &models.EnvironmentUpdateJob{
			ManagerVersionAtStart: versionAtStart,
			ManagerDigestAtStart:  digestAtStart,
		}
		job.CreatedAt = createdAt
		return job
	}

	tests := []struct {
		name           string
		job            *models.EnvironmentUpdateJob
		currentVersion string
		currentDigest  string
		wantStale      bool
		wantManagerOK  bool
	}{
		{
			name:           "stale job is failed regardless of version",
			job:            newJob(now.Add(-2*time.Hour), "1.0.0", "sha256:a"),
			currentVersion: "1.1.0",
			currentDigest:  "sha256:b",
			wantStale:      true,
		},
		{
			name:           "version changed means manager upgraded",
			job:            newJob(now.Add(-5*time.Minute), "1.0.0", "sha256:a"),
			currentVersion: "1.1.0",
			currentDigest:  "sha256:a",
			wantManagerOK:  true,
		},
		{
			name:           "digest changed means manager upgraded (digest-pinned install)",
			job:            newJob(now.Add(-5*time.Minute), "latest", "sha256:a"),
			currentVersion: "latest",
			currentDigest:  "sha256:b",
			wantManagerOK:  true,
		},
		{
			name:           "nothing changed means manager upgrade did not take",
			job:            newJob(now.Add(-5*time.Minute), "1.0.0", "sha256:a"),
			currentVersion: "1.0.0",
			currentDigest:  "sha256:a",
			wantManagerOK:  false,
		},
		{
			name: "unchanged but already on target means force-update succeeded",
			job: func() *models.EnvironmentUpdateJob {
				job := newJob(now.Add(-5*time.Minute), "1.0.0", "sha256:a")
				job.ManagerTargetVersion = "v1.0.0"
				return job
			}(),
			currentVersion: "1.0.0",
			currentDigest:  "sha256:a",
			wantManagerOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveResumeActionInternal(tt.job, tt.currentVersion, tt.currentDigest, now)
			if got.markStale != tt.wantStale {
				t.Fatalf("markStale = %v, want %v", got.markStale, tt.wantStale)
			}
			if !tt.wantStale && got.managerSucceeded != tt.wantManagerOK {
				t.Fatalf("managerSucceeded = %v, want %v", got.managerSucceeded, tt.wantManagerOK)
			}
		})
	}
}

// A force-update with an unknown latest (offline or rate-limited version check)
// must still record a target — the current identifiers — so the resume check can
// recognize a same-image recreation as success instead of finalizing it as failed.
func TestUpdateAllTargetVersionFallsBackToCurrent(t *testing.T) {
	require.Equal(t, "v2.0.0", updateAllTargetVersionInternal(&version.Info{NewestVersion: "v2.0.0", CurrentVersion: "v1.2.3"}))
	require.Equal(t, "v1.2.3", updateAllTargetVersionInternal(&version.Info{CurrentVersion: "v1.2.3", CurrentDigest: "sha256:a"}))
	require.Equal(t, "sha256:a", updateAllTargetVersionInternal(&version.Info{CurrentDigest: "sha256:a"}))
}

func TestUpsertPendingResult(t *testing.T) {
	job := &models.EnvironmentUpdateJob{
		Results: models.EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: models.EnvironmentUpdateResultStatusUpdated},
			{EnvironmentID: "abc", EnvironmentName: "palladium", Status: models.EnvironmentUpdateResultStatusPending},
		},
	}

	// A seeded environment resolves to its existing row without appending.
	if idx := upsertPendingResultInternal(job, "abc", "palladium"); idx != 1 {
		t.Fatalf("existing env index = %d, want 1", idx)
	}
	if len(job.Results) != 2 {
		t.Fatalf("results grew to %d, want 2", len(job.Results))
	}

	// A missing environment (seeding raced or a new env was registered) appends a
	// fresh pending row and returns the new index.
	idx := upsertPendingResultInternal(job, "xyz", "oracle-cloud")
	if idx != 2 {
		t.Fatalf("new env index = %d, want 2", idx)
	}
	if len(job.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(job.Results))
	}
	got := job.Results[2]
	if got.EnvironmentID != "xyz" || got.EnvironmentName != "oracle-cloud" {
		t.Fatalf("appended row = %+v, want id=xyz name=oracle-cloud", got)
	}
	if got.Status != models.EnvironmentUpdateResultStatusPending {
		t.Fatalf("appended row status = %q, want pending", got.Status)
	}
}

func TestUpdateAllFailedJobMarksUpdatingResultsFailed(t *testing.T) {
	ctx := context.Background()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&models.EnvironmentUpdateJob{}, &models.Event{}))

	svc := NewSystemUpgradeService(db, nil, nil, NewEventService(db, nil, nil), nil)
	job := &models.EnvironmentUpdateJob{
		Status:   models.EnvironmentUpdateJobStatusRunning,
		UserID:   "user-1",
		Username: "arcane",
		Results: models.EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: models.EnvironmentUpdateResultStatusUpdated},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: models.EnvironmentUpdateResultStatusUpdating},
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: models.EnvironmentUpdateResultStatusPending},
			{EnvironmentID: "remote-3", EnvironmentName: "parquetide", Status: models.EnvironmentUpdateResultStatusFailed, Error: "already failed"},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	reason := "interrupted by manager restart"
	svc.markUpdateAllFailedInternal(ctx, job, reason)

	var got models.EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)
	require.Equal(t, models.EnvironmentUpdateJobStatusFailed, got.Status)
	require.NotNil(t, got.Error)
	require.Equal(t, reason, *got.Error)
	require.NotNil(t, got.CompletedAt)
	require.Len(t, got.Results, 4)

	require.Equal(t, models.EnvironmentUpdateResultStatusUpdated, got.Results[0].Status)
	require.Empty(t, got.Results[0].Error)

	require.Equal(t, models.EnvironmentUpdateResultStatusFailed, got.Results[1].Status)
	require.Equal(t, reason, got.Results[1].Error)

	require.Equal(t, models.EnvironmentUpdateResultStatusPending, got.Results[2].Status)
	require.Empty(t, got.Results[2].Error)

	require.Equal(t, models.EnvironmentUpdateResultStatusFailed, got.Results[3].Status)
	require.Equal(t, "already failed", got.Results[3].Error)
}

// An up-to-date manager never restarts: its upgrader pulls, finds the image already
// running and skips the recreate. The pending_restart job must then be finalized in
// place, because no next boot is coming to do it.
func TestUpdateAllFinalizesUpToDateManagerWithoutRestart(t *testing.T) {
	ctx := context.Background()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&models.EnvironmentUpdateJob{}, &models.Event{}))

	svc := NewSystemUpgradeService(db, nil, nil, NewEventService(db, nil, nil), nil)
	job := &models.EnvironmentUpdateJob{
		Status:                models.EnvironmentUpdateJobStatusPendingRestart,
		UserID:                "user-1",
		Username:              "arcane",
		ManagerVersionAtStart: "v1.0.0",
		Results: models.EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: models.EnvironmentUpdateResultStatusUpdating, FromVersion: "v1.0.0"},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: models.EnvironmentUpdateResultStatusUpToDate},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.recordManagerResultInternal(job, models.EnvironmentUpdateResultStatusUpToDate, "v1.0.0")
	svc.finalizeUpdateAllJobInternal(ctx, job)

	var got models.EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)

	// Nothing is left waiting on a restart that will never happen.
	require.Equal(t, models.EnvironmentUpdateJobStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.Nil(t, got.Error)

	require.Equal(t, "0", got.Results[0].EnvironmentID)
	require.Equal(t, models.EnvironmentUpdateResultStatusUpToDate, got.Results[0].Status)
	require.Equal(t, "v1.0.0", got.Results[0].ToVersion)
	require.Empty(t, got.Results[0].Error)
	require.Equal(t, models.EnvironmentUpdateResultStatusUpToDate, got.Results[1].Status)
}

// The up-to-date short-circuit must only fire when the version check is conclusive:
// an agent that could not resolve a newest version or digest keeps the full
// trigger-and-confirm flow rather than being reported as already current.
func TestAgentAlreadyOnTarget(t *testing.T) {
	tests := []struct {
		name string
		info version.Info
		want bool
	}{
		{
			name: "on newest version",
			info: version.Info{CurrentVersion: "1.2.3", NewestVersion: "v1.2.3"},
			want: true,
		},
		{
			name: "update available",
			info: version.Info{CurrentVersion: "1.2.3", NewestVersion: "v1.3.0", UpdateAvailable: true},
			want: false,
		},
		{
			name: "on newest digest",
			info: version.Info{CurrentDigest: "sha256:abc", NewestDigest: "sha256:abc"},
			want: true,
		},
		{
			// A mutable tag rebuilt at the same version: the semver track reports no
			// update, but the differing digest means the pull really will replace the
			// image, so this must not be treated as already current.
			name: "same version, rebuilt digest",
			info: version.Info{
				CurrentVersion: "1.2.3",
				NewestVersion:  "v1.2.3",
				CurrentDigest:  "sha256:old",
				NewestDigest:   "sha256:new",
			},
			want: false,
		},
		{
			// Only one digest resolved, so a rebuild cannot be ruled out: the matching
			// version tag must not be enough on its own.
			name: "same version, remote digest unresolved",
			info: version.Info{
				CurrentVersion: "1.2.3",
				NewestVersion:  "v1.2.3",
				CurrentDigest:  "sha256:running",
			},
			want: false,
		},
		{
			name: "inconclusive check resolves nothing",
			info: version.Info{CurrentVersion: "1.2.3"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, agentAlreadyOnTargetInternal(&tt.info))
		})
	}
}

// With the manager-last ordering, a resumed pending_restart job means the agents
// phase already ran before the restart: resume must finalize the manager's own row
// and complete the job, NOT re-run the agents phase.
func TestResumeUpdateAllFinalizesManagerWithoutRerunningAgents(t *testing.T) {
	ctx := context.Background()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&models.EnvironmentUpdateJob{}, &models.Event{}))

	// disabled=true keeps GetAppVersionInfo offline; nil docker => empty current digest.
	// The reported version differs from ManagerVersionAtStart, so the manager upgrade
	// is judged successful.
	versionSvc := NewVersionService(nil, true, "v9.9.9-new", "", nil, nil, nil)
	svc := NewSystemUpgradeService(db, nil, versionSvc, NewEventService(db, nil, nil), nil)

	job := &models.EnvironmentUpdateJob{
		Status:                models.EnvironmentUpdateJobStatusPendingRestart,
		UserID:                "user-1",
		Username:              "arcane",
		ManagerVersionAtStart: "v1.0.0-old",
		Results: models.EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: models.EnvironmentUpdateResultStatusUpdating},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: models.EnvironmentUpdateResultStatusUpdated},
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: models.EnvironmentUpdateResultStatusSkippedOffline},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.ResumeUpdateAllOnStartup(ctx)

	var got models.EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)

	// Job is finalized in-process (no re-run, not left running/pending).
	require.Equal(t, models.EnvironmentUpdateJobStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.Len(t, got.Results, 3)

	// Manager row transitioned updating -> updated (version changed across the restart).
	require.Equal(t, "0", got.Results[0].EnvironmentID)
	require.Equal(t, models.EnvironmentUpdateResultStatusUpdated, got.Results[0].Status)
	require.NotEmpty(t, got.Results[0].ToVersion)

	// Remote rows are untouched — proving the agents phase was NOT re-run on resume.
	require.Equal(t, models.EnvironmentUpdateResultStatusUpdated, got.Results[1].Status)
	require.Equal(t, models.EnvironmentUpdateResultStatusSkippedOffline, got.Results[2].Status)
}
