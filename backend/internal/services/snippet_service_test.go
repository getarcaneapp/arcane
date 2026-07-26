package services

import (
	"context"
	"testing"
	"time"

	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	snippettypes "github.com/getarcaneapp/arcane/types/v2/snippet"
)

type snippetTestSchedulerInternal struct {
	added   []string
	removed []string
}

func (s *snippetTestSchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.added = append(s.added, job.Name())
	return nil
}

func (s *snippetTestSchedulerInternal) RemoveJob(_ context.Context, name string) {
	s.removed = append(s.removed, name)
}

func (s *snippetTestSchedulerInternal) HasJob(_ string) bool { return false }

func setupSnippetTestService(t *testing.T) (*SnippetService, *database.DB) {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&models.Snippet{}, &models.SnippetRun{}, &models.Environment{}, &models.Event{}, &models.SettingVariable{}))
	db := &database.DB{DB: gdb}

	require.NoError(t, gdb.Exec("INSERT INTO environments (id, created_at, name) VALUES ('0', ?, 'local')", time.Now()).Error)

	ctx := context.Background()
	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	hostShellService := NewHostShellService(nil, nil, settingsService, eventService)

	return NewSnippetService(db, eventService, hostShellService, nil), db
}

func TestSnippetService_CreateSnippet_RegistersScheduleJob(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)
	scheduler := &snippetTestSchedulerInternal{}
	svc.SetScheduler(ctx, scheduler)

	scheduleEnabled := true
	schedule := "*/5 * * * * *"
	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name:            "restart-web",
		Script:          "echo hi",
		Schedule:        &schedule,
		ScheduleEnabled: &scheduleEnabled,
	}, systemUser)
	require.NoError(t, err)

	require.Contains(t, scheduler.added, snippetJobNameInternal(snippet.ID))
}

func TestSnippetService_CreateSnippet_DefaultsToHostTarget(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "no-target", Script: "echo hi",
	}, systemUser)
	require.NoError(t, err)
	require.Equal(t, snippettypes.TargetHost, snippet.Target)
	require.Nil(t, snippet.ContainerID)
}

func TestSnippetService_CreateSnippet_ContainerTargetRequiresContainerID(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	target := snippettypes.TargetContainer
	_, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "no-container-id", Script: "echo hi", Target: &target,
	}, systemUser)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
}

func TestSnippetService_CreateSnippet_InvalidTargetRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	bogus := "vm"
	_, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "bad-target", Script: "echo hi", Target: &bogus,
	}, systemUser)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
}

func TestSnippetService_CreateSnippet_ContainerTargetPersists(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	target := snippettypes.TargetContainer
	containerID := "abc123"
	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "restart-in-container", Script: "echo hi", Target: &target, ContainerID: &containerID,
	}, systemUser)
	require.NoError(t, err)
	require.Equal(t, snippettypes.TargetContainer, snippet.Target)
	require.NotNil(t, snippet.ContainerID)
	require.Equal(t, containerID, *snippet.ContainerID)
}

func TestSnippetService_UpdateSnippet_SwitchingToHostClearsContainerID(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	target := snippettypes.TargetContainer
	containerID := "abc123"
	created, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "switching", Script: "echo hi", Target: &target, ContainerID: &containerID,
	}, systemUser)
	require.NoError(t, err)

	hostTarget := snippettypes.TargetHost
	updated, err := svc.UpdateSnippet(ctx, "0", created.ID, snippettypes.UpdateSnippetRequest{Target: &hostTarget}, systemUser)
	require.NoError(t, err)
	require.Equal(t, snippettypes.TargetHost, updated.Target)
	require.Nil(t, updated.ContainerID)
}

func TestSnippetService_UpdateSnippet_DisablingRemovesJob(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)
	scheduler := &snippetTestSchedulerInternal{}
	svc.SetScheduler(ctx, scheduler)

	scheduleEnabled := true
	schedule := "*/5 * * * * *"
	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name:            "restart-web",
		Script:          "echo hi",
		Schedule:        &schedule,
		ScheduleEnabled: &scheduleEnabled,
	}, systemUser)
	require.NoError(t, err)
	require.Contains(t, scheduler.added, snippetJobNameInternal(snippet.ID))

	disabled := false
	_, err = svc.UpdateSnippet(ctx, "0", snippet.ID, snippettypes.UpdateSnippetRequest{ScheduleEnabled: &disabled}, systemUser)
	require.NoError(t, err)

	require.Contains(t, scheduler.removed, snippetJobNameInternal(snippet.ID))
}

func TestSnippetService_DeleteSnippet_RemovesJob(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)
	scheduler := &snippetTestSchedulerInternal{}
	svc.SetScheduler(ctx, scheduler)

	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name:   "one-off",
		Script: "echo hi",
	}, systemUser)
	require.NoError(t, err)

	require.NoError(t, svc.DeleteSnippet(ctx, "0", snippet.ID, systemUser))
	require.Contains(t, scheduler.removed, snippetJobNameInternal(snippet.ID))
}

