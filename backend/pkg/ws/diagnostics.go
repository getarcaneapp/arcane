package ws

import "sync/atomic"

var WorkerGoroutines atomic.Int64

func trackWorkerGoroutine() func() {
	WorkerGoroutines.Add(1)
	return func() { WorkerGoroutines.Add(-1) }
}
