package environment

import (
	"hash/maphash"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/samber/hot"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

// edgeTokenCache maps agent tokens to environment IDs; the reverse index drops
// an environment's entry when its token rotates.
type edgeTokenCache struct {
	mu      sync.RWMutex
	byToken *hot.HotCache[string, string]
	byEnvID map[string]string
}

type accessTokenIndex struct {
	mu       sync.RWMutex
	byEnvID  map[string]string
	byHashed map[string]int
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

type remoteSyncQueue struct {
	mu      sync.Mutex
	running map[string]bool
	pending map[string]bool
}

// runtimeWatchers fans a coalesced wake-up out to environment liveness watchers.
type runtimeWatchers struct {
	mu    sync.Mutex
	chans map[int]chan struct{}
	seq   int
}

// sync replaces an environment's cached token, dropping it when the new token is blank.
func (c *edgeTokenCache) sync(envID, token string) {
	if envID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if previousToken, ok := c.byEnvID[envID]; ok {
		delete(c.byEnvID, envID)
		c.byToken.Delete(previousToken)
	}
	if resolvedToken := strings.TrimSpace(token); resolvedToken != "" {
		c.byEnvID[envID] = resolvedToken
		c.byToken.Set(resolvedToken, envID)
	}
}

func (i *accessTokenIndex) sync(envID, token string) {
	if envID == "" {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if previousHash, ok := i.byEnvID[envID]; ok {
		delete(i.byEnvID, envID)
		i.byHashed[previousHash]--
		if i.byHashed[previousHash] <= 0 {
			delete(i.byHashed, previousHash)
		}
	}
	if resolvedToken := strings.TrimSpace(token); resolvedToken != "" {
		hashed := kit.SHA256Hex(resolvedToken)
		i.byEnvID[envID] = hashed
		i.byHashed[hashed]++
	}
}

func isActiveRemoteEnvironment(env Environment) bool {
	return env.ID != "" && env.ID != "0" && env.Enabled && !env.Hidden
}

func (c *remoteEnvSnapshotCache) put(env Environment) {
	if env.ID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if isActiveRemoteEnvironment(env) {
		c.envs[env.ID] = env
	} else {
		delete(c.envs, env.ID)
	}
	c.revision++
}

// accessibleEnvironmentIDs returns nil when the caller may list every environment,
// otherwise the sorted IDs it holds at least one permission on.
func accessibleEnvironmentIDs(ps *authz.PermissionSet) []string {
	switch {
	case ps == nil:
		return []string{}
	case ps.Allows(authz.PermEnvironmentsList, ""):
		return nil
	default:
		// Never nil: callers treat nil as unrestricted, so a user without grants must get an empty list.
		ids := slices.AppendSeq(make([]string, 0, len(ps.PerEnv)), maps.Keys(ps.PerEnv))
		slices.Sort(ids)
		return ids
	}
}
