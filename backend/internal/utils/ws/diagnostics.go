package ws

import (
	"runtime"
	"strings"
	"sync"
	"time"
)

const wsPkgPath = "internal/utils/ws"

const workerGoroutineCountTTL = 5 * time.Second

var workerGoroutineCountCache struct {
	sync.Mutex
	value int
	at    time.Time
}

// CountWorkerGoroutines returns a best-effort count of websocket worker goroutines
// belonging to this package. Intended for diagnostics endpoints only.
func CountWorkerGoroutines() int {
	workerGoroutineCountCache.Lock()
	if !workerGoroutineCountCache.at.IsZero() && time.Since(workerGoroutineCountCache.at) < workerGoroutineCountTTL {
		value := workerGoroutineCountCache.value
		workerGoroutineCountCache.Unlock()
		return value
	}
	workerGoroutineCountCache.Unlock()

	count := countWorkerGoroutinesInternal()

	workerGoroutineCountCache.Lock()
	workerGoroutineCountCache.value = count
	workerGoroutineCountCache.at = time.Now()
	workerGoroutineCountCache.Unlock()

	return count
}

func countWorkerGoroutinesInternal() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		if len(buf) >= 8<<20 {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}

	s := string(buf)
	blocks := strings.Split(s, "\n\n")
	count := 0
	for _, block := range blocks {
		if block == "" || !strings.Contains(block, wsPkgPath) {
			continue
		}
		if strings.Contains(block, ".Run(") ||
			strings.Contains(block, "readPump") ||
			strings.Contains(block, "writePump") ||
			strings.Contains(block, "ForwardLines") ||
			strings.Contains(block, "ForwardLogJSON") ||
			strings.Contains(block, "ForwardLogJSONBatched") {
			count++
		}
	}

	return count
}
