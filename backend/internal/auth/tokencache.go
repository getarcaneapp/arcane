package auth

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/hot"

	"getarcane.dev/arcane/v3/internal/user"
)

type (
	TokenCache struct {
		entries *hot.HotCache[string, verifiedTokenEntry]
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
		entries: hot.NewHotCache[string, verifiedTokenEntry](hot.LRU, 4096).
			WithTTL(60 * time.Second).
			WithJanitor().
			Build(),
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
		c.entries.Set(tokenHash, entry)
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
	c.entries.DeleteMany(keys)
}

func (c *TokenCache) Stop() {
	c.entries.StopJanitor()
}
