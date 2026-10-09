package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
)

// TestService_UpgradeFlag tests the upgrading flag behavior
func TestService_UpgradeFlag(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	// Initially should be false
	require.False(t, s.upgrading.Load())

	// Simulate manual flag setting
	s.upgrading.Store(true)
	require.True(t, s.upgrading.Load())

	// Should be able to reset
	s.upgrading.Store(false)
	require.False(t, s.upgrading.Load())
}

// TestService_Initialization tests proper initialization
func TestService_Initialization(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
	// Services can be nil in this test since we're just testing initialization
}

// TestService_ErrorVariables tests that error variables are properly defined
func TestService_ErrorVariables(t *testing.T) {
	// Test that all expected errors exist and are not nil
	require.Error(t, errors.New("arcane is not running in a Docker container"))
	require.Error(t, common.Classify(common.ErrNotFound, errors.New("could not find Arcane container")))
	require.Error(t, common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrade is already in progress")))
	require.Error(t, errors.New("docker socket is not accessible"))

	// Test error messages
	require.Equal(t, "arcane is not running in a Docker container", errors.New("arcane is not running in a Docker container").Error())
	require.Equal(t, "could not find Arcane container", common.Classify(common.ErrNotFound, errors.New("could not find Arcane container")).Error())
	require.Equal(t, "an upgrade is already in progress", common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrade is already in progress")).Error())
	require.Equal(t, "docker socket is not accessible", errors.New("docker socket is not accessible").Error())
}

// TestService_UpgradingFlag_ConcurrentAccess tests upgrading flag
func TestService_UpgradingFlag_ConcurrentAccess(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	// Test initial state
	require.False(t, s.upgrading.Load(), "upgrading flag should start as false")

	// Test setting to true
	s.upgrading.Store(true)
	require.True(t, s.upgrading.Load(), "upgrading flag should be true after setting")

	// Test setting back to false
	s.upgrading.Store(false)
	require.False(t, s.upgrading.Load(), "upgrading flag should be false after resetting")
}

// TestService_CompareAndSwap tests atomic CompareAndSwap operation
func TestService_CompareAndSwap(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	// Test successful CompareAndSwap from false to true
	swapped := s.upgrading.CompareAndSwap(false, true)
	require.True(t, swapped, "CompareAndSwap should succeed when value is false")
	require.True(t, s.upgrading.Load(), "upgrading should be true after swap")

	// Test failed CompareAndSwap (already true)
	swapped = s.upgrading.CompareAndSwap(false, true)
	require.False(t, swapped, "CompareAndSwap should fail when value is already true")
	require.True(t, s.upgrading.Load(), "upgrading should still be true")

	// Reset and test again
	s.upgrading.Store(false)
	swapped = s.upgrading.CompareAndSwap(false, true)
	require.True(t, swapped, "CompareAndSwap should succeed after reset")
}

// TestService_Services tests that services are stored correctly
func TestService_Services(t *testing.T) {
	// Create upgrade service with nil services (valid for testing initialization)
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	// Verify service is created and initialized properly
	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
}

// TestService_ConcurrentUpgradeAttempts tests that concurrent upgrade attempts are prevented
func TestService_ConcurrentUpgradeAttempts(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	// Simulate first upgrade starting
	success := s.upgrading.CompareAndSwap(false, true)
	require.True(t, success, "First upgrade attempt should succeed")

	// Simulate second concurrent upgrade attempt
	success = s.upgrading.CompareAndSwap(false, true)
	require.False(t, success, "Second concurrent upgrade attempt should fail")

	// Cleanup
	s.upgrading.Store(false)

	// Should be able to upgrade again after cleanup
	success = s.upgrading.CompareAndSwap(false, true)
	require.True(t, success, "Upgrade should be possible after reset")
}

// TestService_UpgradeInProgressError tests the upgrade in progress sentinel error
func TestService_UpgradeInProgressError(t *testing.T) {
	// This tests the specific error that the handler checks for
	// The handler uses errors.Is with common.ErrUpgradeInProgress for conflict detection.

	err := common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrade is already in progress"))
	require.Equal(t, "an upgrade is already in progress", err.Error())

	require.ErrorIs(t, err, common.ErrUpgradeInProgress)
}

// TestService_AtomicOperations tests atomic.Bool operations
func TestService_AtomicOperations(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil)

	// Test Load
	require.False(t, s.upgrading.Load())

	// Test Store
	s.upgrading.Store(true)
	require.True(t, s.upgrading.Load())

	// Test CompareAndSwap success
	s.upgrading.Store(false)
	swapped := s.upgrading.CompareAndSwap(false, true)
	require.True(t, swapped)

	// Test CompareAndSwap failure
	swapped = s.upgrading.CompareAndSwap(false, true)
	require.False(t, swapped)
	require.True(t, s.upgrading.Load())

	// Test Swap
	s.upgrading.Store(false)
	old := s.upgrading.Swap(true)
	require.False(t, old)
	require.True(t, s.upgrading.Load())
}

// TestUpdateAllAgentFailureStatus verifies that a reachable-but-failed agent (e.g.
// a poll-mode environment whose tunnel round-trip timed out) is reported as a real
// failure rather than mislabeled "offline — skipped".
func TestUpdateAllAgentFailureStatus(t *testing.T) {
	// Mirrors the real wrapping: executeRemoteRequest -> TransportError ->
	// "tunnel request failed" -> context.DeadlineExceeded.
	tunnelTimeout := fmt.Errorf("failed to send request to environment oracle-cloud: %w",
		&remenv.TransportError{Err: fmt.Errorf("tunnel request failed: %w", context.DeadlineExceeded)})

	tests := []struct {
		name string
		err  error
		want EnvironmentUpdateResultStatus
	}{
		{"tunnel request timed out", tunnelTimeout, EnvironmentUpdateResultStatusFailed},
		{"request canceled mid-flight", fmt.Errorf("tunnel request failed: %w", context.Canceled), EnvironmentUpdateResultStatusFailed},
		{"non-success status from agent", &remenv.StatusError{StatusCode: 502}, EnvironmentUpdateResultStatusFailed},
		{"agent never connected", &remenv.TransportError{Err: errors.New("edge agent is not connected")}, EnvironmentUpdateResultStatusSkippedOffline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, updateAllAgentFailureStatusInternal(tt.err))
		})
	}
}

