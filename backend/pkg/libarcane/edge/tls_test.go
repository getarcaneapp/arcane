package edge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	libcrypto "github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type testAuthInfo struct{}

func (testAuthInfo) AuthType() string { return "test" }

func TestMain(m *testing.M) {
	libcrypto.InitEncryption(&libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-edge-mtls-32bytes-min",
		Environment:   "test",
	})
	os.Exit(m.Run())
}

func initEdgeTestCrypto(t *testing.T) {
	t.Helper()
	libcrypto.InitEncryption(&libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-edge-mtls-32bytes-min",
		Environment:   "test",
	})
}

func TestPrepareManagerMTLSAssetsWithContext(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
		AppURL:            "https://manager.example.com",
	}

	require.NoError(t, PrepareManagerMTLSAssetsWithContext(context.Background(), cfg))
	require.NotEmpty(t, cfg.EdgeMTLSCAFile)
	require.FileExists(t, cfg.EdgeMTLSCAFile)
}

func TestGenerateManagerClientMTLSAssetsWithContext(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
		AppURL:            "https://manager.example.com",
	}

	assets, err := GenerateManagerClientMTLSAssetsWithContext(context.Background(), cfg, "env-123", "Lab Server")
	require.NoError(t, err)
	require.NotNil(t, assets)
	require.Empty(t, cfg.EdgeMTLSCAFile)
	clientCertPath, err := GeneratedManagerClientMTLSCertPath(cfg, "env-123")
	require.NoError(t, err)
	require.FileExists(t, clientCertPath)
	require.Equal(t, "./arcane-edge-certs", assets.HostDirHint)
	require.Len(t, assets.Files, 3)
	require.Equal(t, "ca.crt", assets.Files[0].Name)
	require.Contains(t, assets.Files[0].Content, "BEGIN CERTIFICATE")
	require.Equal(t, "/app/data/edge-mtls-agent/ca.crt", assets.Files[0].ContainerPath)
	require.Equal(t, "/app/data/edge-mtls-agent/agent.crt", assets.Files[1].ContainerPath)
	require.Equal(t, "agent.key", assets.Files[2].Name)
	require.Equal(t, "/app/data/edge-mtls-agent/agent.key", assets.Files[2].ContainerPath)
	require.Contains(t, assets.Files[2].Content, "BEGIN EC PRIVATE KEY")

	keyBlock, _ := pem.Decode([]byte(assets.Files[2].Content))
	require.NotNil(t, keyBlock)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, elliptic.P384(), privateKey.Curve)

	certBlock, _ := pem.Decode([]byte(assets.Files[1].Content))
	require.NotNil(t, certBlock)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	require.Equal(t, elliptic.P384(), publicKey.Curve)
	require.Equal(t, "Lab-Server-env-123", cert.Subject.CommonName)
	require.Len(t, cert.URIs, 1)
	require.Equal(t, "spiffe://manager.example.com/edge/env-123", cert.URIs[0].String())
}

func TestGeneratedClientCertificate_IncludesSANs(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
		AppURL:            "https://manager.example.com",
	}

	assets, err := GenerateManagerClientMTLSAssetsWithContext(context.Background(), cfg, "env-abc", "Lab Server")
	require.NoError(t, err)
	require.NotNil(t, assets)

	var certPEM string
	for _, file := range assets.Files {
		if file.Name == generatedMTLSClientCertName {
			certPEM = file.Content
			break
		}
	}
	require.NotEmpty(t, certPEM, "agent cert must be in generated assets")

	block, _ := pem.Decode([]byte(certPEM))
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	require.NotEmpty(t, cert.URIs, "agent cert must include URI SAN for stable identity")
	uri := cert.URIs[0]
	require.Equal(t, "spiffe", uri.Scheme)
	require.Equal(t, "manager.example.com", uri.Host)
	require.Contains(t, uri.Path, "env-abc")

	require.NotEmpty(t, cert.DNSNames, "agent cert must include at least one DNS SAN")
	require.Contains(t, cert.DNSNames, "arcane-agent")

	require.True(t, cert.NotBefore.Before(cert.NotAfter))
	skew := cert.NotAfter.Sub(cert.NotBefore)
	require.True(t, skew > 0)
}

