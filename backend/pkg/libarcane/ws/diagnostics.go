package ws

import "sync/atomic"

// workerGoroutines tracks how many websocket worker goroutines this package is
// currently running (hub loops, read/write pumps and log forwarders).
//
// This used to be derived by scanning a full runtime.Stack(buf, true) dump on a
// 5s timer whenever a diagnostics client was connected — a stop-the-world pause
// of up to 8MB plus a string split, just to produce one integer. It also matched
// frames against a package path that no longer exists, so the reported number
// had been zero regardless. Workers now account for themselves.
var workerGoroutines atomic.Int64

// trackWorkerGoroutine marks the calling goroutine as an active worker and
// returns the function that releases it. Intended to be deferred immediately:
//
//	defer trackWorkerGoroutine()()
func trackWorkerGoroutine() func() {
	workerGoroutines.Add(1)
	return func() { workerGoroutines.Add(-1) }
}

// CountWorkerGoroutines returns the number of websocket worker goroutines
// belonging to this package. Intended for diagnostics endpoints only.
func CountWorkerGoroutines() int {
	return int(workerGoroutines.Load())
}
