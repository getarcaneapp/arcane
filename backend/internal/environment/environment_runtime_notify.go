package environment

// subscribe returns a channel that receives a coalesced wake-up whenever
// environment liveness may have changed, plus a function to release it. The
// channel is never closed; callers select on it alongside their own context.
func (w *runtimeWatchersInternal) subscribe() (<-chan struct{}, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.chans == nil {
		w.chans = make(map[int]chan struct{})
	}

	w.seq++
	id := w.seq
	// Capacity 1: a second signal arriving before the watcher wakes is
	// redundant, because the watcher reads live state rather than a queue.
	ch := make(chan struct{}, 1)
	w.chans[id] = ch

	return ch, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		delete(w.chans, id)
	}
}

// notify wakes every watcher. It never blocks, so it is safe to call from
// connection callbacks on the tunnel hot path.
func (w *runtimeWatchersInternal) notify() {
	if w == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, ch := range w.chans {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// SubscribeRuntimeChanges returns a channel that receives a coalesced wake-up
// whenever environment liveness may have changed, plus a function to release it.
func (s *EnvironmentService) SubscribeRuntimeChanges() (<-chan struct{}, func()) {
	return s.runtimeWatchers.subscribe()
}

// NotifyRuntimeStateChanged wakes every runtime watcher.
func (s *EnvironmentService) NotifyRuntimeStateChanged() {
	if s == nil {
		return
	}
	s.runtimeWatchers.notify()
}
