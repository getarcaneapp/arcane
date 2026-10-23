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

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/version"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow/flowtest"
)

var testDockerService = (&dockerInternal.DockerClientService{}).WithClient(func() *client.Client {
	cli, _ := client.New(client.WithHost("tcp://127.0.0.1:1"), client.WithAPIVersion("1.41"))
	return cli
}())

// TestService_UpgradeFlag tests the upgrading flag behavior
func TestService_UpgradeFlag(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

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
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

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
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

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
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

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
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

	// Verify service is created and initialized properly
	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
}

// TestService_ConcurrentUpgradeAttempts tests that concurrent upgrade attempts are prevented
func TestService_ConcurrentUpgradeAttempts(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

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
	s := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)

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

// TestUpgradeJobAgentFailureStatus verifies that a reachable-but-failed agent (e.g.
// a poll-mode environment whose tunnel round-trip timed out) is reported as a real
// failure rather than mislabeled "offline — skipped".
func TestUpgradeJobAgentFailureStatus(t *testing.T) {
	// Mirrors the real wrapping: executeRemoteRequest -> TransportError ->
	// "tunnel request failed" -> context.DeadlineExceeded.
	tunnelTimeout := fmt.Errorf("failed to send request to environment oracle-cloud: %w",
		&remenv.TransportError{Err: fmt.Errorf("tunnel request failed: %w", context.DeadlineExceeded)})

	tests := []struct {
		name string
		err  error
		want UpgradeResultStatus
	}{
		{"tunnel request timed out", tunnelTimeout, UpgradeResultStatusFailed},
		{"request canceled mid-flight", fmt.Errorf("tunnel request failed: %w", context.Canceled), UpgradeResultStatusFailed},
		{"non-success status from agent", &remenv.StatusError{StatusCode: 502}, UpgradeResultStatusFailed},
		{"agent never connected", &remenv.TransportError{Err: errors.New("edge agent is not connected")}, UpgradeResultStatusSkippedOffline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, upgradeJobAgentFailureStatus(tt.err))
		})
	}
}

