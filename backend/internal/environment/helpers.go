package environment

import (
	"hash/maphash"
	"strings"
	"sync"

	"github.com/samber/hot"

	"kit.getarcane.app"
)

type (
	// agentTokenCache maps agent tokens to environment IDs; the reverse index drops
	// an environment's entry when its token rotates.
	agentTokenCache struct {
		byToken *hot.HotCache[string, string]
		byEnvID map[string]string
		mu      sync.RWMutex
	}

	accessTokenIndex struct {
		byEnvID  map[string]string
		byHashed map[string]int
		mu       sync.RWMutex
	}

	// remoteEnvSnapshotCache holds every enabled, visible, non-local environment
	// in memory so hot paths avoid a DB round trip.
	remoteEnvSnapshotCache struct {
		envs     map[string]Environment
		revision uint64
		mu       sync.RWMutex
		seeded   bool
	}

	// environmentCacheKey scopes cached records to a generation so loads that
	// started before a write are never served after it.
	environmentCacheKey struct {
		id  string
		gen uint64
	}

	remoteSyncQueue struct {
		running map[string]bool
		pending map[string]bool
		mu      sync.Mutex
	}

	// runtimeWatchers fans a coalesced wake-up out to environment liveness watchers.
	runtimeWatchers struct {
		chans map[int]chan struct{}
		seq   int
		mu    sync.Mutex
	}
)

const (
	// environmentCacheStripes bounds generation tracking: a write orphans only the
	// cached records whose IDs hash to its stripe.
	environmentCacheStripes = 64
)

var environmentCacheSeed = maphash.MakeSeed()

// sync replaces an environment's cached token, dropping it when the new token is blank.
func (c *agentTokenCache) sync(envID, token string) {
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

func (c *remoteEnvSnapshotCache) put(env Environment) {
	if env.ID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if env.ID != "" && env.ID != "0" && env.Enabled && !env.Hidden {
		c.envs[env.ID] = env
	} else {
		delete(c.envs, env.ID)
	}
	c.revision++
}
