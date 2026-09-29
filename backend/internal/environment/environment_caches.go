package environment

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/samber/hot"
	"github.com/samber/mo"
)

const (
	edgeTokenCacheTTL   = time.Minute
	environmentCacheTTL = 30 * time.Second
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
func newEnvironmentCacheInternal() *hot.HotCache[string, Environment] {
	return hot.NewHotCache[string, Environment](hot.LRU, 256).
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

func isActiveRemoteEnvironmentInternal(environment Environment) bool {
	return environment.ID != "" && environment.ID != "0" && environment.Enabled && !environment.Hidden
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

func (c *remoteEnvSnapshotCacheInternal) put(environment Environment) {
	if c == nil {
		return
	}

	if !isActiveRemoteEnvironmentInternal(environment) {
		c.remove(environment.ID)
		return
	}

	c.mu.Lock()
	c.envs[environment.ID] = environment
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

// GetActiveRemoteEnvironmentSnapshot returns the latest in-process snapshot for
// an enabled, visible, non-local remote environment.
func (s *EnvironmentService) GetActiveRemoteEnvironmentSnapshot(environmentID string) mo.Option[Environment] {
	if s == nil {
		return mo.None[Environment]()
	}
	return s.remoteEnvs.get(environmentID)
}

// ListActiveRemoteEnvironments returns every enabled, visible, non-local
// environment from memory, sorted by ID. The first call loads them from the
// database.
func (s *EnvironmentService) ListActiveRemoteEnvironments(ctx context.Context) ([]Environment, error) {
	s.remoteEnvs.mu.RLock()
	seeded := s.remoteEnvs.seeded
	environments := make([]Environment, 0, len(s.remoteEnvs.envs))
	for _, envRecord := range s.remoteEnvs.envs {
		environments = append(environments, envRecord)
	}
	s.remoteEnvs.mu.RUnlock()

	if !seeded {
		var err error
		if environments, err = s.ListRemoteEnvironments(ctx); err != nil {
			return nil, err
		}
	}

	slices.SortFunc(environments, func(a, b Environment) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return environments, nil
}

// GetEnvironmentByIDCached is GetEnvironmentByID behind a short TTL cache that
// also remembers unknown IDs. Status and heartbeat fields may be stale.
func (s *EnvironmentService) GetEnvironmentByIDCached(ctx context.Context, id string) (*Environment, error) {
	envRecord, found, err := s.environmentCache.GetWithLoaders(id, func(ids []string) (map[string]Environment, error) {
		found := make(map[string]Environment, len(ids))
		for _, environmentID := range ids {
			loaded, loadErr := s.GetEnvironmentByID(ctx, environmentID)
			if errors.Is(loadErr, ErrEnvironmentNotFound) {
				continue
			}
			if loadErr != nil {
				return nil, loadErr
			}
			found[environmentID] = *loaded
		}
		return found, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrEnvironmentNotFound
	}
	return &envRecord, nil
}

// invalidateEnvironmentCacheInternal drops a cached record after a CRUD write.
func (s *EnvironmentService) invalidateEnvironmentCacheInternal(id string) {
	if s == nil || s.environmentCache == nil || id == "" {
		return
	}
	s.environmentCache.Delete(id)
}
