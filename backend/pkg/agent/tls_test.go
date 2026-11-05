package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kit.getarcane.app"
	"kit.getarcane.app/fs"
	libcrypto "kit.getarcane.app/sys/crypto"

	certgen "getarcane.dev/cli/v3/pkg/generate"
)

func TestMain(m *testing.M) {
	libcrypto.InitEncryption(libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-agent-mtls-32bytes-min",
		Environment:   "test",
	})
	os.Exit(m.Run())
}

func initAgentTestCrypto(t *testing.T) {
	t.Helper()
	libcrypto.InitEncryption(libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-agent-mtls-32bytes-min",
		Environment:   "test",
	})
}

func TestMTLSServer_GeneratesManagerCA(t *testing.T) {
	cfg := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	})

	_, err := cfg.Server(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, cfg.CAFile)
	require.FileExists(t, cfg.CAFile)

	configured := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		CAFile:    filepath.Join(t.TempDir(), "ca.crt"),
		AssetsDir: t.TempDir(),
	})
	_, err = configured.Server(t.Context())
	require.ErrorContains(t, err, "failed to load agent mTLS CA file")
	require.NoFileExists(t, filepath.Join(configured.AssetsDir, generatedMTLSCACertFileName))
}

func TestMTLSIssueClient(t *testing.T) {
	cfg := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	})

	assets, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-123", EnvironmentName: "Lab Server"})
	require.NoError(t, err)
	require.NotNil(t, assets)
	require.Empty(t, cfg.CAFile)
	issued, err := cfg.ClientCertificate("env-123")
	require.NoError(t, err)
	require.Equal(t, "Lab-Server-env-123", issued.Subject.CommonName)
	require.Equal(t, "./arcane-agent-certs", assets.HostDirHint)
	require.Len(t, assets.Files, 3)
	require.Equal(t, "ca.crt", assets.Files[0].Name)
	require.Contains(t, assets.Files[0].Content, "BEGIN CERTIFICATE")
	require.Equal(t, "/app/data/agent-mtls-client/ca.crt", assets.Files[0].ContainerPath)
	require.Equal(t, "/app/data/agent-mtls-client/agent.crt", assets.Files[1].ContainerPath)
	require.Equal(t, "agent.key", assets.Files[2].Name)
	require.Equal(t, "/app/data/agent-mtls-client/agent.key", assets.Files[2].ContainerPath)
	require.Contains(t, assets.Files[2].Content, "BEGIN PRIVATE KEY")

	keyBlock, _ := pem.Decode([]byte(assets.Files[2].Content))
	require.NotNil(t, keyBlock)
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	privateKey, ok := parsedKey.(*mldsa.PrivateKey)
	require.True(t, ok)
	require.Equal(t, mldsa.MLDSA87(), privateKey.PublicKey().Parameters())

	certBlock, _ := pem.Decode([]byte(assets.Files[1].Content))
	require.NotNil(t, certBlock)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	publicKey, ok := cert.PublicKey.(*mldsa.PublicKey)
	require.True(t, ok)
	require.Equal(t, mldsa.MLDSA87(), publicKey.Parameters())
	require.Equal(t, "Lab-Server-env-123", cert.Subject.CommonName)
	require.Len(t, cert.URIs, 1)
	require.Equal(t, "spiffe://manager.example.com/agent/env-123", cert.URIs[0].String())
}

func TestGeneratedClientCertificate_IncludesSANs(t *testing.T) {
	cfg := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	})

	assets, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-abc", EnvironmentName: "Lab Server"})
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
	require.Equal(t, "/agent/env-abc", uri.Path)

	require.Contains(t, cert.DNSNames, "arcane-agent")
	require.Contains(t, cert.DNSNames, "Lab-Server.agent.manager.example.com")

	require.True(t, cert.NotBefore.Before(cert.NotAfter))
	skew := cert.NotAfter.Sub(cert.NotBefore)
	require.Positive(t, skew)
}

func TestMTLSManagerClientEnroll_RejectsPlainHTTPEnrollment(t *testing.T) {
	cfg := NewMTLS(MTLSConfig{
		Mode:       MTLSModeRequired,
		AssetsDir:  t.TempDir(),
		ManagerURL: "http://manager.example.com/api",
	})

	err := cfg.enrollClient(t.Context(), "valid-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ARCANE_MANAGER_API_URL to use https for certificate enrollment")

	cfg.CertFile = filepath.Join(t.TempDir(), "agent.crt")
	cfg.KeyFile = filepath.Join(t.TempDir(), "agent.key")
	require.NoError(t, cfg.enrollClient(t.Context(), "valid-token"), "configured client files skip enrollment")
}