func TestEnsureAgentMTLSAssets_RejectsPlainHTTPEnrollment(t *testing.T) {
	cfg := &Config{
		ManagerApiUrl:     "http://manager.example.com/api",
		AgentToken:        "valid-token",
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	err := EnsureAgentMTLSAssets(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MANAGER_API_URL to use https for certificate enrollment")
}

func TestEnsureAgentMTLSAssets_LimitsEnrollmentErrorBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("a", maxEnrollResponseBytes*2)))
	}))
	t.Cleanup(server.Close)

	caPath := filepath.Join(t.TempDir(), "manager.crt")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	}), 0o600))

	cfg := &Config{
		ManagerApiUrl:     server.URL + "/api",
		AgentToken:        "valid-token",
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSCAFile:    caPath,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	err := EnsureAgentMTLSAssets(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "edge mTLS enrollment failed with status 500")
	require.LessOrEqual(t, len(err.Error()), maxEnrollResponseBytes+128)
}

func TestEnsureAgentMTLSAssets_UsesDownloadedCAPathWhenPresent(t *testing.T) {
	assetsDir := t.TempDir()
	caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)

	generated, err := GenerateManagerClientMTLSAssetsWithContext(context.Background(), &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
		AppURL:            "https://manager.example.com",
	}, "env-existing", "Existing")
	require.NoError(t, err)
	require.NotNil(t, generated)
	for _, file := range generated.Files {
		targetPath := filepath.Join(assetsDir, filepath.Base(file.Name))
		perm := common.FilePerm
		if file.Permissions == "0600" {
			perm = 0o600
		}
		require.NoError(t, os.WriteFile(targetPath, []byte(file.Content), perm))
	}

	cfg := &Config{
		ManagerApiUrl:     "https://manager.example.com/api",
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: assetsDir,
	}

	require.NoError(t, EnsureAgentMTLSAssets(context.Background(), cfg))
	require.NotEmpty(t, cfg.EdgeMTLSCertFile)
	require.NotEmpty(t, cfg.EdgeMTLSKeyFile)
	require.FileExists(t, cfg.EdgeMTLSCertFile)
	require.FileExists(t, cfg.EdgeMTLSKeyFile)
	require.Equal(t, caPath, cfg.EdgeMTLSCAFile)
}

func TestRemoveStaleEdgeMTLSLockInternal_PreservesLivePID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".ca.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf("%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))), 0o600))

	require.False(t, removeStaleEdgeMTLSLockInternal(lockPath))
	require.FileExists(t, lockPath)
}

func TestRemoveStaleEdgeMTLSLockInternal_RemovesOldLockDespiteLivePID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".ca.lock")
	oldTimestamp := time.Now().Add(-(2*managerCALockTimeout + time.Second)).UTC().Format(time.RFC3339Nano)
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf("%d %s\n", os.Getpid(), oldTimestamp)), 0o600))

	require.True(t, removeStaleEdgeMTLSLockInternal(lockPath))
	require.NoFileExists(t, lockPath)
}

func TestBuildManagerClientTLSConfigInternal_OptionalIgnoresBrokenClientCertificate(t *testing.T) {
	assetsDir := t.TempDir()
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	require.NoError(t, os.WriteFile(certPath, []byte("not a cert"), 0o644))
	require.NoError(t, os.WriteFile(keyPath, []byte("not a key"), 0o600))

	tlsConfig, err := buildManagerClientTLSConfigInternal(&Config{
		ManagerApiUrl:    "https://manager.example.com",
		EdgeMTLSMode:     EdgeMTLSModeOptional,
		EdgeMTLSCertFile: certPath,
		EdgeMTLSKeyFile:  keyPath,
	})
	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	require.Empty(t, tlsConfig.Certificates)
}

func TestVerifiedPeerCertificateEnvironmentIDMatchesInternal(t *testing.T) {
	uriSAN, err := url.Parse("spiffe://manager.example.com/edge/env-123")
	require.NoError(t, err)
	cert := &x509.Certificate{URIs: []*url.URL{uriSAN}}
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}

	require.NoError(t, verifiedPeerCertificateEnvironmentIDMatchesInternal(state, "env-123", "manager.example.com"))
	require.Error(t, verifiedPeerCertificateEnvironmentIDMatchesInternal(state, "env-456", "manager.example.com"))
	require.Error(t, verifiedPeerCertificateEnvironmentIDMatchesInternal(state, "env-123", "other.example.com"))
}

