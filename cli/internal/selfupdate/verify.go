// Package selfupdate verifies release checksums. Valid signatures do not prevent replay of older releases.
package selfupdate

import (
	"context"
	"crypto"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

// For rotation, ship a CLI trusting both keys before signing exclusively with the new key.
//
//go:embed cosign.pub
var cosignPublicKey []byte

// Verifier authenticates checksum bytes against the configured release keys and Sigstore infrastructure.
type Verifier struct {
	keys []*verify.Verifier
}

type trustHTTPClient struct {
	ctx    context.Context
	client http.Client
}

func (c *trustHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req.Clone(c.ctx))
}

// New loads Sigstore infrastructure trust from TUF, using unexpired cached metadata offline.
func New(ctx context.Context) (*Verifier, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to locate cache for %s trust data: %w", tuf.DefaultMirror, err)
	}
	opts := tuf.DefaultOptions()
	opts.CachePath = filepath.Join(cacheDir, "arcane-cli", "sigstore")
	opts.ForceCache = true
	httpFetcher := fetcher.NewDefaultFetcher()
	httpFetcher.SetHTTPClient(&trustHTTPClient{ctx: ctx, client: http.Client{Timeout: 30 * time.Second}})
	opts.Fetcher = httpFetcher
	client, err := tuf.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to load trust data from %s: %w", tuf.DefaultMirror, err)
	}
	trustedRoot, err := root.GetTrustedRoot(client)
	if err != nil {
		return nil, fmt.Errorf("failed to load trust data from %s: %w", tuf.DefaultMirror, err)
	}
	return NewVerifier(trustedRoot, [][]byte{cosignPublicKey})
}

// NewVerifier uses injected infrastructure trust and PEM release keys, ignoring bundle public-key hints.
func NewVerifier(infrastructure root.TrustedMaterial, publicKeys [][]byte) (*Verifier, error) {
	if len(publicKeys) == 0 {
		return nil, errors.New("no trusted release keys configured")
	}
	v := &Verifier{keys: make([]*verify.Verifier, 0, len(publicKeys))}
	for _, key := range publicKeys {
		publicKey, err := cryptoutils.UnmarshalPEMToPublicKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse trusted release key: %w", err)
		}
		keyVerifier, err := signature.LoadVerifier(publicKey, crypto.SHA256)
		if err != nil {
			return nil, fmt.Errorf("failed to load trusted release key: %w", err)
		}
		verifier, err := verify.NewVerifier(root.TrustedMaterialCollection{
			root.NewTrustedPublicKeyMaterial(func(string) (root.TimeConstrainedVerifier, error) {
				return root.NewExpiringKey(keyVerifier, time.Time{}, time.Time{}), nil
			}), infrastructure,
		}, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
		if err != nil {
			return nil, fmt.Errorf("failed to create checksum verifier: %w", err)
		}
		v.keys = append(v.keys, verifier)
	}
	return v, nil
}

// Verify requires a release-key signature, transparency-log inclusion, and an observer timestamp.
func (v *Verifier) Verify(checksums, bundleJSON []byte) error {
	var entity bundle.Bundle
	if err := entity.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("failed to parse checksum signature bundle: %w", err)
	}
	digest := sha256.Sum256(checksums)
	policy := verify.NewPolicy(verify.WithArtifactDigest("sha256", digest[:]), verify.WithKey())
	var failures []error
	for _, verifier := range v.keys {
		_, err := verifier.Verify(&entity, policy)
		if err == nil {
			return nil
		}
		failures = append(failures, err)
	}
	return fmt.Errorf("checksum signature verification failed: %w", errors.Join(failures...))
}
