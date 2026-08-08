package environment

import (
	"strings"
	"sync"
	"time"

	"github.com/samber/hot"
	"github.com/samber/mo"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
)

const edgeTokenCacheTTL = time.Minute

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
	mu   sync.RWMutex
	envs map[string]models.Environment
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

func newRemoteEnvSnapshotCacheInternal() *remoteEnvSnapshotCacheInternal {
	return &remoteEnvSnapshotCacheInternal{envs: make(map[string]models.Environment)}
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

func (c *edgeTokenCacheInternal) put(envID string, token string) {
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
func (c *edgeTokenCacheInternal) sync(envID string, token string) {
	if c == nil || envID == "" {
		return
	}

	c.invalidate(envID)

	if resolvedToken := strings.TrimSpace(token); resolvedToken != "" {
		c.put(envID, resolvedToken)
	}
}

func isActiveRemoteEnvironmentInternal(environment models.Environment) bool {
	return environment.ID != "" && environment.ID != "0" && environment.Enabled && !environment.Hidden
}

func (c *remoteEnvSnapshotCacheInternal) get(environmentID string) mo.Option[models.Environment] {
	if c == nil || environmentID == "" {
		return mo.None[models.Environment]()
	}

	c.mu.RLock()
	envRecord, ok := c.envs[environmentID]
	c.mu.RUnlock()
	if !ok || !isActiveRemoteEnvironmentInternal(envRecord) {
		return mo.None[models.Environment]()
	}
	return mo.Some(envRecord)
}

func (c *remoteEnvSnapshotCacheInternal) replace(environments []models.Environment) {
	if c == nil {
		return
	}

	next := make(map[string]models.Environment, len(environments))
	for _, envRecord := range environments {
		if isActiveRemoteEnvironmentInternal(envRecord) {
			next[envRecord.ID] = envRecord
		}
	}

	c.mu.Lock()
	c.envs = next
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCacheInternal) put(environment models.Environment) {
	if c == nil {
		return
	}

	if !isActiveRemoteEnvironmentInternal(environment) {
		c.remove(environment.ID)
		return
	}

	c.mu.Lock()
	c.envs[environment.ID] = environment
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCacheInternal) remove(environmentID string) {
	if c == nil || environmentID == "" {
		return
	}

	c.mu.Lock()
	delete(c.envs, environmentID)
	c.mu.Unlock()
}

func (c *remoteEnvSnapshotCacheInternal) update(environmentID string, update func(*models.Environment)) {
	if c == nil || environmentID == "" || update == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

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
func (s *EnvironmentService) GetActiveRemoteEnvironmentSnapshot(environmentID string) mo.Option[models.Environment] {
	if s == nil {
		return mo.None[models.Environment]()
	}
	return s.remoteEnvs.get(environmentID)
}
