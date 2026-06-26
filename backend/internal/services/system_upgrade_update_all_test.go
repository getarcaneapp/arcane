package services

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	glsqlite "github.com/glebarez/sqlite"
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
	gormDB, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&models.EnvironmentUpdateJob{}, &models.Event{}))

	svc := NewSystemUpgradeService(db, nil, nil, NewEventService(db, nil, nil), nil)
	job := &models.EnvironmentUpdateJob{
		Status:   models.EnvironmentUpdateJobStatusRunning,
		UserID:   "user-1",
		Username: "arcane",
		Results: models.EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: models.EnvironmentUpdateResultStatusSkippedUpToDate},
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

	require.Equal(t, models.EnvironmentUpdateResultStatusSkippedUpToDate, got.Results[0].Status)
	require.Empty(t, got.Results[0].Error)

	require.Equal(t, models.EnvironmentUpdateResultStatusFailed, got.Results[1].Status)
	require.Equal(t, reason, got.Results[1].Error)

	require.Equal(t, models.EnvironmentUpdateResultStatusPending, got.Results[2].Status)
	require.Empty(t, got.Results[2].Error)

	require.Equal(t, models.EnvironmentUpdateResultStatusFailed, got.Results[3].Status)
	require.Equal(t, "already failed", got.Results[3].Error)
}

// With the manager-last ordering, a resumed pending_restart job means the agents
// phase already ran before the restart: resume must finalize the manager's own row
// and complete the job, NOT re-run the agents phase.
func TestResumeUpdateAllFinalizesManagerWithoutRerunningAgents(t *testing.T) {
	ctx := context.Background()
	gormDB, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
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
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: models.EnvironmentUpdateResultStatusSkippedUpToDate},
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
	require.Equal(t, models.EnvironmentUpdateResultStatusSkippedUpToDate, got.Results[2].Status)
}
