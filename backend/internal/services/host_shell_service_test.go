package services

import (
	"context"
	"testing"

	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/hostshell"
)

func setupHostShellTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.SettingVariable{},
		&models.Event{},
	))
	return &database.DB{DB: db}
}

func newHostShellTestService(t *testing.T) (*HostShellService, *SettingsService) {
	t.Helper()
	db := setupHostShellTestDB(t)
	settings, err := NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	events := NewEventService(db, nil, nil)
	// dockerService and containerService are nil: every test here only
	// exercises paths that return before touching Docker (Enabled/disabled
	// short-circuit, ValidateShell, and the slot bookkeeping), which is
	// exactly what StartInteractive's early-return ordering guarantees.
	return NewHostShellService(nil, nil, settings, events), settings
}

func TestHostShellService_Enabled_DefaultsFalse(t *testing.T) {
	svc, _ := newHostShellTestService(t)
	require.False(t, svc.Enabled(context.Background()))
}

func TestHostShellService_Enabled_ReflectsSetting(t *testing.T) {
	svc, settings := newHostShellTestService(t)
	ctx := context.Background()

	require.NoError(t, settings.UpdateSetting(ctx, "hostTerminalEnabled", "true"))
	require.True(t, svc.Enabled(ctx))

	require.NoError(t, settings.UpdateSetting(ctx, "hostTerminalEnabled", "false"))
	require.False(t, svc.Enabled(ctx))
}

func TestHostShellService_StartInteractive_DisabledByDefault(t *testing.T) {
	svc, _ := newHostShellTestService(t)

	sess, err := svc.StartInteractive(context.Background(), StartInteractiveRequest{Shell: "/bin/sh"})

	require.Nil(t, sess)
	require.ErrorIs(t, err, ErrHostShellDisabled)
}

func TestHostShellService_StartInteractive_InvalidShellNeverTouchesDocker(t *testing.T) {
	svc, settings := newHostShellTestService(t)
	ctx := context.Background()
	require.NoError(t, settings.UpdateSetting(ctx, "hostTerminalEnabled", "true"))

	// dockerService is nil on this service; reaching a GetClient call would
	// nil-panic. ValidateShell must reject "bash" (not absolute) before
	// StartInteractive gets anywhere near Docker.
	sess, err := svc.StartInteractive(ctx, StartInteractiveRequest{Shell: "bash"})

	require.Nil(t, sess)
	require.ErrorIs(t, err, hostshell.ErrInvalidShell)
}

func TestHostShellService_SlotAccounting(t *testing.T) {
	svc, _ := newHostShellTestService(t)

	for i := 0; i < hostShellMaxConcurrentSessions; i++ {
		require.NoError(t, svc.acquireSlotInternal(), "slot %d", i)
	}

	err := svc.acquireSlotInternal()
	require.ErrorIs(t, err, ErrHostShellSessionLimit)

	svc.releaseSlotInternal()
	require.NoError(t, svc.acquireSlotInternal())
}

func TestHostShellService_ReleaseSlot_NeverGoesNegative(t *testing.T) {
	svc, _ := newHostShellTestService(t)

	svc.releaseSlotInternal()
	svc.releaseSlotInternal()

	for i := 0; i < hostShellMaxConcurrentSessions; i++ {
		require.NoError(t, svc.acquireSlotInternal())
	}
	require.ErrorIs(t, svc.acquireSlotInternal(), ErrHostShellSessionLimit)
}

func TestHostShellService_CleanupAll_NoSessionsIsNoop(t *testing.T) {
	svc, _ := newHostShellTestService(t)
	svc.CleanupAll(context.Background())
}
