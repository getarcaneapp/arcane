package selfupdate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestVerify(t *testing.T) {
	trustJSON, err := os.ReadFile("testdata/trusted_root.json")
	require.NoError(t, err)
	trustedRoot, err := root.NewTrustedRootFromJSON(trustJSON)
	require.NoError(t, err)
	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	wrongDER, err := x509.MarshalPKIXPublicKey(&wrongKey.PublicKey)
	require.NoError(t, err)
	wrongPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: wrongDER})

	for _, version := range []string{"1.18.0", "2.15.1"} {
		t.Run(version, func(t *testing.T) {
			checksums, readErr := os.ReadFile("testdata/arcane_" + version + "_checksums.txt")
			require.NoError(t, readErr)
			bundleJSON, readErr := os.ReadFile("testdata/arcane_" + version + "_checksums.txt.sigstore.json")
			require.NoError(t, readErr)
			for _, tt := range []struct {
				name                                 string
				keys                                 [][]byte
				mutate                               func(*bundle.Bundle)
				alterChecksums, malformed, wantError bool
			}{
				{name: "valid", keys: [][]byte{cosignPublicKey}},
				{name: "altered checksums", keys: [][]byte{cosignPublicKey}, alterChecksums: true, wantError: true},
				{name: "wrong key", keys: [][]byte{wrongPEM}, wantError: true},
				{name: "rotation", keys: [][]byte{wrongPEM, cosignPublicKey}},
				{name: "malformed JSON", keys: [][]byte{cosignPublicKey}, malformed: true, wantError: true},
				{name: "no transparency log", keys: [][]byte{cosignPublicKey}, wantError: true, mutate: func(entity *bundle.Bundle) {
					entity.VerificationMaterial.TlogEntries = nil
				}},
				{name: "TSA only", keys: [][]byte{cosignPublicKey}, mutate: func(entity *bundle.Bundle) {
					for _, entry := range entity.VerificationMaterial.TlogEntries {
						entry.IntegratedTime = 0
						entry.InclusionPromise = nil
					}
				}},
				{name: "Rekor timestamp only", keys: [][]byte{cosignPublicKey}, mutate: func(entity *bundle.Bundle) {
					entity.VerificationMaterial.TimestampVerificationData = nil
				}},
				{name: "no observer timestamps", keys: [][]byte{cosignPublicKey}, wantError: true, mutate: func(entity *bundle.Bundle) {
					entity.VerificationMaterial.TimestampVerificationData = nil
					for _, entry := range entity.VerificationMaterial.TlogEntries {
						entry.IntegratedTime = 0
						entry.InclusionPromise = nil
					}
				}},
				{name: "ignore key hint", keys: [][]byte{cosignPublicKey}, mutate: func(entity *bundle.Bundle) {
					entity.VerificationMaterial.GetPublicKey().Hint = "untrusted key hint"
				}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					verifier, verificationErr := NewVerifier(trustedRoot, tt.keys)
					require.NoError(t, verificationErr)
					payload := bundleJSON
					if tt.mutate != nil {
						var entity bundle.Bundle
						require.NoError(t, entity.UnmarshalJSON(bundleJSON))
						tt.mutate(&entity)
						var marshalErr error
						payload, marshalErr = protojson.Marshal(entity.Bundle)
						require.NoError(t, marshalErr)
					}
					if tt.malformed {
						payload = []byte("{")
					}
					artifact := append([]byte(nil), checksums...)
					if tt.alterChecksums {
						artifact[0] ^= 1
					}
					verificationErr = verifier.Verify(artifact, payload)
					if tt.wantError {
						require.Error(t, verificationErr)
					} else {
						require.NoError(t, verificationErr)
					}
				})
			}
		})
	}
}

func TestNew(t *testing.T) {
	key, err := os.ReadFile("../../../cosign.pub")
	require.NoError(t, err)
	require.Equal(t, key, cosignPublicKey)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	verifier, err := New(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, verifier)
}