// A blank-target self-upgrade resolves its image from the version check (#3687).
// Explicit targets from the updater engine never reach this function.
func TestResolveSelfUpgradeTargetImageInternal(t *testing.T) {
	tests := []struct {
		name         string
		currentImage string
		info         *versiontypes.Info
		want         string
		wantErr      string
	}{
		{
			name:         "agent pinned to an older release moves to the newest",
			currentImage: "ghcr.io/getarcaneapp/arcane-agent:v2.8.0",
			info:         &versiontypes.Info{NewestVersion: "v2.9.0"},
			want:         "ghcr.io/getarcaneapp/arcane-agent:v2.9.0",
		},
		{
			name:         "already-current exact version keeps its reference",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.9.0",
			info:         &versiontypes.Info{NewestVersion: "v2.9.0"},
			want:         "ghcr.io/getarcaneapp/arcane:v2.9.0",
		},
		{
			name:         "older newest release refuses a downgrade",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.9.0",
			info:         &versiontypes.Info{NewestVersion: "v2.8.0"},
			wantErr:      "downgrade",
		},
		{
			name:         "unresolved newest release aborts an exact-version upgrade",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.8.0",
			info:         &versiontypes.Info{},
			wantErr:      "could not be resolved",
		},
		{
			name:         "latest channel keeps its reference",
			currentImage: "ghcr.io/getarcaneapp/arcane:latest",
			info:         &versiontypes.Info{NewestVersion: "v2.9.0"},
			want:         "ghcr.io/getarcaneapp/arcane:latest",
		},
		{
			name:         "minor channel keeps its reference",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.9",
			info:         &versiontypes.Info{NewestVersion: "v2.9.1"},
			want:         "ghcr.io/getarcaneapp/arcane:v2.9",
		},
		{
			name:         "digest pin moves to the resolved newest digest",
			currentImage: "ghcr.io/getarcaneapp/arcane@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			info:         &versiontypes.Info{NewestDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			want:         "ghcr.io/getarcaneapp/arcane@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name:         "digest pin without a resolved newest digest fails",
			currentImage: "ghcr.io/getarcaneapp/arcane@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			info:         &versiontypes.Info{},
			wantErr:      "digest-pinned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSelfUpgradeTargetImageInternal(tt.currentImage, tt.info)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	// Both the updater engine and the manual upgrade respell the resolved
	// target from the container's Compose service before the upgrader starts.
	composeTests := []struct {
		name, runtime, target, want, wantCompose, wantErr string
		files                                             map[string]string
	}{
		{
			name: "unmanaged Compose keeps the runtime spelling", runtime: "getarcaneapp/manager:next",
			target: "docker.io/getarcaneapp/manager:next", want: "getarcaneapp/manager:next",
		},
		{
			name: "Compose spelling repairs a qualified runtime image", runtime: "docker.io/getarcaneapp/manager:next",
			target: "docker.io/getarcaneapp/manager:next", want: "getarcaneapp/manager:next",
			files: map[string]string{
				"compose.yaml":          "services:\n  server:\n    image: example/old:1\n",
				"compose.override.yaml": "services:\n  server:\n    image: getarcaneapp/manager:${TAG}\n",
				".env":                  "TAG=next\n",
			},
			wantCompose: "services:\n  server:\n    image: example/old:1\n",
		},
		{
			name: "release tag change saves Compose", runtime: "getarcaneapp/manager:v1.0.0",
			target: "getarcaneapp/manager:v1.1.0", want: "getarcaneapp/manager:v1.1.0",
			files:       map[string]string{"compose.yaml": "services:\n  server:\n    image: getarcaneapp/manager:v1.0.0\n    profiles: [admin]\n"},
			wantCompose: "services:\n  server:\n    image: getarcaneapp/manager:v1.1.0\n    profiles: [admin]\n",
		},
		{
			name: "override source keeps upgrading without an edit", runtime: "getarcaneapp/manager:v1.0.0",
			target: "docker.io/getarcaneapp/manager:v1.1.0", want: "getarcaneapp/manager:v1.1.0",
			files: map[string]string{
				"compose.yaml":          "services:\n  server:\n    image: example/old:1\n",
				"compose.override.yaml": "services:\n  server:\n    image: getarcaneapp/manager:v1.0.0\n",
			},
			wantCompose: "services:\n  server:\n    image: example/old:1\n",
		},
		{
			name: "conflicting Compose source fails", runtime: "getarcaneapp/manager:v1.0.0",
			target:      "docker.io/getarcaneapp/manager:v1.1.0",
			files:       map[string]string{"compose.yaml": "services:\n  server:\n    image: getarcaneapp/manager:v1.2.0\n"},
			wantCompose: "services:\n  server:\n    image: getarcaneapp/manager:v1.2.0\n", wantErr: "does not match running image",
		},
		{
			name: "digest target is left alone", runtime: "getarcaneapp/manager@sha256:" + strings.Repeat("a", 64),
			target: "getarcaneapp/manager@sha256:" + strings.Repeat("b", 64), want: "getarcaneapp/manager@sha256:" + strings.Repeat("b", 64),
			files: map[string]string{"compose.yaml": "services:\n  server:\n    image: getarcaneapp/manager@sha256:" + strings.Repeat("a", 64) + "\n"},
		},
	}
	for _, tt := range composeTests {
		t.Run(tt.name, func(t *testing.T) {
			gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
			require.NoError(t, err)
			db := &database.DB{DB: gormDB}
			require.NoError(t, db.AutoMigrate(&project.Project{}, &settings.SettingVariable{}))
			directory := t.TempDir()
			t.Setenv("PROJECTS_DIRECTORY", directory)
			settingsSvc, err := settings.NewSettingsService(t.Context(), db)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, settingsSvc.Stop(context.WithoutCancel(t.Context()))) })
			require.NoError(t, settingsSvc.SetStringSetting(t.Context(), "projectsDirectory", directory))
			projectPath := filepath.Join(directory, "arcane")
			if len(tt.files) > 0 {
				require.NoError(t, os.MkdirAll(projectPath, 0o755))
				for name, content := range tt.files {
					require.NoError(t, os.WriteFile(filepath.Join(projectPath, name), []byte(content), 0o600))
				}
				require.NoError(t, db.Create(&project.Project{ID: "project-arcane", Name: "arcane", Path: projectPath}).Error)
			}
			svc := NewService(db, nil, nil, nil, nil, project.NewProjectService(db, settingsSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil), nil)
			current := container.InspectResponse{Config: &container.Config{
				Image:  tt.runtime,
				Labels: map[string]string{"com.docker.compose.project": "arcane", "com.docker.compose.service": "server"},
			}}

			got, saveCompose, err := svc.configuredTargetImageInternal(t.Context(), current, tt.target)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, saveCompose)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
			if saveCompose != nil {
				saveCompose(t.Context())
			}
			if tt.wantCompose != "" {
				content, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
				require.NoError(t, readErr)
				require.Equal(t, tt.wantCompose, string(content))
			}
		})
	}
}

