package mldsajose

import (
	"context"
	"io"
	"net/http"
	"sync"

	"emperror.dev/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
)

type KeySet struct {
	client  *http.Client
	jwksURL string
	remote  *oidc.RemoteKeySet
	mu      sync.RWMutex
	keys    []Key
	group   singleflight.Group
}

func NewKeySet(ctx context.Context, jwksURL string) *KeySet {
	client := http.DefaultClient
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && c != nil {
		client = c
	}
	return &KeySet{
		client:  client,
		jwksURL: jwksURL,
		remote:  oidc.NewRemoteKeySet(ctx, jwksURL),
	}
}

func (k *KeySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	jws, err := jose.ParseSigned(jwt, joseAlgorithmsInternal(SupportedSigningAlgs()))
	if err != nil {
		return nil, errors.WrapIf(err, "malformed jwt")
	}
	if len(jws.Signatures) != 1 {
		return nil, errors.New("expected exactly one signature")
	}
	if !IsAlg(jws.Signatures[0].Header.Algorithm) {
		return k.remote.VerifySignature(ctx, jwt)
	}

	k.mu.RLock()
	keys := k.keys
	k.mu.RUnlock()
	if len(keys) > 0 {
		if payload, err := VerifyCompact(jwt, keys); err == nil {
			return payload, nil
		}
	}

	keys, err = k.refreshInternal(ctx)
	if err != nil {
		return nil, err
	}
	return VerifyCompact(jwt, keys)
}

func (k *KeySet) refreshInternal(ctx context.Context) ([]Key, error) {
	v, err, _ := k.group.Do("jwks", func() (any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.jwksURL, nil)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to build jwks request")
		}
		req.Header.Set("Cache-Control", "no-cache")

		resp, err := k.client.Do(req)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to fetch jwks")
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to read jwks response")
		}
		if resp.StatusCode != http.StatusOK {
			return nil, errors.Errorf("failed to fetch jwks: %s", resp.Status)
		}

		keys, err := ParseJWKS(body)
		if err != nil {
			return nil, err
		}

		k.mu.Lock()
		k.keys = keys
		k.mu.Unlock()
		return keys, nil
	})
	if err != nil {
		return nil, err
	}
	keys, ok := v.([]Key)
	if !ok {
		return nil, errors.New("jwks refresh returned invalid keys")
	}
	return keys, nil
}