// A blank-target self-upgrade resolves its image from the version check (#3687).
// Explicit targets from the updater engine never reach this function.
func TestResolveSelfUpgradeTargetImage(t *testing.T) {
	tests := []struct {
		name         string
		currentImage string
		info         *types.VersionInfo
		want         string
		wantErr      string
	}{
		{
			name:         "agent pinned to an older release moves to the newest",
			currentImage: "ghcr.io/getarcaneapp/arcane-agent:v2.8.0",
			info:         &types.VersionInfo{NewestVersion: "v2.9.0"},
			want:         "ghcr.io/getarcaneapp/arcane-agent:v2.9.0",
		},
		{
			name:         "already-current exact version keeps its reference",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.9.0",
			info:         &types.VersionInfo{NewestVersion: "v2.9.0"},
			want:         "ghcr.io/getarcaneapp/arcane:v2.9.0",
		},
		{
			name:         "older newest release refuses a downgrade",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.9.0",
			info:         &types.VersionInfo{NewestVersion: "v2.8.0"},
			wantErr:      "downgrade",
		},
		{
			name:         "unresolved newest release aborts an exact-version upgrade",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.8.0",
			info:         &types.VersionInfo{},
			wantErr:      "could not be resolved",
		},
		{
			name:         "latest channel keeps its reference",
			currentImage: "ghcr.io/getarcaneapp/arcane:latest",
			info:         &types.VersionInfo{NewestVersion: "v2.9.0"},
			want:         "ghcr.io/getarcaneapp/arcane:latest",
		},
		{
			name:         "minor channel keeps its reference",
			currentImage: "ghcr.io/getarcaneapp/arcane:v2.9",
			info:         &types.VersionInfo{NewestVersion: "v2.9.1"},
			want:         "ghcr.io/getarcaneapp/arcane:v2.9",
		},
		{
			name:         "digest pin moves to the resolved newest digest",
			currentImage: "ghcr.io/getarcaneapp/arcane@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			info:         &types.VersionInfo{NewestDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			want:         "ghcr.io/getarcaneapp/arcane@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name:         "digest pin without a resolved newest digest fails",
			currentImage: "ghcr.io/getarcaneapp/arcane@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			info:         &types.VersionInfo{},
			wantErr:      "digest-pinned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSelfUpgradeTargetImage(tt.currentImage, tt.info)
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
			t.Setenv("ARCANE_PROJECTS_DIRECTORY", directory)
			settingsEnv, err := settings.LoadEnv()
			require.NoError(t, err)
			settingsSvc, err := settings.NewSettingsService(t.Context(), db, &config.Config{}, settingsEnv)
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
			svc := NewService(db, nil, nil, nil, nil, project.NewProjectService(db, settingsSvc, nil, nil, testDockerService, nil, nil, nil, nil, nil, nil), nil, nil, false)
			current := container.InspectResponse{Config: &container.Config{
				Image:  tt.runtime,
				Labels: map[string]string{"com.docker.compose.project": "arcane", "com.docker.compose.service": "server"},
			}}

			got, saveCompose, err := svc.configuredTargetImage(t.Context(), current, tt.target)
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

func TestUpgradeJobResolveResumeAction(t *testing.T) {
	now := time.Now()

	newJob := func(createdAt time.Time, versionAtStart, digestAtStart string) *UpgradeJob {
		job := &UpgradeJob{
			ManagerVersionAtStart: versionAtStart,
			ManagerDigestAtStart:  digestAtStart,
		}
		job.CreatedAt = createdAt
		return job
	}

	tests := []struct {
		name           string
		job            *UpgradeJob
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
			job: func() *UpgradeJob {
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
			got := resolveResumeAction(tt.job, tt.currentVersion, tt.currentDigest, now)

			require.Equal(t, tt.wantStale, got.markStale,
				"markStale = %v, want %v", got.markStale, tt.wantStale)

			require.False(t, !tt.wantStale && got.managerSucceeded != tt.wantManagerOK,
				"managerSucceeded = %v, want %v", got.managerSucceeded, tt.wantManagerOK)
		})
	}
}

func TestUpsertPendingResult(t *testing.T) {
	job := &UpgradeJob{
		Results: UpgradeJobResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: UpgradeResultStatusUpdated},
			{EnvironmentID: "abc", EnvironmentName: "palladium", Status: UpgradeResultStatusPending},
		},
	}
	{

		// A seeded environment resolves to its existing row without appending.
		idx := upsertPendingResult(job, "abc", "palladium")
		require.Equal(t, 1, idx,
			"existing env index = %d, want 1", idx)
	}

	require.Len(t, job.Results, 2,
		"results grew to %d, want 2", len(job.Results))

	// A missing environment (seeding raced or a new env was registered) appends a
	// fresh pending row and returns the new index.
	idx := upsertPendingResult(job, "xyz", "oracle-cloud")

	require.Equal(t, 2, idx,
		"new env index = %d, want 2", idx)

	require.Len(t, job.Results, 3,
		"results = %d, want 3", len(job.Results))

	got := job.Results[2]

	require.False(t, got.EnvironmentID != "xyz" || got.EnvironmentName != "oracle-cloud",
		"appended row = %+v, want id=xyz name=oracle-cloud", got)

	require.Equal(t, UpgradeResultStatusPending, got.Status,
		"appended row status = %q, want pending", got.Status)
}

func TestUpgradeJobFailedJobMarksUnfinishedResultsFailed(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&UpgradeJob{}, &event.Event{}))

	svc := NewService(db, nil, nil, event.NewEventService(db, nil, nil), nil, nil, nil, nil, false)
	job := &UpgradeJob{
		Status:   UpgradeJobStatusRunning,
		UserID:   "user-1",
		Username: "arcane",
		Results: UpgradeJobResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: UpgradeResultStatusUpdated},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: UpgradeResultStatusUpdating},
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: UpgradeResultStatusPending},
			{EnvironmentID: "remote-3", EnvironmentName: "parquetide", Status: UpgradeResultStatusFailed, Error: "already failed"},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	reason := "interrupted by manager restart"
	svc.markUpgradeJobFailed(ctx, job, reason)

	var got UpgradeJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)
	require.Equal(t, UpgradeJobStatusFailed, got.Status)
	require.NotNil(t, got.Error)
	require.Equal(t, reason, *got.Error)
	require.NotNil(t, got.CompletedAt)
	require.Len(t, got.Results, 4)

	require.Equal(t, UpgradeResultStatusUpdated, got.Results[0].Status)
	require.Empty(t, got.Results[0].Error)

	require.Equal(t, UpgradeResultStatusFailed, got.Results[1].Status)
	require.Equal(t, reason, got.Results[1].Error)

	require.Equal(t, UpgradeResultStatusFailed, got.Results[2].Status)
	require.Equal(t, reason, got.Results[2].Error)

	require.Equal(t, UpgradeResultStatusFailed, got.Results[3].Status)
	require.Equal(t, "already failed", got.Results[3].Error)
}