func TestMTLSManagerClientEnroll_LimitsEnrollmentErrorBody(t *testing.T) {
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

	cfg := NewMTLS(MTLSConfig{
		Mode:       MTLSModeRequired,
		CAFile:     caPath,
		AssetsDir:  t.TempDir(),
		ManagerURL: server.URL + "/api",
	})

	err := cfg.enrollClient(t.Context(), "valid-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent mTLS enrollment failed with status 500")
	require.LessOrEqual(t, len(err.Error()), maxEnrollResponseBytes+128)
}

func TestMTLSManagerClientEnroll_EnrollsWithoutUnusableGeneratedCert(t *testing.T) {
	generated, err := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	}).IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-renew", EnvironmentName: "Renew"})
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) > 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.MarshalWrite(w, EnrollMTLSResponse{Files: generated.Files})
	}))
	t.Cleanup(server.Close)

	caPath := filepath.Join(t.TempDir(), "manager.crt")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	}), 0o600))

	assetsDir := filepath.Join(t.TempDir(), "assets")
	require.NoError(t, os.MkdirAll(assetsDir, 0o700))
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	require.NoError(t, os.WriteFile(certPath, []byte("not a certificate"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("not a key"), 0o600))

	cfg := NewMTLS(MTLSConfig{
		Mode:       MTLSModeRequired,
		CAFile:     caPath,
		CertFile:   certPath,
		KeyFile:    keyPath,
		AssetsDir:  assetsDir,
		ManagerURL: server.URL + "/api",
	})

	require.NoError(t, cfg.enrollClient(t.Context(), "valid-token"))
	_, reason := loadClientCertificate(certPath, keyPath, time.Now(), 0)
	require.Empty(t, reason)
}

func TestMTLSManagerClientEnroll_UsesDownloadedCAPathWhenPresent(t *testing.T) {
	assetsDir := t.TempDir()
	caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)

	generated, err := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	}).IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-existing", EnvironmentName: "Existing"})
	require.NoError(t, err)
	require.NotNil(t, generated)
	for _, file := range generated.Files {
		targetPath := filepath.Join(assetsDir, filepath.Base(file.Name))
		perm := kit.Ternary(file.Permissions == "0600", 0o600, fs.FilePerm)
		require.NoError(t, os.WriteFile(targetPath, []byte(file.Content), perm))
	}

	cfg := NewMTLS(MTLSConfig{
		Mode:       MTLSModeRequired,
		AssetsDir:  assetsDir,
		ManagerURL: "https://manager.example.com/api",
	})

	require.NoError(t, cfg.enrollClient(t.Context(), "valid-token"))
	require.NotEmpty(t, cfg.CertFile)
	require.NotEmpty(t, cfg.KeyFile)
	require.FileExists(t, cfg.CertFile)
	require.FileExists(t, cfg.KeyFile)
	require.Equal(t, caPath, cfg.CAFile)
}

func TestRemoveStaleLock_PreservesLivePID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".ca.lock")
	require.NoError(t, os.WriteFile(lockPath, fmt.Appendf(nil, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano)), 0o600))

	require.False(t, removeStaleLock(lockPath))
	require.FileExists(t, lockPath)
}

func TestRemoveStaleLock_RemovesOldLockDespiteLivePID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".ca.lock")
	oldTimestamp := time.Now().Add(-(2*managerCALockTimeout + time.Second)).UTC().Format(time.RFC3339Nano)
	require.NoError(t, os.WriteFile(lockPath, fmt.Appendf(nil, "%d %s\n", os.Getpid(), oldTimestamp), 0o600))

	require.True(t, removeStaleLock(lockPath))
	require.NoFileExists(t, lockPath)
}

func TestMTLSClientTLS_OptionalIgnoresBrokenClientCertificate(t *testing.T) {
	assetsDir := t.TempDir()
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	require.NoError(t, os.WriteFile(certPath, []byte("not a cert"), 0o644))
	require.NoError(t, os.WriteFile(keyPath, []byte("not a key"), 0o600))

	tlsConfig, err := NewMTLS(MTLSConfig{
		Mode:       MTLSModeOptional,
		CertFile:   certPath,
		KeyFile:    keyPath,
		ManagerURL: "https://manager.example.com",
	}).clientTLS(t.Context())
	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	require.Empty(t, tlsConfig.Certificates)
}