func TestTunnelServerRequireCertificateIdentityInternal_RejectsWrongEnvironmentURI(t *testing.T) {
	uriSAN, err := url.Parse("spiffe://manager.example.com/edge/env-a")
	require.NoError(t, err)
	cert := &x509.Certificate{URIs: []*url.URL{uriSAN}}
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	server := NewTunnelServer(nil, nil)
	server.SetConfig(&Config{
		EdgeMTLSMode: EdgeMTLSModeRequired,
		AppURL:       "https://manager.example.com",
	})

	require.NoError(t, server.requireCertificateIdentityInternal(state, "env-a"))
	require.Error(t, server.requireCertificateIdentityInternal(state, "env-b"))
}

func TestValidateManagerMTLSConfig_DoesNotRequireArcaneTLSTermination(t *testing.T) {
	assetsDir := t.TempDir()
	caPath, _, _, err := ensureManagerCAInternal(context.Background(), assetsDir)
	require.NoError(t, err)

	err = ValidateManagerMTLSConfig(&Config{
		EdgeMTLSMode:   EdgeMTLSModeRequired,
		EdgeMTLSCAFile: caPath,
	})
	require.NoError(t, err)
}

func TestValidateManagerMTLSConfig_RejectsMissingOrMalformedCAFile(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		err := ValidateManagerMTLSConfig(&Config{
			EdgeMTLSMode:   EdgeMTLSModeRequired,
			EdgeMTLSCAFile: filepath.Join(t.TempDir(), "missing-ca.crt"),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to load edge mTLS CA file")
	})

	t.Run("malformed file", func(t *testing.T) {
		caPath := filepath.Join(t.TempDir(), "ca.crt")
		require.NoError(t, os.WriteFile(caPath, []byte("not a pem"), 0o600))

		err := ValidateManagerMTLSConfig(&Config{
			EdgeMTLSMode:   EdgeMTLSModeRequired,
			EdgeMTLSCAFile: caPath,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to load edge mTLS CA file")
	})
}

func TestTunnelServerRequiredMTLS_RejectsRequestWithoutTLSState(t *testing.T) {
	server := NewTunnelServer(nil, nil)
	server.SetConfig(&Config{
		EdgeMTLSMode: EdgeMTLSModeRequired,
		AppURL:       "https://manager.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tunnel/connect", nil)
	require.ErrorContains(t, server.requireClientCertificateInternal(req), "verified client certificate required")
	require.NoError(t, server.requireCertificateIdentityInternal(nil, "env-a"))
}

func TestTunnelServerRequiredMTLS_RejectsGRPCContextsWithoutVerifiedPeerCertificate(t *testing.T) {
	server := NewTunnelServer(nil, nil)
	server.SetConfig(&Config{EdgeMTLSMode: EdgeMTLSModeRequired})

	t.Run("missing peer", func(t *testing.T) {
		require.ErrorContains(t, server.requireClientCertificateFromContextInternal(context.Background()), "verified client certificate required")
	})

	t.Run("non tls peer", func(t *testing.T) {
		ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: testAuthInfo{}})
		require.ErrorContains(t, server.requireClientCertificateFromContextInternal(ctx), "verified client certificate required")
	})

	t.Run("verified tls peer", func(t *testing.T) {
		cert := &x509.Certificate{}
		ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
			VerifiedChains:   [][]*x509.Certificate{{cert}},
		}}})
		require.NoError(t, server.requireClientCertificateFromContextInternal(ctx))
	})
}

func TestAgentMTLSAssetsNeedEnrollmentInternal_RenewsExpiredCertificate(t *testing.T) {
	assetsDir := t.TempDir()
	assets, err := GenerateManagerClientMTLSAssetsWithContext(context.Background(), &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
		AppURL:            "https://manager.example.com",
	}, "env-renew", "Renew")
	require.NoError(t, err)

	for _, file := range assets.Files {
		targetPath := filepath.Join(assetsDir, filepath.Base(file.Name))
		perm := common.FilePerm
		if file.Permissions == "0600" {
			perm = 0o600
		}
		require.NoError(t, os.WriteFile(targetPath, []byte(file.Content), perm))
	}

	needsEnrollment, reason := agentMTLSAssetsNeedEnrollmentInternal(
		filepath.Join(assetsDir, generatedMTLSClientCertName),
		filepath.Join(assetsDir, generatedMTLSClientKeyName),
		time.Now().Add(generatedMTLSCertValidity+24*time.Hour),
	)
	require.True(t, needsEnrollment)
	require.Contains(t, reason, "expired")
}