// An up-to-date manager never restarts: its upgrader pulls, finds the image already
// running and skips the recreate. The pending_restart job must then be finalized in
// place, because no next boot is coming to do it.
func TestUpgradeJobFinalizesUpToDateManagerWithoutRestart(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&UpgradeJob{}, &event.Event{}))

	svc := NewService(db, nil, nil, event.NewEventService(db, nil, nil), nil, nil, nil, nil, false)
	job := &UpgradeJob{
		Status:                UpgradeJobStatusPendingRestart,
		UserID:                "user-1",
		Username:              "arcane",
		ManagerVersionAtStart: "v1.0.0",
		Results: UpgradeJobResults{
			{
				EnvironmentID: "0", EnvironmentName: "Local", Status: UpgradeResultStatusUpdating, FromVersion: "v1.0.0",
				Stage: UpgradeStageReconnecting, StageStartedAt: new(time.Now()),
			},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: UpgradeResultStatusUpToDate},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.finalizeUpgradeJob(ctx, job, UpgradeResultStatusUpToDate, "v1.0.0")

	var got UpgradeJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)

	// Nothing is left waiting on a restart that will never happen.
	require.Equal(t, UpgradeJobStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.Nil(t, got.Error)

	require.Equal(t, "0", got.Results[0].EnvironmentID)
	require.Equal(t, UpgradeResultStatusUpToDate, got.Results[0].Status)
	require.Equal(t, "v1.0.0", got.Results[0].ToVersion)
	require.Empty(t, got.Results[0].Error)
	require.Equal(t, UpgradeResultStatusUpToDate, got.Results[1].Status)
	require.Empty(t, got.Results[0].Stage)
	require.Nil(t, got.Results[0].StageStartedAt)
}

// Each stage change is persisted immediately so the status endpoint shows live
// progress; re-entering the current stage must not restart its clock.
func TestUpgradeJobStageChangesPersist(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&UpgradeJob{}))

	svc := NewService(db, nil, nil, nil, nil, nil, nil, nil, false)
	job := &UpgradeJob{
		Status: UpgradeJobStatusRunning,
		Results: UpgradeJobResults{
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: UpgradeResultStatusUpdating},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.setUpdateStage(ctx, job, 0, UpgradeStageChecking)
	started := job.Results[0].StageStartedAt
	require.NotNil(t, started)
	require.WithinDuration(t, time.Now(), *started, time.Second)

	svc.setUpdateStage(ctx, job, 0, UpgradeStageChecking)
	require.Same(t, started, job.Results[0].StageStartedAt)

	svc.setUpdateStage(ctx, job, 0, UpgradeStageStarting)
	var got UpgradeJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)
	require.Equal(t, UpgradeStageStarting, got.Results[0].Stage)
	require.NotNil(t, got.Results[0].StageStartedAt)
	require.NotSame(t, started, job.Results[0].StageStartedAt)
}

