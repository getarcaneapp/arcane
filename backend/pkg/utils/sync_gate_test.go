package utils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func deliver(t *testing.T, gate *SyncGate, scope, key string, body []byte) bool {
	t.Helper()
	unchanged, finish, err := gate.Begin(t.Context(), scope, key, body)
	require.NoError(t, err)
	finish(!unchanged)
	return unchanged
}

func TestSyncGate_TracksLastDeliveredPayloadPerScopeAndKey(t *testing.T) {
	var gate SyncGate
	body := []byte(`{"registries":[]}`)

	require.False(t, deliver(t, &gate, "env", "/registries", body), "nothing delivered yet")
	require.True(t, deliver(t, &gate, "env", "/registries", body))
	require.False(t, deliver(t, &gate, "env", "/registries", []byte(`{"registries":[{"id":"a"}]}`)))
	require.False(t, deliver(t, &gate, "env", "/s3", body), "keys are independent")
	require.False(t, deliver(t, &gate, "other", "/registries", body), "scopes are independent")

	gate.Forget("env")
	require.False(t, deliver(t, &gate, "env", "/registries", body), "forget drops every key in the scope")
	gate.Forget("missing")
}

func TestSyncGate_FailedDeliveryIsNotRemembered(t *testing.T) {
	var gate SyncGate
	body := []byte(`{"registries":[]}`)
	t.Helper()
	unchanged, finish, err := gate.Begin(t.Context(), "env", "/registries", body)
	require.NoError(t, err)
	require.False(t, unchanged)
	finish(false)
	require.False(t, deliver(t, &gate, "env", "/registries", body))
}

func TestSyncGate_ForgetDiscardsInFlightDelivery(t *testing.T) {
	var gate SyncGate
	body := []byte(`{"registries":[]}`)

	_, finish, err := gate.Begin(t.Context(), "env", "/registries", body)
	require.NoError(t, err)
	gate.Forget("env")
	finish(true)
	require.False(t, deliver(t, &gate, "env", "/registries", body), "a delivery that started before Forget must not count")
	require.True(t, deliver(t, &gate, "env", "/registries", body))
}

func TestSyncGate_SerializesDeliveriesPerKey(t *testing.T) {
	var gate SyncGate
	older, newer := []byte(`{"v":1}`), []byte(`{"v":2}`)
	_, finishOlder, err := gate.Begin(t.Context(), "env", "/registries", older)
	require.NoError(t, err)

	started := make(chan struct{})
	done := make(chan bool)
	go func() {
		close(started)
		t.Helper()
		unchanged, finish, beginErr := gate.Begin(t.Context(), "env", "/registries", newer)
		if beginErr != nil {
			t.Error(beginErr)
			done <- false
			return
		}
		finish(true)
		done <- unchanged
	}()
	<-started
	select {
	case <-done:
		t.Fatal("the second delivery must wait for the first to finish")
	case <-time.After(50 * time.Millisecond):
	}
	finishOlder(true)
	require.False(t, <-done)
	require.True(t, deliver(t, &gate, "env", "/registries", newer), "the last delivery wins")
}

func TestSyncGate_BeginStopsWaitingWhenContextEnds(t *testing.T) {
	var gate SyncGate
	body := []byte(`{"v":1}`)
	_, finish, err := gate.Begin(t.Context(), "env", "/registries", body)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, _, err = gate.Begin(ctx, "env", "/registries", body)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	finish(true)
	require.True(t, deliver(t, &gate, "env", "/registries", body), "the key is usable after an abandoned wait")
}

func TestSyncGate_ExpiryForcesPeriodicResend(t *testing.T) {
	gate := SyncGate{Expiry: time.Hour}
	body := []byte(`{"registries":[]}`)
	require.False(t, deliver(t, &gate, "env", "/registries", body))
	require.True(t, deliver(t, &gate, "env", "/registries", body))

	gate.sent["env"]["/registries"] = syncGateEntry{digest: gate.sent["env"]["/registries"].digest, sentAt: time.Now().Add(-2 * time.Hour)}
	require.False(t, deliver(t, &gate, "env", "/registries", body), "an old delivery is no longer trusted")
}