func TestValidateGeneratedClientCertificateInternal_RejectsMismatchedKeyPair(t *testing.T) {
	assetsDir := t.TempDir()

	clientCertPath, _, _, err := ensureClientCertificateInternal(context.Background(), assetsDir, "env-123", "Lab Server", "https://manager.example.com")
	require.NoError(t, err)

	replacementKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	replacementKeyDER, err := x509.MarshalECPrivateKey(replacementKey)
	require.NoError(t, err)

	clientKeyPath := filepath.Join(assetsDir, generatedClientMTLSSubdir, "env-123", generatedMTLSClientKeyName)
	err = writePEMFileInternal(clientKeyPath, "EC PRIVATE KEY", replacementKeyDER, 0o600)
	require.NoError(t, err)

	expectedURI, err := url.Parse("spiffe://manager.example.com/edge/env-123")
	require.NoError(t, err)

	err = validateGeneratedClientCertificateInternal(clientCertPath, clientKeyPath, "Lab-Server-env-123", expectedURI)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match private key")
}

func TestEnsureClientCertificateInternal_PreservesCertificateWhenEnvironmentNameChanges(t *testing.T) {
	assetsDir := t.TempDir()

	clientCertPath, _, _, err := ensureClientCertificateInternal(context.Background(), assetsDir, "env-123", "", "https://manager.example.com")
	require.NoError(t, err)

	originalPEM, err := os.ReadFile(clientCertPath)
	require.NoError(t, err)

	clientCertPath, _, _, err = ensureClientCertificateInternal(context.Background(), assetsDir, "env-123", "Lab Server", "https://manager.example.com")
	require.NoError(t, err)

	updatedPEM, err := os.ReadFile(clientCertPath)
	require.NoError(t, err)
	require.Equal(t, string(originalPEM), string(updatedPEM))

	certBlock, _ := pem.Decode(updatedPEM)
	require.NotNil(t, certBlock)

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, "env-123", cert.Subject.CommonName)
}

func TestCAKey_EncryptedOnDiskWhenCryptoInitialized(t *testing.T) {
	initEdgeTestCrypto(t)

	assetsDir := t.TempDir()
	caCertPath, caKeyPath, _, err := ensureManagerCAInternal(context.Background(), assetsDir)
	require.NoError(t, err)
	require.FileExists(t, caCertPath)
	require.FileExists(t, caKeyPath)

	require.True(t, isCAKeyEncryptedOnDiskInternal(caKeyPath),
		"CA key file must be stored in the encrypted envelope format when libcrypto is initialized")

	raw, err := os.ReadFile(caKeyPath)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), "BEGIN EC PRIVATE KEY"),
		"encrypted CA key file must not contain plain PEM markers")

	pemBytes, err := readCAKeyPEMInternal(caKeyPath)
	require.NoError(t, err)
	block, _ := pem.Decode(pemBytes)
	require.NotNil(t, block)
	_, err = x509.ParseECPrivateKey(block.Bytes)
	require.NoError(t, err)

	clientCertPath, clientKeyPath, _, err := ensureClientCertificateInternal(context.Background(), assetsDir, "env-round", "Round Trip", "https://manager.example.com")
	require.NoError(t, err)
	require.FileExists(t, clientCertPath)
	require.FileExists(t, clientKeyPath)
}

func TestCAKey_MigratesLegacyPlainFile(t *testing.T) {
	initEdgeTestCrypto(t)

	assetsDir := t.TempDir()
	_, caKeyPath, _, err := ensureManagerCAInternal(context.Background(), assetsDir)
	require.NoError(t, err)

	pemBytes, err := readCAKeyPEMInternal(caKeyPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(caKeyPath, pemBytes, 0o600))
	require.False(t, isCAKeyEncryptedOnDiskInternal(caKeyPath))

	_, caKeyPath2, _, err := ensureManagerCAInternal(context.Background(), assetsDir)
	require.NoError(t, err)
	require.Equal(t, caKeyPath, caKeyPath2)
	require.True(t, isCAKeyEncryptedOnDiskInternal(caKeyPath),
		"legacy plain CA key file must be migrated to the encrypted envelope format on next load")
}

