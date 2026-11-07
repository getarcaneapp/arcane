package auth

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kit.getarcane.app/cache"

	"getarcane.dev/arcane/v3/internal/user"
)

type (
	TokenCache struct {
		entries *cache.Cache[string, verifiedTokenEntry]
		gen     atomic.Uint64
		fillMu  sync.Mutex
	}

	verifiedTokenEntry struct {
		TokenExpiresAt   time.Time
		SessionExpiresAt time.Time
		SessionID        string
		User             user.User
	}
)

func NewTokenCache() *TokenCache {
	return &TokenCache{
		entries: &cache.Cache[string, verifiedTokenEntry]{
			Capacity:        4096,
			TTL:             60 * time.Second,
			JanitorInterval: 60 * time.Second,
		},
	}
}

func (c *TokenCache) lookup(tokenHash string) (*user.User, string, bool) {
	cached, ok, _ := c.entries.Get(tokenHash)
	if !ok {
		return nil, "", false
	}
	now := time.Now()
	if now.After(cached.TokenExpiresAt) || now.After(cached.SessionExpiresAt) {
		c.entries.Delete(tokenHash)
		return nil, "", false
	}
	return new(cached.User), cached.SessionID, true
}

func (c *TokenCache) store(gen uint64, tokenHash string, entry verifiedTokenEntry) {
	c.fillMu.Lock()
	defer c.fillMu.Unlock()
	if c.gen.Load() == gen {
		c.entries.Set(cache.Entry[string, verifiedTokenEntry]{
			Key:   tokenHash,
			Value: entry,
		})
	}
}

func (c *TokenCache) InvalidateUser(userID string) {
	if strings.TrimSpace(userID) == "" {
		return
	}
	c.invalidate(func(entry verifiedTokenEntry) bool { return entry.User.ID == userID })
}

func (c *TokenCache) invalidate(matches func(verifiedTokenEntry) bool) {
	c.fillMu.Lock()
	defer c.fillMu.Unlock()
	c.gen.Add(1)
	keys := make([]string, 0)
	c.entries.Range(func(key string, entry verifiedTokenEntry) bool {
		if matches(entry) {
			keys = append(keys, key)
		}
		return true
	})
	c.entries.Delete(keys...)
}

func (c *TokenCache) Stop() {
	c.entries.Close()
}