func TestSnippetService_RegisterScheduledSnippetsOnStartup_SkipsDisabled(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	scheduleEnabled := true
	scheduleDisabled := false
	schedule := "*/5 * * * * *"

	enabledSnippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "enabled", Script: "echo 1", Schedule: &schedule, ScheduleEnabled: &scheduleEnabled,
	}, systemUser)
	require.NoError(t, err)

	disabledSnippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "disabled", Script: "echo 2", Schedule: &schedule, ScheduleEnabled: &scheduleDisabled,
	}, systemUser)
	require.NoError(t, err)

	// RegisterScheduledSnippetsOnStartup is exercised on a fresh scheduler,
	// mirroring a process restart where SetScheduler is called again before
	// any prior registrations exist.
	scheduler := &snippetTestSchedulerInternal{}
	svc.SetScheduler(ctx, scheduler)
	svc.RegisterScheduledSnippetsOnStartup(ctx)

	require.Contains(t, scheduler.added, snippetJobNameInternal(enabledSnippet.ID))
	require.NotContains(t, scheduler.added, snippetJobNameInternal(disabledSnippet.ID))
}

func TestSnippetService_ScheduledFireOnDeletedRow_Unregisters(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)
	scheduler := &snippetTestSchedulerInternal{}
	svc.SetScheduler(ctx, scheduler)

	svc.runScheduledInternal(ctx, "0", "does-not-exist")

	require.Contains(t, scheduler.removed, snippetJobNameInternal("does-not-exist"))
}

func TestSnippetService_RunSnippet_HostShellDisabled_NoRunRowCreated(t *testing.T) {
	ctx := context.Background()
	svc, db := setupSnippetTestService(t)

	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "df", Script: "df -h",
	}, systemUser)
	require.NoError(t, err)

	_, err = svc.RunSnippet(ctx, "0", snippet.ID, nil, "manual", systemUser)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrUnavailable)

	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&models.SnippetRun{}).Where("snippet_id = ?", snippet.ID).Count(&count).Error)
	require.Zero(t, count, "no run row should be created when the host shell guard rejects the run")
}

func TestSnippetService_CreateThenUpdate_PersistsParametersAsJSON(t *testing.T) {
	ctx := context.Background()
	svc, _ := setupSnippetTestService(t)

	params := []snippettypes.ParameterDef{
		{Name: "CONTAINER", Type: snippettypes.ParameterTypeString, Required: true},
		{Name: "MODE", Type: snippettypes.ParameterTypeSelect, Options: []string{"fast", "slow"}, Default: "fast"},
	}
	created, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name:       "restart",
		Script:     "docker restart \"$CONTAINER\"",
		Parameters: params,
	}, systemUser)
	require.NoError(t, err)
	require.Equal(t, params, created.Parameters)

	fetched, err := svc.GetSnippetByID(ctx, "0", created.ID)
	require.NoError(t, err)
	require.Equal(t, params, fetched.Parameters)

	newParams := []snippettypes.ParameterDef{
		{Name: "TARGET", Type: snippettypes.ParameterTypeString},
	}
	updated, err := svc.UpdateSnippet(ctx, "0", created.ID, snippettypes.UpdateSnippetRequest{Parameters: newParams}, systemUser)
	require.NoError(t, err)
	require.Equal(t, newParams, updated.Parameters)

	refetched, err := svc.GetSnippetByID(ctx, "0", created.ID)
	require.NoError(t, err)
	require.Equal(t, newParams, refetched.Parameters)
}

func TestPruneSnippetRunsInternal_KeepsFixedCap(t *testing.T) {
	ctx := context.Background()
	svc, db := setupSnippetTestService(t)

	snippet, err := svc.CreateSnippet(ctx, "0", snippettypes.CreateSnippetRequest{
		Name: "chatty", Script: "echo hi",
	}, systemUser)
	require.NoError(t, err)

	base := time.Now()
	for i := 0; i < snippetRunHistoryLimit+5; i++ {
		run := &models.SnippetRun{
			SnippetID:     snippet.ID,
			EnvironmentID: "0",
			TriggerSource: "manual",
			Status:        "success",
			StartedAt:     base.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, svc.persistRunInternal(ctx, run))
	}

	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&models.SnippetRun{}).Where("snippet_id = ?", snippet.ID).Count(&count).Error)
	require.EqualValues(t, snippetRunHistoryLimit, count)

	var newest models.SnippetRun
	require.NoError(t, db.WithContext(ctx).Where("snippet_id = ?", snippet.ID).Order("started_at DESC").First(&newest).Error)
	require.WithinDuration(t, base.Add(time.Duration(snippetRunHistoryLimit+4)*time.Second), newest.StartedAt, time.Second)

	var oldestKept models.SnippetRun
	require.NoError(t, db.WithContext(ctx).Where("snippet_id = ?", snippet.ID).Order("started_at ASC").First(&oldestKept).Error)
	require.WithinDuration(t, base.Add(5*time.Second), oldestKept.StartedAt, time.Second)
}
