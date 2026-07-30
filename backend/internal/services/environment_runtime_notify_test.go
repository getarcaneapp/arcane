package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A watcher must see a change signal, and a burst must not block the notifier —
// it runs on the edge tunnel's connect/disconnect path.
func TestEnvironmentServiceRuntimeChangeSignalCoalescesInternal(t *testing.T) {
	s := &EnvironmentService{}

	changes, unsubscribe := s.SubscribeRuntimeChanges()
	defer unsubscribe()

	// More notifications than the channel can hold: the extras are dropped
	// rather than blocking, because a watcher re-derives live state on wake.
	for range 5 {
		s.NotifyRuntimeStateChanged()
	}

	select {
	case <-changes:
	default:
		t.Fatal("expected a pending runtime change signal")
	}

	select {
	case <-changes:
		t.Fatal("expected coalesced signals, got a second wake-up")
	default:
	}

	// Unsubscribing must stop delivery so a closed stream cannot leak.
	unsubscribe()
	s.NotifyRuntimeStateChanged()
	select {
	case <-changes:
		t.Fatal("expected no signal after unsubscribe")
	default:
	}

	require.Empty(t, s.runtimeWatchers)
}