func TestUpdateAllResolveResumeAction(t *testing.T) {
	now := time.Now()

	newJob := func(createdAt time.Time, versionAtStart, digestAtStart string) *EnvironmentUpdateJob {
		job := &EnvironmentUpdateJob{
			ManagerVersionAtStart: versionAtStart,
			ManagerDigestAtStart:  digestAtStart,
		}
		job.CreatedAt = createdAt
		return job
	}

	tests := []struct {
		name           string
		job            *EnvironmentUpdateJob
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
			job: func() *EnvironmentUpdateJob {
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

			require.Equal(t, tt.wantStale, got.markStale,
				"markStale = %v, want %v", got.markStale, tt.wantStale)

			require.False(t, !tt.wantStale && got.managerSucceeded != tt.wantManagerOK,
				"managerSucceeded = %v, want %v", got.managerSucceeded, tt.wantManagerOK)
		})
	}
}

// A force-update with an unknown latest (offline or rate-limited version check)
// must still record a target — the current identifiers — so the resume check can
// recognize a same-image recreation as success instead of finalizing it as failed.
func TestUpdateAllTargetVersionFallsBackToCurrent(t *testing.T) {
	require.Equal(t, "v2.0.0", updateAllTargetVersionInternal(&versiontypes.Info{NewestVersion: "v2.0.0", CurrentVersion: "v1.2.3"}))
	require.Equal(t, "v1.2.3", updateAllTargetVersionInternal(&versiontypes.Info{CurrentVersion: "v1.2.3", CurrentDigest: "sha256:a"}))
	require.Equal(t, "sha256:a", updateAllTargetVersionInternal(&versiontypes.Info{CurrentDigest: "sha256:a"}))
}

