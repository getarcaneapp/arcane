// Package testing starts isolated Francis hosts for owning-logic tests.
package testing

import (
	"context"
	"net"
	"strings"
	stdtesting "testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	"github.com/italypaleale/francis/components/standalone"
	"github.com/italypaleale/francis/host/local"
)

// New constructs a host whose factories can be registered before Start.
// With no database URL it uses an isolated in-memory provider. Pass a temporary
// SQLite URL or the gated Postgres test URL to exercise durable persistence.
func New(t stdtesting.TB, databaseURLs ...string) *francis.Runtime {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strings.TrimPrefix(listener.LocalAddr().String(), "127.0.0.1:")
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	databaseURL := "file:actor-test-unused.db"
	options := []local.HostOption{local.WithShutdownGracePeriod(time.Second), local.WithAlarmsPollInterval(110 * time.Millisecond), local.WithAlarmsFetchAheadInterval(time.Second), local.WithAlarmsLeaseDuration(2 * time.Second)}
	if len(databaseURLs) > 0 {
		databaseURL = databaseURLs[0]
	} else {
		options = append(options, local.WithStandaloneMemoryProvider(standalone.StandaloneMemoryOptions{}))
	}
	runtime, err := francis.New(databaseURL, "arcane-test-actor-key-32-characters", "test", port, options...)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// Start starts a previously registered host, verifies its listener, and joins
// its shutdown during test cleanup.
func Start(t stdtesting.TB, runtime *francis.Runtime) {
	t.Helper()
	appCtx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
		defer stopCancel()
		if err := runtime.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	})
	startupCtx, startupCancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer startupCancel()
	if err := runtime.Start(startupCtx, appCtx, func(err error) { t.Error(err); cancel() }); err != nil {
		t.Fatal(err)
	}
}