func TestCAKey_LegacyPlainMigrationFailureReturnsError(t *testing.T) {
	initEdgeTestCrypto(t)

	assetsDir := t.TempDir()
	_, caKeyPath, _, err := ensureManagerCAInternal(context.Background(), assetsDir)
	require.NoError(t, err)

	pemBytes, err := readCAKeyPEMInternal(caKeyPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(caKeyPath, pemBytes, 0o600))

	originalEncrypt := caKeyEncryptInternal
	t.Cleanup(func() {
		caKeyEncryptInternal = originalEncrypt
	})
	caKeyEncryptInternal = func(string) (string, error) {
		return "", errors.New("encrypt failed")
	}

	_, _, _, err = ensureManagerCAInternal(context.Background(), assetsDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to migrate edge mTLS CA key to encrypted format")
}

func TestCAKey_EncryptionFailureReturnsError(t *testing.T) {
	originalEncrypt := caKeyEncryptInternal
	t.Cleanup(func() {
		caKeyEncryptInternal = originalEncrypt
	})

	caKeyPath := filepath.Join(t.TempDir(), "ca.key")
	caKeyEncryptInternal = func(string) (string, error) {
		return "", errors.New("encrypt failed")
	}

	err := writeCAKeyFileInternal(caKeyPath, []byte{1, 2, 3})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to encrypt edge mTLS CA private key")
	require.NoFileExists(t, caKeyPath)
}

func TestLockEdgeMTLSPathInternal_ReturnsOnContextCancellation(t *testing.T) {
	assetsDir := t.TempDir()

	unlock, err := lockEdgeMTLSPathInternal(context.Background(), assetsDir, ".ca.lock")
	require.NoError(t, err)
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = lockEdgeMTLSPathInternal(ctx, assetsDir, ".ca.lock")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancelled waiting for edge mTLS CA lock")
}

func TestCAKey_EmptyEncryptionPayloadReturnsError(t *testing.T) {
	originalEncrypt := caKeyEncryptInternal
	t.Cleanup(func() {
		caKeyEncryptInternal = originalEncrypt
	})

	caKeyPath := filepath.Join(t.TempDir(), "ca.key")
	caKeyEncryptInternal = func(string) (string, error) {
		return "", nil
	}

	err := writeCAKeyFileInternal(caKeyPath, []byte{1, 2, 3})
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypted payload is empty")
	require.NoFileExists(t, caKeyPath)
}

func TestBuildGeneratedClientSANsInternal(t *testing.T) {
	uri, dns := buildGeneratedClientSANsInternal("Lab Server", "env-123", "https://manager.example.com")
	require.NotNil(t, uri)
	require.Equal(t, "spiffe", uri.Scheme)
	require.Equal(t, "manager.example.com", uri.Host)
	require.Equal(t, "/edge/env-123", uri.Path)
	require.Contains(t, dns, "arcane-agent")
	require.Contains(t, dns, "Lab-Server.agent.manager.example.com")

	uriEmpty, dnsEmpty := buildGeneratedClientSANsInternal("", "", "https://manager.example.com")
	require.Nil(t, uriEmpty)
	require.Nil(t, dnsEmpty)
}

func TestShouldAutoGenerateManagerCAInternal(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	require.True(t, shouldAutoGenerateManagerCAInternal(cfg))

	cfg.EdgeMTLSCAFile = filepath.Join(t.TempDir(), "ca.crt")
	require.False(t, shouldAutoGenerateManagerCAInternal(cfg))
}

func TestShouldAutoEnrollAgentMTLSInternal(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode: EdgeMTLSModeRequired,
	}

	require.True(t, shouldAutoEnrollAgentMTLSInternal(cfg))

	cfg.EdgeMTLSCertFile = filepath.Join(t.TempDir(), "agent.crt")
	cfg.EdgeMTLSKeyFile = filepath.Join(t.TempDir(), "agent.key")
	require.False(t, shouldAutoEnrollAgentMTLSInternal(cfg))
}