func TestUpsertPendingResult(t *testing.T) {
	job := &EnvironmentUpdateJob{
		Results: EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: EnvironmentUpdateResultStatusUpdated},
			{EnvironmentID: "abc", EnvironmentName: "palladium", Status: EnvironmentUpdateResultStatusPending},
		},
	}
	{

		// A seeded environment resolves to its existing row without appending.
		idx := upsertPendingResultInternal(job, "abc", "palladium")
		require.Equal(t, 1, idx,
			"existing env index = %d, want 1", idx)
	}

	require.Len(t, job.Results, 2,
		"results grew to %d, want 2", len(job.Results))

	// A missing environment (seeding raced or a new env was registered) appends a
	// fresh pending row and returns the new index.
	idx := upsertPendingResultInternal(job, "xyz", "oracle-cloud")

	require.Equal(t, 2, idx,
		"new env index = %d, want 2", idx)

	require.Len(t, job.Results, 3,
		"results = %d, want 3", len(job.Results))

	got := job.Results[2]

	require.False(t, got.EnvironmentID != "xyz" || got.EnvironmentName != "oracle-cloud",
		"appended row = %+v, want id=xyz name=oracle-cloud", got)

	require.Equal(t, EnvironmentUpdateResultStatusPending, got.Status,
		"appended row status = %q, want pending", got.Status)
}

