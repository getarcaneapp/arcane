package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	streamtypes "github.com/getarcaneapp/arcane/types/v2/stream"
	"github.com/stretchr/testify/require"
)

// The environments channel must report an environment the manager cannot reach.
//
// This is the property the whole live-status fix rests on, and the one neither
// of the other channels can provide: the dashboard channel only emits when it
// gets a snapshot back from an agent, so an environment that is down produces
// nothing. Without a pushed status an offline agent stays offline in the UI
// forever, which is exactly what happens after a fleet update.
func TestStreamHandlerEnvironmentsChannelEmitsUnreachableEnvironmentInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)

	// An edge environment with no tunnel registered: unreachable by construction.
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", "edge://remote-1", "token")
	require.NoError(t, db.Exec(`UPDATE environments SET is_edge = ? WHERE id = ?`, true, "remote-1").Error)

	handler := &StreamHandler{
		environment: &EnvironmentHandler{
			environmentService: services.NewEnvironmentService(db, nil, nil, nil, nil, nil),
		},
	}
	ps := authz.NewPermissionSet()
	ps.Sudo = true

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = pw.Close() }()
		handler.streamClientInternal(ctx, ps, &StreamClientInput{Channels: streamtypes.ChannelEnvironments}, pw, func() {})
	}()

	var seen *streamtypes.Event
	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		var envelope streamtypes.Event
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &envelope))
		if envelope.Environment == nil {
			continue
		}
		seen = &envelope
		cancel()
		break
	}

	go func() { _, _ = io.Copy(io.Discard, pr) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate after cancel")
	}

	require.NotNil(t, seen, "expected an environments snapshot for an unreachable environment")
	require.Equal(t, streamtypes.ChannelEnvironments, seen.Channel)

	var remote *string
	for _, env := range seen.Environment.Environments {
		if env.ID == "remote-1" {
			status := env.Status
			remote = &status
		}
	}
	require.NotNil(t, remote, "unreachable environment must still appear in the snapshot")
	require.Equal(t, "offline", *remote)
}