func TestTunnelServerRequireCertificateIdentity_RejectsWrongEnvironmentURI(t *testing.T) {
	tunnels := NewTunnelRegistry()
	uriSAN, err := url.Parse("spiffe://manager.example.com/agent/env-a")
	require.NoError(t, err)
	cert := &x509.Certificate{URIs: []*url.URL{uriSAN}}
	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		MTLS:            NewMTLS(MTLSConfig{Mode: MTLSModeRequired, AppURL: "https://manager.example.com"}),
	})

	require.NoError(t, server.requireCertificateIdentity(state, "env-a"))
	require.Error(t, server.requireCertificateIdentity(state, "env-b"))

	server.MTLS.AppURL = "https://other.example.com"
	require.Error(t, server.requireCertificateIdentity(state, "env-a"), "a different trust domain must not match")
}

func TestMTLSServer_DoesNotRequireArcaneTLSTermination(t *testing.T) {
	_, err := NewMTLS(MTLSConfig{Mode: MTLSModeRequired, AssetsDir: t.TempDir()}).Server(t.Context())
	require.NoError(t, err)

	assetsDir := t.TempDir()
	_, _, err = ensureManagerCA(t.Context(), assetsDir)
	require.NoError(t, err)
	caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)

	_, err = NewMTLS(MTLSConfig{Mode: MTLSModeRequired, CAFile: caPath}).Server(t.Context())
	require.NoError(t, err)
}

func TestMTLSServer_RejectsMissingOrMalformedCAFile(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := NewMTLS(MTLSConfig{Mode: MTLSModeRequired, CAFile: filepath.Join(t.TempDir(), "missing-ca.crt")}).Server(t.Context())
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to load agent mTLS CA file")
	})

	t.Run("malformed file", func(t *testing.T) {
		caPath := filepath.Join(t.TempDir(), "ca.crt")
		require.NoError(t, os.WriteFile(caPath, []byte("not a pem"), 0o600))

		_, err := NewMTLS(MTLSConfig{Mode: MTLSModeRequired, CAFile: caPath}).Server(t.Context())
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to load agent mTLS CA file")
	})
}

func TestTunnelServerRequiredMTLS_AllowsHTTPRequestsWithoutVisibleTLSState(t *testing.T) {
	tunnels := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		MTLS:            NewMTLS(MTLSConfig{Mode: MTLSModeRequired, AppURL: "https://manager.example.com"}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tunnel/connect", http.NoBody)
	require.NoError(t, server.requireCertificateIdentity(req.TLS, "env-a"))
}

func TestTunnelServerRequiredMTLS_RejectsDirectTLSWithoutVerifiedClientCertificate(t *testing.T) {
	tunnels := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		MTLS:            NewMTLS(MTLSConfig{Mode: MTLSModeRequired}),
	})

	req := httptest.NewRequest(http.MethodGet, "https://manager.example.com/api/tunnel/connect", http.NoBody)
	req.TLS = &tls.ConnectionState{}
	require.ErrorContains(t, server.requireCertificateIdentity(req.TLS, "env-a"), "verified agent mTLS client certificate is required")

	server.MTLS = NewMTLS(MTLSConfig{Mode: MTLSModeOptional})
	require.NoError(t, server.requireCertificateIdentity(req.TLS, "env-a"))
}

func TestTunnelServerRequiredMTLS_DetectsOnlyDirectTLSForRequestSecurityMode(t *testing.T) {
	tunnels := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		MTLS:            NewMTLS(MTLSConfig{Mode: MTLSModeRequired, AppURL: "https://manager.example.com"}),
	})

	cert := &x509.Certificate{}
	req := httptest.NewRequest(http.MethodGet, "/api/tunnel/connect", http.NoBody)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	require.True(t, hasVerifiedPeerCertificate(req.TLS))

	req = httptest.NewRequest(http.MethodGet, "/api/tunnel/connect", http.NoBody)
	req.Header.Set("X-SSL-Client-Verify", "SUCCESS")
	require.False(t, hasVerifiedPeerCertificate(req.TLS))
	require.NoError(t, server.requireCertificateIdentity(req.TLS, "env-a"))
}

func TestLoadClientCertificate_RenewsExpiredCertificate(t *testing.T) {
	assetsDir := t.TempDir()
	assets, err := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	}).IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-renew", EnvironmentName: "Renew"})
	require.NoError(t, err)

	for _, file := range assets.Files {
		targetPath := filepath.Join(assetsDir, filepath.Base(file.Name))
		perm := kit.Ternary(file.Permissions == "0600", 0o600, fs.FilePerm)
		require.NoError(t, os.WriteFile(targetPath, []byte(file.Content), perm))
	}

	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	cert, reason := loadClientCertificate(certPath, keyPath, time.Now(), agentMTLSRenewBefore)
	require.Empty(t, reason)

	_, reason = loadClientCertificate(certPath, keyPath, cert.Leaf.NotAfter.Add(24*time.Hour), agentMTLSRenewBefore)
	require.Contains(t, reason, "expired")
}