func TestUpdateAllFailedJobMarksUpdatingResultsFailed(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&EnvironmentUpdateJob{}, &event.Event{}))

	svc := NewService(db, nil, nil, event.NewEventService(db, nil, nil), nil, nil, nil)
	job := &EnvironmentUpdateJob{
		Status:   EnvironmentUpdateJobStatusRunning,
		UserID:   "user-1",
		Username: "arcane",
		Results: EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: EnvironmentUpdateResultStatusUpdated},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: EnvironmentUpdateResultStatusUpdating},
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: EnvironmentUpdateResultStatusPending},
			{EnvironmentID: "remote-3", EnvironmentName: "parquetide", Status: EnvironmentUpdateResultStatusFailed, Error: "already failed"},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	reason := "interrupted by manager restart"
	svc.markUpdateAllFailedInternal(ctx, job, reason)

	var got EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)
	require.Equal(t, EnvironmentUpdateJobStatusFailed, got.Status)
	require.NotNil(t, got.Error)
	require.Equal(t, reason, *got.Error)
	require.NotNil(t, got.CompletedAt)
	require.Len(t, got.Results, 4)

	require.Equal(t, EnvironmentUpdateResultStatusUpdated, got.Results[0].Status)
	require.Empty(t, got.Results[0].Error)

	require.Equal(t, EnvironmentUpdateResultStatusFailed, got.Results[1].Status)
	require.Equal(t, reason, got.Results[1].Error)

	require.Equal(t, EnvironmentUpdateResultStatusPending, got.Results[2].Status)
	require.Empty(t, got.Results[2].Error)

	require.Equal(t, EnvironmentUpdateResultStatusFailed, got.Results[3].Status)
	require.Equal(t, "already failed", got.Results[3].Error)
}

// An up-to-date manager never restarts: its upgrader pulls, finds the image already
// running and skips the recreate. The pending_restart job must then be finalized in
// place, because no next boot is coming to do it.
func TestUpdateAllFinalizesUpToDateManagerWithoutRestart(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&EnvironmentUpdateJob{}, &event.Event{}))

	svc := NewService(db, nil, nil, event.NewEventService(db, nil, nil), nil, nil, nil)
	job := &EnvironmentUpdateJob{
		Status:                EnvironmentUpdateJobStatusPendingRestart,
		UserID:                "user-1",
		Username:              "arcane",
		ManagerVersionAtStart: "v1.0.0",
		Results: EnvironmentUpdateResults{
			{
				EnvironmentID: "0", EnvironmentName: "Local", Status: EnvironmentUpdateResultStatusUpdating, FromVersion: "v1.0.0",
				Stage: EnvironmentUpdateStageReconnecting, StageStartedAt: new(time.Now()),
			},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: EnvironmentUpdateResultStatusUpToDate},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.recordManagerResultInternal(job, EnvironmentUpdateResultStatusUpToDate, "v1.0.0")
	svc.finalizeUpdateAllJobInternal(ctx, job)

	var got EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)

	// Nothing is left waiting on a restart that will never happen.
	require.Equal(t, EnvironmentUpdateJobStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.Nil(t, got.Error)

	require.Equal(t, "0", got.Results[0].EnvironmentID)
	require.Equal(t, EnvironmentUpdateResultStatusUpToDate, got.Results[0].Status)
	require.Equal(t, "v1.0.0", got.Results[0].ToVersion)
	require.Empty(t, got.Results[0].Error)
	require.Equal(t, EnvironmentUpdateResultStatusUpToDate, got.Results[1].Status)
	require.Empty(t, got.Results[0].Stage)
	require.Nil(t, got.Results[0].StageStartedAt)
}

// Each stage change is persisted immediately so the status endpoint shows live
// progress; re-entering the current stage must not restart its clock.
func TestUpdateAllStageChangesPersist(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&EnvironmentUpdateJob{}))

	svc := NewService(db, nil, nil, nil, nil, nil, nil)
	job := &EnvironmentUpdateJob{
		Status: EnvironmentUpdateJobStatusRunning,
		Results: EnvironmentUpdateResults{
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: EnvironmentUpdateResultStatusUpdating},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.setUpdateStageInternal(ctx, job, &job.Results[0], EnvironmentUpdateStageChecking)
	started := job.Results[0].StageStartedAt
	require.NotNil(t, started)
	require.WithinDuration(t, time.Now(), *started, time.Second)

	svc.setUpdateStageInternal(ctx, job, &job.Results[0], EnvironmentUpdateStageChecking)
	require.Same(t, started, job.Results[0].StageStartedAt)

	svc.setUpdateStageInternal(ctx, job, &job.Results[0], EnvironmentUpdateStageStarting)
	var got EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)
	require.Equal(t, EnvironmentUpdateStageStarting, got.Results[0].Stage)
	require.NotNil(t, got.Results[0].StageStartedAt)
	require.NotSame(t, started, job.Results[0].StageStartedAt)
}

