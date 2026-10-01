package francis

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/stretchr/testify/require"
)

func TestSQLiteHostRestartPersistence(t *testing.T) {
	databaseURL := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "actors.db"))
	testRestartPersistenceInternal(t, databaseURL)

	var output bytes.Buffer
	runtime := &Runtime{}
	logger := slog.New(&actorLogHandlerInternal{
		Handler: slog.NewTextHandler(&output, nil),
		names:   &runtime.actorNames,
	})
	bound := logger.With("id", "job-executor/durable-hash/run-1").WithGroup("alarm")
	runtime.NameActor("durable-hash", "image-polling@0")
	bound.WarnContext(t.Context(), "Lease was lost", "actorRef", "job-coordinator/durable-hash")
	require.Contains(t, output.String(), "id=job-executor/image-polling@0/run-1")
	require.Contains(t, output.String(), "alarm.actorRef=job-coordinator/image-polling@0")
	output.Reset()
	logger.InfoContext(t.Context(), "unknown", "id", "other/unknown/run-1", "hostId", "durable-hash", slog.Group("nested", "actorRef", "job-health-executor/durable-hash"))
	require.Contains(t, output.String(), "id=other/unknown/run-1")
	require.Contains(t, output.String(), "hostId=durable-hash")
	require.Contains(t, output.String(), "nested.actorRef=job-health-executor/image-polling@0")
}

func TestPostgresHostRestartPersistence(t *testing.T) {
	databaseURL := os.Getenv("ARCANE_TEST_POSTGRES_DSN")
	if databaseURL == "" {
		t.Skip("ARCANE_TEST_POSTGRES_DSN is not set")
	}
	testRestartPersistenceInternal(t, databaseURL)
}

func testRestartPersistenceInternal(t *testing.T, databaseURL string) {
	t.Helper()
	actorID := t.Name() + time.Now().Format("150405.000000000")
	port := freePortInternal(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	first, err := New(databaseURL, "test-encryption-key", "test-instance", port)
	require.NoError(t, err)
	service := first.Service()
	first.NameActor(actorID, "durable@0")
	require.NoError(t, first.RegisterActor("test", func(string, *actor.Service) actor.Actor { return struct{}{} }))
	require.NoError(t, first.Start(ctx, ctx, func(err error) { t.Errorf("unexpected host failure: %v", err) }))
	t.Cleanup(func() { require.NoError(t, first.Stop(context.WithoutCancel(ctx))) })
	require.Same(t, service, first.Service())
	state := struct {
		Time time.Time
		Name string
	}{Time: time.Now().UTC().Truncate(time.Millisecond), Name: "durable"}
	require.NoError(t, service.SetState(ctx, "test", actorID, state, nil))
	require.NoError(t, first.Stop(ctx))

	second, err := New(databaseURL, "test-encryption-key", "test-instance", port)
	require.NoError(t, err)
	second.NameActor(actorID, "durable@0")
	require.NoError(t, second.RegisterActor("test", func(string, *actor.Service) actor.Actor { return struct{}{} }))
	require.NoError(t, second.Start(ctx, ctx, nil))
	t.Cleanup(func() { require.NoError(t, second.Stop(context.WithoutCancel(ctx))) })
	var restored struct {
		Time time.Time
		Name string
	}
	require.NoError(t, second.Service().GetState(ctx, "test", actorID, &restored))
	require.Equal(t, state.Name, restored.Name)
	require.True(t, state.Time.Equal(restored.Time))
	require.NoError(t, second.Service().DeleteState(ctx, "test", actorID))
	require.Error(t, second.RegisterActor("late", nil))
}

func freePortInternal(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strings.TrimPrefix(listener.LocalAddr().String(), "127.0.0.1:")
	require.NoError(t, listener.Close())
	return port
}