// The up-to-date short-circuit must only fire when the version check is conclusive:
// an agent that could not resolve a newest version or digest keeps the full
// trigger-and-confirm flow rather than being reported as already current.
func TestAgentAlreadyOnTarget(t *testing.T) {
	tests := []struct {
		name string
		info types.VersionInfo
		want bool
	}{
		{
			name: "on newest version",
			info: types.VersionInfo{CurrentVersion: "1.2.3", NewestVersion: "v1.2.3"},
			want: true,
		},
		{
			name: "update available",
			info: types.VersionInfo{CurrentVersion: "1.2.3", NewestVersion: "v1.3.0", UpdateAvailable: true},
			want: false,
		},
		{
			name: "on newest digest",
			info: types.VersionInfo{CurrentDigest: "sha256:abc", NewestDigest: "sha256:abc"},
			want: true,
		},
		{
			// A mutable tag rebuilt at the same version: the semver track reports no
			// update, but the differing digest means the pull really will replace the
			// image, so this must not be treated as already current.
			name: "same version, rebuilt digest",
			info: types.VersionInfo{
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
			info: types.VersionInfo{
				CurrentVersion: "1.2.3",
				NewestVersion:  "v1.2.3",
				CurrentDigest:  "sha256:running",
			},
			want: false,
		},
		{
			name: "inconclusive check resolves nothing",
			info: types.VersionInfo{CurrentVersion: "1.2.3"},
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
func TestResumeUpgradeJobFinalizesManagerWithoutRerunningAgents(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&UpgradeJob{}, &event.Event{}, &settings.SettingVariable{}))
	versionSettings, err := settings.NewSettingsService(ctx, db, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, versionSettings.Stop(context.WithoutCancel(t.Context()))) })
	require.NoError(t, versionSettings.SetBoolSetting(ctx, "updateCheckEnabled", false))

	// disabled=true keeps GetAppVersionInfo offline; nil docker => empty current digest.
	// The reported version differs from ManagerVersionAtStart, so the manager upgrade
	// is judged successful.
	versionSvc := version.NewVersionService(nil, "v9.9.9-new", "", nil, nil, nil, versionSettings)
	svc := NewService(db, nil, versionSvc, event.NewEventService(db, nil, nil), nil, nil, nil, nil, false)
	harness := flowtest.New(t, nil)
	require.NoError(t, svc.RegisterWorkflows(harness.Engine, nil, nil))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)

	job := &UpgradeJob{
		Status:                UpgradeJobStatusPendingRestart,
		UserID:                "user-1",
		Username:              "arcane",
		ManagerVersionAtStart: "v1.0.0-old",
		Results: UpgradeJobResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: UpgradeResultStatusUpdating},
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: UpgradeResultStatusUpdated},
			{EnvironmentID: "remote-2", EnvironmentName: "oracle-cloud", Status: UpgradeResultStatusSkippedOffline},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	svc.ResumeUpgradeJobOnStartup(ctx)

	var got UpgradeJob
	require.NoError(t, db.WithContext(ctx).First(&got, "id = ?", job.ID).Error)

	// Job is finalized in-process (no re-run, not left running/pending).
	require.Equal(t, UpgradeJobStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.Len(t, got.Results, 3)

	// Manager row transitioned updating -> updated (version changed across the restart).
	require.Equal(t, "0", got.Results[0].EnvironmentID)
	require.Equal(t, UpgradeResultStatusUpdated, got.Results[0].Status)
	require.NotEmpty(t, got.Results[0].ToVersion)

	// Remote rows are untouched — proving the agents phase was NOT re-run on resume.
	require.Equal(t, UpgradeResultStatusUpdated, got.Results[1].Status)
	require.Equal(t, UpgradeResultStatusSkippedOffline, got.Results[2].Status)
}

func TestSeedUpgradeResults(t *testing.T) {
	manager := UpgradeJobResult{EnvironmentID: "0", EnvironmentName: "Local", Status: UpgradeResultStatusPending}
	remotes := []remoteAgent{{ID: "a", Name: "palladium"}, {ID: "b", Name: "oracle-cloud"}}
	ids := func(results UpgradeJobResults) []string {
		out := make([]string, len(results))
		for i, r := range results {
			out[i] = r.EnvironmentID
		}
		return out
	}

	tests := []struct {
		name      string
		requested []string
		want      []string
		wantErr   bool
	}{
		{name: "empty selects every environment", requested: nil, want: []string{"0", "a", "b"}},
		{name: "single remote", requested: []string{"b"}, want: []string{"b"}},
		{name: "manager only", requested: []string{"0"}, want: []string{"0"}},
		{name: "unknown environment", requested: []string{"a", "missing"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := seedUpgradeResults(tt.requested, manager, remotes)
			if tt.wantErr {
				require.ErrorIs(t, err, common.ErrValidation)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, ids(got))
			require.Equal(t, UpgradeResultStatusPending, got[len(got)-1].Status)
		})
	}
}