func TestLoadClientCertificate_RenewBeforeOnlyAppliesWhenRequested(t *testing.T) {
	assetsDir := t.TempDir()
	assets, err := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	}).IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-window", EnvironmentName: "Window"})
	require.NoError(t, err)

	for _, file := range assets.Files {
		targetPath := filepath.Join(assetsDir, filepath.Base(file.Name))
		perm := kit.Ternary(file.Permissions == "0600", 0o600, fs.FilePerm)
		require.NoError(t, os.WriteFile(targetPath, []byte(file.Content), perm))
	}

	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	cert, reason := loadClientCertificate(certPath, keyPath, time.Now(), 0)
	require.Empty(t, reason)

	nearExpiry := cert.Leaf.NotAfter.Add(-24 * time.Hour)
	_, reason = loadClientCertificate(certPath, keyPath, nearExpiry, 0)
	require.Empty(t, reason, "a certificate inside the renewal window stays usable when no window is requested")

	_, reason = loadClientCertificate(certPath, keyPath, nearExpiry, agentMTLSRenewBefore)
	require.Contains(t, reason, "expires soon")
}

func TestMTLSIssueClient_ReissuesMismatchedKeyPair(t *testing.T) {
	cfg := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	})
	_, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-123", EnvironmentName: "Lab Server"})
	require.NoError(t, err)

	replacementKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	replacementKeyDER, err := x509.MarshalECPrivateKey(replacementKey)
	require.NoError(t, err)
	clientKeyPath := filepath.Join(cfg.AssetsDir, generatedClientMTLSSubdir, "env-123", generatedMTLSClientKeyName)
	require.NoError(t, os.WriteFile(clientKeyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: replacementKeyDER}), 0o600))

	assets, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-123", EnvironmentName: "Lab Server"})
	require.NoError(t, err)
	require.True(t, assets.CertIssued, "a key that no longer matches the certificate must be reissued")
	_, err = tls.X509KeyPair([]byte(assets.Files[1].Content), []byte(assets.Files[2].Content))
	require.NoError(t, err)
}

func TestMTLSIssueClient_PreservesCertificateWhenEnvironmentNameChanges(t *testing.T) {
	cfg := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: t.TempDir(),
		AppURL:    "https://manager.example.com",
	})
	original, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-123", EnvironmentName: ""})
	require.NoError(t, err)

	renamed, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-123", EnvironmentName: "Lab Server"})
	require.NoError(t, err)
	require.False(t, renamed.CertIssued)
	require.Equal(t, original.Files[1].Content, renamed.Files[1].Content)

	certBlock, _ := pem.Decode([]byte(renamed.Files[1].Content))
	require.NotNil(t, certBlock)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, "env-123", cert.Subject.CommonName)
}

func TestCAKey_EncryptedOnDiskWhenCryptoInitialized(t *testing.T) {
	initAgentTestCrypto(t)

	assetsDir := t.TempDir()
	ca, generated, err := ensureManagerCA(t.Context(), assetsDir)
	require.NoError(t, err)
	require.True(t, generated)

	raw, err := os.ReadFile(filepath.Join(assetsDir, generatedMTLSCAKeyFileName))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(raw), caKeyEncryptedPrefix))
	require.NotContains(t, string(raw), "BEGIN PRIVATE KEY", "encrypted CA key file must not contain plain PEM markers")

	reused, generated, err := ensureManagerCA(t.Context(), assetsDir)
	require.NoError(t, err)
	require.False(t, generated, "the encrypted CA key must decrypt and be reused")
	require.Equal(t, ca.Certificate, reused.Certificate)

	_, err = NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: assetsDir,
		AppURL:    "https://manager.example.com",
	}).IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-round", EnvironmentName: "Round Trip"})
	require.NoError(t, err)
}

