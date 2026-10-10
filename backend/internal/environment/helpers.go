package environment

import (
	"hash/maphash"
	"strings"
	"sync"

	"github.com/samber/hot"
	"github.com/samber/mo"
)

// edgeTokenCache maps agent tokens to environment IDs; the reverse index drops
// an environment's entry when its token rotates.
type edgeTokenCache struct {
	mu      sync.RWMutex
	byToken *hot.HotCache[string, string]
	byEnvID map[string]string
}

// remoteEnvSnapshotCache holds every enabled, visible, non-local environment
// in memory so hot paths avoid a DB round trip.
type remoteEnvSnapshotCache struct {
	mu       sync.RWMutex
	envs     map[string]Environment
	seeded   bool
	revision uint64
}

// environmentCacheKey scopes cached records to a generation so loads that
// started before a write are never served after it.
type environmentCacheKey struct {
	gen uint64
	id  string
}

// environmentCacheStripes bounds generation tracking: a write orphans only the
// cached records whose IDs hash to its stripe.
const environmentCacheStripes = 64

var environmentCacheSeed = maphash.MakeSeed()

// runtimeWatchers fans a coalesced wake-up out to environment liveness watchers.
type runtimeWatchers struct {
	mu    sync.Mutex
	chans map[int]chan struct{}
	seq   int
}

func (c *edgeTokenCache) environmentID(token string) mo.Option[string] {
	if c == nil || c.byToken == nil || token == "" {
		return mo.None[string]()
	}

	staleEnvironmentID, wasCached := c.byToken.Peek(token)
	environmentID, ok, _ := c.byToken.Get(token)
	if ok {
		return mo.Some(environmentID)
	}
	if wasCached {
		c.mu.Lock()
		if currentToken, indexed := c.byEnvID[staleEnvironmentID]; indexed && currentToken == token {
			delete(c.byEnvID, staleEnvironmentID)
		}
		c.mu.Unlock()
	}
	return mo.None[string]()
}

func (c *edgeTokenCache) put(envID, token string) {
	if c == nil || c.byToken == nil || envID == "" || token == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if previousToken, ok := c.byEnvID[envID]; ok && previousToken != token {
		c.byToken.Delete(previousToken)
	}

	c.byEnvID[envID] = token
	c.byToken.Set(token, envID)
}

func (c *edgeTokenCache) invalidate(envID string) {
	if c == nil || envID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if token, ok := c.byEnvID[envID]; ok {
		delete(c.byEnvID, envID)
		if c.byToken != nil {
			c.byToken.Delete(token)
		}
	}
}

// sync replaces an environment's cached token, dropping it when the new token is blank.
func (c *edgeTokenCache) sync(envID, token string) {
	if c == nil || envID == "" {
		return
	}

	c.invalidate(envID)

	if resolvedToken := strings.TrimSpace(token); resolvedToken != "" {
		c.put(envID, resolvedToken)
	}
}

func isActiveRemoteEnvironment(env Environment) bool {
	return env.ID != "" && env.ID != "0" && env.Enabled && !env.Hidden
}

func (c *remoteEnvSnapshotCache) get(environmentID string) mo.Option[Environment] {
	if c == nil || environmentID == "" {
		return mo.None[Environment]()
	}

	c.mu.RLock()
	envRecord, ok := c.envs[environmentID]
	c.mu.RUnlock()
	if !ok || !isActiveRemoteEnvironment(envRecord) {
		return mo.None[Environment]()
	}
	return mo.Some(envRecord)
}

func (c *remoteEnvSnapshotCache) put(env Environment) {
	if c == nil {
		return
	}

	if !isActiveRemoteEnvironment(env) {
		c.remove(env.ID)
		return
	}

	c.mu.Lock()
	c.envs[env.ID] = env
	c.revision++
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCache) remove(environmentID string) {
	if c == nil || environmentID == "" {
		return
	}

	c.mu.Lock()
	delete(c.envs, environmentID)
	c.revision++
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCache) update(environmentID string, update func(*Environment)) {
	if c == nil || environmentID == "" || update == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.revision++

	envRecord, ok := c.envs[environmentID]
	if !ok {
		return
	}
	update(&envRecord)
	if isActiveRemoteEnvironment(envRecord) {
		c.envs[environmentID] = envRecord
	} else {
		delete(c.envs, environmentID)
	}
}

// subscribe returns a never-closed channel that receives a coalesced wake-up on
// liveness changes, plus a function to release it.
func (w *runtimeWatchers) subscribe() (<-chan struct{}, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.chans == nil {
		w.chans = make(map[int]chan struct{})
	}

	w.seq++
	id := w.seq
	// Capacity 1: watchers read live state, so a second pending signal is redundant.
	ch := make(chan struct{}, 1)
	w.chans[id] = ch

	return ch, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		delete(w.chans, id)
	}
}

// notify wakes every watcher without blocking, so tunnel callbacks may call it.
func (w *runtimeWatchers) notify() {
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
