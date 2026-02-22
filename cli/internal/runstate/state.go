package runstate

import (
	"sync"
	"time"
)

type State struct {
	EnvOverride    string
	OutputMode     string
	AssumeYes      bool
	NoColor        bool
	RequestTimeout time.Duration
}

var (
	mu    sync.RWMutex
	state State
)

func Set(next State) {
	mu.Lock()
	defer mu.Unlock()
	state = next
}

func Get() State {
	mu.RLock()
	defer mu.RUnlock()
	return state
}
