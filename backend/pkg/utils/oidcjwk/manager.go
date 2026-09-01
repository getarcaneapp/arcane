package oidcjwk

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"emperror.dev/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jwx-go/jwkfetch/v4"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/samber/hot"
	"golang.org/x/sync/singleflight"
)

const (
	maxJWKSBodySize        = 1 << 20
	maxJWKSKeys            = 100
	maxHTTPClientPolicies  = 16
	initialReadyTimeout    = 30 * time.Second
	minimumRefreshInterval = 5 * time.Minute
	maximumRefreshInterval = time.Hour
)

var ErrManagerShutdown = errors.Sentinel("oidcjwk: key set manager is shut down")

type managedCache struct {
	cache   *jwkfetch.Cache
	keySets map[string]*keySet
}

type KeySetManager struct {
	ctx      context.Context
	mu       sync.Mutex
	caches   *hot.HotCache[*http.Client, *managedCache]
	group    singleflight.Group
	active   sync.WaitGroup
	shutdown atomic.Bool
}

func NewKeySetManager(ctx context.Context) *KeySetManager { //nolint:contextcheck // cache workers inherit the application lifecycle context, not request contexts.
	if ctx == nil {
		ctx = context.Background()
	}
	return &KeySetManager{
		ctx:    ctx,
		caches: hot.NewHotCache[*http.Client, *managedCache](hot.LRU, maxHTTPClientPolicies).Build(),
	}
}

func (m *KeySetManager) KeySet(ctx context.Context, client *http.Client, jwksURL string) (oidc.KeySet, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if jwksURL == "" {
		return nil, errors.New("oidcjwk: JWKS URL is empty")
	}
	if m.shutdown.Load() {
		return nil, ErrManagerShutdown
	}

	m.mu.Lock()
	if m.shutdown.Load() {
		m.mu.Unlock()
		return nil, ErrManagerShutdown
	}
	m.active.Add(1)
	if managed, found, _ := m.caches.Get(client); found {
		if keySet := managed.keySets[jwksURL]; keySet != nil {
			m.active.Done()
			m.mu.Unlock()
			return keySet, nil
		}
	}
	m.mu.Unlock()
	defer m.active.Done()

	groupKey := fmt.Sprintf("%p\x00%s", client, jwksURL)
	value, err, _ := m.group.Do(groupKey, func() (any, error) {
		m.mu.Lock()
		if m.shutdown.Load() {
			m.mu.Unlock()
			return nil, ErrManagerShutdown
		}
		managed, found, _ := m.caches.Get(client)
		if found {
			if keySet := managed.keySets[jwksURL]; keySet != nil {
				m.mu.Unlock()
				return keySet, nil
			}
		} else {
			var cacheErr error
			managed, cacheErr = m.createManagedCacheLockedInternal(client)
			if cacheErr != nil {
				m.mu.Unlock()
				return nil, cacheErr
			}
		}
		m.mu.Unlock()

		registerCtx, cancel := context.WithTimeout(ctx, initialReadyTimeout)
		defer cancel()
		if registerErr := managed.cache.Register(registerCtx, jwksURL,
			jwkfetch.WithWaitReady(true),
			jwkfetch.WithMinInterval(minimumRefreshInterval),
			jwkfetch.WithMaxInterval(maximumRefreshInterval),
		); registerErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if managed.cache.IsRegistered(cleanupCtx, jwksURL) {
				_ = managed.cache.Unregister(cleanupCtx, jwksURL)
			}
			return nil, errors.WrapIf(registerErr, "failed to register JWKS URL")
		}

		set, setErr := managed.cache.CachedSet(jwksURL)
		if setErr != nil {
			return nil, errors.WrapIf(setErr, "failed to create cached JWK set")
		}
		keySet := &keySet{
			cache:   managed.cache,
			jwksURL: jwksURL,
			set:     set,
		}

		m.mu.Lock()
		defer m.mu.Unlock()
		if m.shutdown.Load() {
			return nil, ErrManagerShutdown
		}
		managed.keySets[jwksURL] = keySet
		return keySet, nil
	})
	if err != nil {
		return nil, err
	}
	keySet, ok := value.(*keySet)
	if !ok || keySet == nil {
		return nil, errors.New("oidcjwk: invalid key set")
	}
	return keySet, nil
}

func (m *KeySetManager) createManagedCacheLockedInternal(client *http.Client) (*managedCache, error) {
	if m.caches.Len() >= maxHTTPClientPolicies {
		return nil, errors.New("oidcjwk: too many HTTP client policies")
	}
	cache, err := jwkfetch.NewCache(m.ctx, httprc.NewClient(),
		jwkfetch.WithHTTPClient(jwkfetch.WrapHTTPClientDefaults(client)),
		jwkfetch.WithMaxBodySize(maxJWKSBodySize),
		jwkfetch.WithParseOptions(
			jwk.WithMaxKeys(maxJWKSKeys),
			jwk.WithRejectDuplicateKID(true),
		),
	)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create JWKS cache")
	}
	managed := &managedCache{cache: cache, keySets: make(map[string]*keySet)}
	m.caches.Set(client, managed)
	return managed, nil
}

func (m *KeySetManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.shutdown.Swap(true) {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	m.active.Wait()

	m.mu.Lock()
	managedCaches := m.caches.Values()
	caches := make([]*jwkfetch.Cache, 0, len(managedCaches))
	for _, managed := range managedCaches {
		caches = append(caches, managed.cache)
	}
	m.caches.Purge()
	m.mu.Unlock()

	shutdownErrors := make([]error, 0, len(caches))
	for _, cache := range caches {
		shutdownErrors = append(shutdownErrors, cache.Shutdown(ctx))
	}
	return errors.Combine(shutdownErrors...)
}