func TestStreamProgressServesRetainedRunBeforeLogReplay(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, false)
	svc.progress.start("v1.0.0", "v1.1.0")
	svc.progress.emit(types.UpgradeProgressEventLog, 10, "Pulling fs layer")
	svc.progress.finish("pull upgrader image: denied")

	var got []types.UpgradeProgressEvent
	svc.StreamProgress(t.Context(), func(event types.UpgradeProgressEvent) bool {
		got = append(got, event)
		return true
	})
	require.Len(t, got, 2)
	require.Equal(t, "Pulling fs layer", got[0].Message)
	require.Equal(t, types.UpgradeProgressEventDone, got[1].Type)
	require.Equal(t, "pull upgrader image: denied", got[1].Message)
}

func TestFollowUpgraderKeepsFeedActiveWhenLogsFail(t *testing.T) {
	svc := NewService(nil, testDockerService, nil, nil, nil, nil, nil, nil, false)
	svc.progress.start("v1.0.0", "v1.1.0")
	_, live, _, unsubscribe := svc.progress.subscribe()
	defer unsubscribe()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go svc.followUpgrader(ctx, "missing-upgrader")

	select {
	case update := <-live:
		require.Equal(t, types.UpgradeProgressEventLog, update.Type)
		require.Contains(t, update.Message, "logs are unavailable")
	case <-time.After(5 * time.Second):
		t.Fatal("expected a logs-unavailable event")
	}
	select {
	case update := <-live:
		t.Fatalf("feed must stay active until the upgrader exit is settled, got %+v", update)
	case <-time.After(300 * time.Millisecond):
	}

	cancel()
	select {
	case update := <-live:
		require.Equal(t, types.UpgradeProgressEventDone, update.Type)
		require.NotEmpty(t, update.Message)
	case <-time.After(5 * time.Second):
		t.Fatal("expected the feed to finish once the follow ended")
	}
}

func TestProgressSinkReplaysWithoutDuplicatingLogs(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&UpgradeJob{}))

	svc := NewService(db, nil, nil, nil, nil, nil, nil, nil, false)
	job := &UpgradeJob{
		Status: UpgradeJobStatusRunning,
		Results: UpgradeJobResults{
			{EnvironmentID: "remote-1", EnvironmentName: "palladium", Status: UpgradeResultStatusUpdating, Logs: []string{"checked"}},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	logEvent := func(message string, replay bool) types.UpgradeProgressEvent {
		return types.UpgradeProgressEvent{
			Type:    types.UpgradeProgressEventLog,
			Message: message,
			Replay:  replay,
		}
	}
	sink := &progressSink{service: svc, job: job, index: 0}
	sink.apply(ctx, logEvent("pulling", true))
	sink.apply(ctx, logEvent("live", false))
	sink.replaying = false
	for _, message := range []string{"pulling", "live", "restarted"} {
		sink.apply(ctx, logEvent(message, true))
	}
	require.Equal(t, []string{"checked", "pulling", "live", "restarted"}, job.Results[0].Logs)

	replace := &progressSink{service: svc, job: job, index: 0, baseSet: true}
	replace.apply(ctx, logEvent("from file", true))
	require.Equal(t, []string{"from file"}, job.Results[0].Logs)
}

func TestManagerOnlyUpgradeJobReachesManagerStep(t *testing.T) {
	ctx := t.Context()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db := &database.DB{DB: gormDB}
	require.NoError(t, db.AutoMigrate(&UpgradeJob{}))

	svc := NewService(db, nil, nil, nil, nil, nil, nil, nil, false)
	harness := flowtest.New(t, nil)
	require.NoError(t, svc.RegisterWorkflows(harness.Engine, nil, nil))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)

	job := &UpgradeJob{
		Status: UpgradeJobStatusCompleted,
		Results: UpgradeJobResults{
			{EnvironmentID: "0", EnvironmentName: "Local", Status: UpgradeResultStatusUpdated},
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(job).Error)

	instanceID, err := svc.flow.Submit(ctx, svc.upgradeJobWorkflow, upgradeJobInput{JobID: job.ID}, activity.StartRequest{})
	require.NoError(t, err)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	outcome, err := svc.flow.Wait(waitCtx, svc.upgradeJobWorkflow, instanceID)
	require.NoError(t, err)
	require.Equal(t, types.RunStatusSucceeded, outcome.Status)
	require.Equal(t, "Upgrade job finished; completed", outcome.Message)
}