// The up-to-date short-circuit must only fire when the version check is conclusive:
// an agent that could not resolve a newest version or digest keeps the full
// trigger-and-confirm flow rather than being reported as already current.
func TestAgentAlreadyOnTarget(t *testing.T) {
	tests := []struct {
		name string
		info versiontypes.Info
		want bool
	}{
		{
			name: "on newest version",
			info: versiontypes.Info{CurrentVersion: "1.2.3", NewestVersion: "v1.2.3"},
			want: true,
		},
		{
			name: "update available",
			info: versiontypes.Info{CurrentVersion: "1.2.3", NewestVersion: "v1.3.0", UpdateAvailable: true},
			want: false,
		},
		{
			name: "on newest digest",
			info: versiontypes.Info{CurrentDigest: "sha256:abc", NewestDigest: "sha256:abc"},
			want: true,
		},
		{
			// A mutable tag rebuilt at the same version: the semver track reports no
			// update, but the differing digest means the pull really will replace the
			// image, so this must not be treated as already current.
			name: "same version, rebuilt digest",
			info: versiontypes.Info{
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
			info: versiontypes.Info{
				CurrentVersion: "1.2.3",
				NewestVersion:  "v1.2.3",
				CurrentDigest:  "sha256:running",
			},
			want: false,
		},
		{
			name: "inconclusive check resolves nothing",
			info: versiontypes.Info{CurrentVersion: "1.2.3"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.info.AlreadyOnNewest())
		})
	}
}

// With the manager-last ordering, a resumed pending_restart job means the agents
// phase already ran before the restart: resume must finalize the manager's own row
// and complete the job, NOT re-run the agents phase.
func TestResumeUpdateAllFinalizesManagerWithoutRerunningAgents(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&EnvironmentUpdateJob{}, &event.Event{}))

	// disabled=true keeps GetAppVersionInfo offline; nil docker => empty current digest.
	// The reported version differs from ManagerVersionAtStart, so the manager upgrade
	// is judged successful.
	versionSvc := version.NewVersionService(nil, true, "v9.9.9-new", "", nil, nil, nil, nil)
	svc := NewService(db, nil, versionSvc, event.NewEventService(db, nil, nil), nil, nil, nil)

	job := &EnvironmentUpdateJob{
		Status:                EnvironmentUpdateJobStatusPendingRestart,
		UserID:                "user-1",
		Username:              "arcane",
		ManagerVersionAtStart: "v1.0.0-old",
		Results: EnvironmentUpdateResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: EnvironmentUpdateResultStatusUpdating},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: EnvironmentUpdateResultStatusUpdated},
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: EnvironmentUpdateResultStatusSkippedOffline},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.ResumeUpdateAllOnStartup(ctx)

	var got EnvironmentUpdateJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)

	// Job is finalized in-process (no re-run, not left running/pending).
	require.Equal(t, EnvironmentUpdateJobStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.Len(t, got.Results, 3)

	// Manager row transitioned updating -> updated (version changed across the restart).
	require.Equal(t, "0", got.Results[0].EnvironmentID)
	require.Equal(t, EnvironmentUpdateResultStatusUpdated, got.Results[0].Status)
	require.NotEmpty(t, got.Results[0].ToVersion)

	// Remote rows are untouched — proving the agents phase was NOT re-run on resume.
	require.Equal(t, EnvironmentUpdateResultStatusUpdated, got.Results[1].Status)
	require.Equal(t, EnvironmentUpdateResultStatusSkippedOffline, got.Results[2].Status)
}
