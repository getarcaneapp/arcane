package utils

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"
)

// SyncGate remembers the SHA-256 of the last payload delivered per scope and
// key so periodic re-syncs skip the network when nothing changed, and
// serializes deliveries for one key so overlapping pushes reach the target in
// the order they are remembered. Only the digest is retained. Expiry bounds
// how long a delivery is trusted (zero means forever) so a target rebuilt
// behind an unchanged status still converges. The zero value is ready to use.
type SyncGate struct {
	Expiry time.Duration

	mu     sync.Mutex
	locks  KeyedMutex
	sent   map[string]map[string]syncGateEntryInternal
	epochs map[string]uint64
}

type syncGateEntryInternal struct {
	digest [sha256.Size]byte
	sentAt time.Time
}

// Begin reports whether body is already delivered for scope and key. When it
// is not, the key stays locked until finish runs; finish(true) remembers the
// delivery unless the scope was forgotten in between. Callers must invoke
// finish exactly once. Waiting for an in-flight delivery ends with ctx.
func (g *SyncGate) Begin(ctx context.Context, scope, key string, body []byte) (unchanged bool, finish func(delivered bool), err error) {
	unlock, err := g.locks.LockContext(ctx, scope+"\x00"+key)
	if err != nil {
		return false, nil, err
	}
	g.mu.Lock()
	epoch := g.epochs[scope]
	entry, ok := g.sent[scope][key]
	unchanged = ok && entry.digest == sha256.Sum256(body) && (g.Expiry <= 0 || time.Since(entry.sentAt) < g.Expiry)
	g.mu.Unlock()
	if unchanged {
		unlock()
		return true, func(bool) {}, nil
	}
	return false, func(delivered bool) {
		defer unlock()
		if !delivered {
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.epochs[scope] != epoch {
			return
		}
		if g.sent == nil {
			g.sent = make(map[string]map[string]syncGateEntryInternal)
		}
		if g.sent[scope] == nil {
			g.sent[scope] = make(map[string]syncGateEntryInternal)
		}
		g.sent[scope][key] = syncGateEntryInternal{digest: sha256.Sum256(body), sentAt: time.Now()}
	}, nil
}

// Forget drops every remembered payload for scope so the next sync resends,
// and discards the outcome of deliveries still in flight for it.
func (g *SyncGate) Forget(scope string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sent, scope)
	if g.epochs == nil {
		g.epochs = make(map[string]uint64)
	}
	g.epochs[scope]++
}
