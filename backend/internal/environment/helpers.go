package environment

import (
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/samber/hot"
	"github.com/samber/mo"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
)

// edgeTokenCacheInternal maps an edge agent token to its environment ID. The
// reverse index lets an environment's entry be dropped when its token rotates,
// which the TTL cache alone cannot do.
type edgeTokenCacheInternal struct {
	mu      sync.RWMutex
	byToken *hot.HotCache[string, string]
	byEnvID map[string]string
}

// remoteEnvSnapshotCacheInternal holds the latest in-process copy of every
// enabled, visible, non-local environment, so hot paths avoid a DB round trip.
type remoteEnvSnapshotCacheInternal struct {
	mu       sync.RWMutex
	envs     map[string]Environment
	seeded   bool
	revision uint64
}

// environmentCacheKeyInternal scopes cached records to a cache generation so
// loads that started before a write can never be served after it.
type environmentCacheKeyInternal struct {
	gen uint64
	id  string
}

// runtimeWatchersInternal fans a coalesced wake-up out to everyone watching for
// environment liveness changes.
type runtimeWatchersInternal struct {
	mu    sync.Mutex
	chans map[int]chan struct{}
	seq   int
}

func newEdgeTokenCacheInternal() *edgeTokenCacheInternal {
	return &edgeTokenCacheInternal{
		byToken: hot.NewHotCache[string, string](hot.LRU, 1024).
			WithTTL(edgeTokenCacheTTL).
			WithJanitor().
			Build(),
		byEnvID: make(map[string]string),
	}
}

// newEnvironmentCacheInternal caches environment records by ID, including
// misses, for hot paths that only read CRUD-managed fields.
func newEnvironmentCacheInternal() *hot.HotCache[environmentCacheKeyInternal, Environment] {
	return hot.NewHotCache[environmentCacheKeyInternal, Environment](hot.LRU, 256).
		WithTTL(environmentCacheTTL).
		WithMissingSharedCache().
		WithJanitor().
		Build()
}

func newRemoteEnvSnapshotCacheInternal() *remoteEnvSnapshotCacheInternal {
	return &remoteEnvSnapshotCacheInternal{envs: make(map[string]Environment)}
}

func (c *edgeTokenCacheInternal) environmentID(token string) mo.Option[string] {
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

func (c *edgeTokenCacheInternal) put(envID, token string) {
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

func (c *edgeTokenCacheInternal) invalidate(envID string) {
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

// sync replaces an environment's cached token, dropping the entry entirely when
// the new token is blank.
func (c *edgeTokenCacheInternal) sync(envID, token string) {
	if c == nil || envID == "" {
		return
	}

	c.invalidate(envID)

	if resolvedToken := strings.TrimSpace(token); resolvedToken != "" {
		c.put(envID, resolvedToken)
	}
}

func isActiveRemoteEnvironmentInternal(env Environment) bool {
	return env.ID != "" && env.ID != "0" && env.Enabled && !env.Hidden
}

func (c *remoteEnvSnapshotCacheInternal) get(environmentID string) mo.Option[Environment] {
	if c == nil || environmentID == "" {
		return mo.None[Environment]()
	}

	c.mu.RLock()
	envRecord, ok := c.envs[environmentID]
	c.mu.RUnlock()
	if !ok || !isActiveRemoteEnvironmentInternal(envRecord) {
		return mo.None[Environment]()
	}
	return mo.Some(envRecord)
}

func (c *remoteEnvSnapshotCacheInternal) put(env Environment) {
	if c == nil {
		return
	}

	if !isActiveRemoteEnvironmentInternal(env) {
		c.remove(env.ID)
		return
	}

	c.mu.Lock()
	c.envs[env.ID] = env
	c.revision++
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCacheInternal) remove(environmentID string) {
	if c == nil || environmentID == "" {
		return
	}

	c.mu.Lock()
	delete(c.envs, environmentID)
	c.revision++
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCacheInternal) update(environmentID string, update func(*Environment)) {
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
	if isActiveRemoteEnvironmentInternal(envRecord) {
		c.envs[environmentID] = envRecord
	} else {
		delete(c.envs, environmentID)
	}
}

// filterEnvironmentsByIDInternal returns only the environments whose ID is in
// allowedIDs. A non-nil but empty allowedIDs yields an empty slice. Used to
// restrict the runtime-filtered list path to a caller's accessible environments.
func filterEnvironmentsByIDInternal(items []environment.Environment, allowedIDs []string) []environment.Environment {
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	out := make([]environment.Environment, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func environmentTypeMatchesInternal(env environment.Environment, filterValue string) bool {
	return environmentTypeKeyInternal(env) == strings.ToLower(strings.TrimSpace(filterValue))
}

func environmentTypeKeyInternal(env environment.Environment) string {
	if !env.IsEdge {
		return "http"
	}
	transport := ""
	if env.Connected != nil && *env.Connected && env.EdgeTransport != nil {
		transport = *env.EdgeTransport
	} else if env.LastEdgeTransport != nil {
		// Disconnected or poll-only agents classify by the transport they
		// last used rather than collapsing into the generic edge bucket.
		transport = *env.LastEdgeTransport
	}
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case edge.EdgeTransportWebSocket:
		return "websocket"
	case edge.EdgeTransportGRPC:
		return "grpc"
	default:
		return "edge"
	}
}

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
