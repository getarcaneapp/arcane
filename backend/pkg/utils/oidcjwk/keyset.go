package oidcjwk

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"emperror.dev/errors"
	"github.com/jwx-go/jwkfetch/v4"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
	"golang.org/x/sync/singleflight"
)

const forcedRefreshInterval = 30 * time.Second

var errForcedRefreshThrottled = errors.Sentinel("oidcjwk: forced refresh throttled")

type keySet struct {
	cache       *jwkfetch.Cache
	jwksURL     string
	set         jwk.Set
	nextRefresh atomic.Int64
	group       singleflight.Group
}

func (k *keySet) VerifySignature(ctx context.Context, rawToken string) ([]byte, error) {
	if err := validateTokenAlgorithmInternal(rawToken); err != nil {
		return nil, err
	}

	payload, verifyErr := k.verifyInternal(ctx, rawToken)
	if verifyErr == nil {
		return payload, nil
	}

	// Providers may rotate key material without changing kid, so every failure earns
	// one throttled refresh before the token is rejected.
	refreshResult := k.group.DoChan("refresh", func() (any, error) {
		if !k.reserveForcedRefreshInternal(time.Now()) {
			return nil, errForcedRefreshThrottled
		}
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), initialReadyTimeout)
		defer cancel()
		_, refreshErr := k.cache.Refresh(refreshCtx, k.jwksURL)
		if refreshErr != nil {
			return nil, errors.WrapIf(refreshErr, "failed to refresh JWKS")
		}
		return nil, nil
	})
	var refreshErr error
	select {
	case result := <-refreshResult:
		refreshErr = result.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if refreshErr != nil {
		if errors.Is(refreshErr, errForcedRefreshThrottled) {
			return k.verifyInternal(ctx, rawToken)
		}
		return nil, refreshErr
	}
	return k.verifyInternal(ctx, rawToken)
}

func (k *keySet) verifyInternal(ctx context.Context, rawToken string) ([]byte, error) {
	return jws.Verify([]byte(rawToken),
		jws.WithCompact(),
		jws.WithContext(ctx),
		jws.WithKeySet(k.set,
			jws.WithRequireKid(false),
			jws.WithInferAlgorithmFromKey(true),
		),
	)
}

func (k *keySet) reserveForcedRefreshInternal(now time.Time) bool {
	nowUnixNano := now.UnixNano()
	for {
		nextRefresh := k.nextRefresh.Load()
		if nowUnixNano < nextRefresh {
			return false
		}
		if k.nextRefresh.CompareAndSwap(nextRefresh, now.Add(forcedRefreshInterval).UnixNano()) {
			return true
		}
	}
}

func validateTokenAlgorithmInternal(rawToken string) error {
	message, err := jws.Parse([]byte(rawToken), jws.WithCompact())
	if err != nil {
		return errors.WrapIf(err, "malformed JWT")
	}
	signatures := message.Signatures()
	if len(signatures) != 1 {
		return errors.New("expected exactly one signature")
	}
	headers := signatures[0].ProtectedHeaders()
	if headers == nil {
		return errors.New("JWT is missing protected headers")
	}
	algorithm, ok := headers.Algorithm()
	if !ok || !slices.Contains(SupportedSigningAlgs(), algorithm.String()) {
		return errors.New("JWT uses an unsupported signing algorithm")
	}
	return nil
}