func TestCAKey_EncryptionFailureReturnsError(t *testing.T) {
	originalEncrypt := caKeyEncrypt
	t.Cleanup(func() {
		caKeyEncrypt = originalEncrypt
	})
	caKeyEncrypt = func(string) (string, error) {
		return "", errors.New("encrypt failed")
	}

	assetsDir := t.TempDir()
	_, _, err := ensureManagerCA(t.Context(), assetsDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to encrypt agent mTLS CA private key")
	require.NoFileExists(t, filepath.Join(assetsDir, generatedMTLSCAKeyFileName))
}

func TestLockAgentMTLSPath_ReturnsOnContextCancellation(t *testing.T) {
	assetsDir := t.TempDir()

	unlock, err := lockAgentMTLSPath(t.Context(), assetsDir, ".ca.lock")
	require.NoError(t, err)
	defer unlock()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = lockAgentMTLSPath(ctx, assetsDir, ".ca.lock")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancelled waiting for agent mTLS CA lock")
}

func TestCAKey_EmptyEncryptionPayloadReturnsError(t *testing.T) {
	originalEncrypt := caKeyEncrypt
	t.Cleanup(func() {
		caKeyEncrypt = originalEncrypt
	})
	caKeyEncrypt = func(string) (string, error) {
		return "", nil
	}

	assetsDir := t.TempDir()
	_, _, err := ensureManagerCA(t.Context(), assetsDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypted payload is empty")
	require.NoFileExists(t, filepath.Join(assetsDir, generatedMTLSCAKeyFileName))
}

func TestGeneratedMLDSAAssets_HandshakeWithVerifiedClientCertificate(t *testing.T) {
	tunnels := NewTunnelRegistry()
	assetsDir := t.TempDir()
	cfg := NewMTLS(MTLSConfig{
		Mode:      MTLSModeRequired,
		AssetsDir: assetsDir,
		AppURL:    "https://manager.example.com",
	})
	_, err := cfg.IssueClient(t.Context(), IssueClientOptions{EnvironmentID: "env-hs", EnvironmentName: "Handshake"})
	require.NoError(t, err)

	caCertPath := filepath.Join(assetsDir, "ca.crt")
	ca, caGenerated, err := ensureManagerCA(t.Context(), assetsDir)
	require.NoError(t, err)
	require.False(t, caGenerated)

	serverKey, err := certgen.GenerateMLDSA87PrivateKey()
	require.NoError(t, err)
	serverTemplate, err := certgen.NewServerTLSTemplate("localhost", []string{"localhost", "127.0.0.1"})
	require.NoError(t, err)
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca.Leaf, serverKey.PublicKey(), ca.PrivateKey)
	require.NoError(t, err)

	serverTLS, err := NewMTLS(MTLSConfig{Mode: MTLSModeRequired, CAFile: caCertPath}).Server(t.Context())
	require.NoError(t, err)
	require.NotNil(t, serverTLS)
	serverTLS.Certificates = []tls.Certificate{{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}}

	clientCertPath := filepath.Join(assetsDir, generatedClientMTLSSubdir, "env-hs", generatedMTLSClientCertName)
	clientTLS, err := NewMTLS(MTLSConfig{
		Mode:       MTLSModeRequired,
		CAFile:     caCertPath,
		CertFile:   clientCertPath,
		KeyFile:    filepath.Join(filepath.Dir(clientCertPath), "agent.key"),
		ServerName: "localhost",
		ManagerURL: "https://localhost/api",
	}).clientTLS(t.Context())
	require.NoError(t, err)
	require.NotNil(t, clientTLS)
	require.Equal(t, uint16(tls.VersionTLS13), clientTLS.MinVersion)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	require.NoError(t, err)
	defer func() { assert.NoError(t, listener.Close()) }()

	type handshakeResult struct {
		err   error
		state tls.ConnectionState
	}
	resultCh := make(chan handshakeResult, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			resultCh <- handshakeResult{err: acceptErr}
			return
		}
		defer func() { assert.NoError(t, conn.Close()) }()
		tlsConn := conn.(*tls.Conn)
		if handshakeErr := tlsConn.Handshake(); handshakeErr != nil {
			resultCh <- handshakeResult{err: handshakeErr}
			return
		}
		resultCh <- handshakeResult{state: tlsConn.ConnectionState()}
	}()

	conn, err := tls.Dial("tcp", listener.Addr().String(), clientTLS)
	require.NoError(t, err)
	defer func() { assert.NoError(t, conn.Close()) }()
	require.Equal(t, uint16(tls.VersionTLS13), conn.ConnectionState().Version)

	result := <-resultCh
	require.NoError(t, result.err)
	require.Equal(t, uint16(tls.VersionTLS13), result.state.Version)
	require.True(t, hasVerifiedPeerCertificate(&result.state))
	server := NewTunnelServer(TunnelServerOptions{StreamWindowMiB: testStreamWindowMiB, Registry: tunnels, MTLS: cfg})
	require.NoError(t, server.requireCertificateIdentity(&result.state, "env-hs"))
}
