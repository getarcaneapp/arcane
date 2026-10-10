package edge

import (
	"cmp"
	"context"
	"crypto"
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
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	certgen "github.com/getarcaneapp/arcane/cli/v2/pkg/generate"
	"go.getarcane.app/acfs"
	"go.getarcane.app/acfs/atomic"
	kit "go.getarcane.app/kit/pkg"
	libcrypto "go.getarcane.app/sys/crypto"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

const (
	managerMTLSDirName          = "edge-mtls"
	agentMTLSDirName            = "edge-mtls-agent"
	generatedMTLSContainerDir   = "/app/data/" + agentMTLSDirName
	generatedClientMTLSSubdir   = "clients"
	generatedMTLSCACertFileName = "ca.crt"
	generatedMTLSCAKeyFileName  = "ca.key"
	generatedMTLSClientCertName = "agent.crt"
	generatedMTLSClientKeyName  = "agent.key"
	generatedMTLSEnrolledName   = ".enrolled"
	managerMTLSReenrollCooldown = 15 * time.Minute
	agentMTLSRenewBefore        = 30 * 24 * time.Hour
	maxEnrollResponseBytes      = 1 << 20
	managerCALockTimeout        = 2 * time.Minute
	managerCALockPollInterval   = 100 * time.Millisecond

	// caKeyEncryptedPrefix marks a CA key file holding the libcrypto.Encrypt output of the PEM key.
	caKeyEncryptedPrefix = "ARCANE-ENC-V1:"
)

var (
	generatedAssetNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	managerCALocks              utils.KeyedMutex
	// caKeyEncrypt is a variable so tests can force encryption failures.
	caKeyEncrypt = libcrypto.Encrypt
)

// BuildManagerServerTLSConfig returns the manager TLS configuration needed to
// support optional edge mTLS on the shared Arcane listener.
func BuildManagerServerTLSConfig(cfg *Config) (*tls.Config, error) {
	if cfg == nil || NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled {
		return nil, nil
	}

	caPool := x509.NewCertPool()
	if err := appendCAFile(caPool, cfg.EdgeMTLSCAFile); err != nil {
		return nil, fmt.Errorf("failed to load edge mTLS CA file: %w", err)
	}

	// Even "required" mode only verifies certificates when given: agents enroll before owning one,
	// so identity is enforced per request. TODO: reload ClientCAs when CA rotation support is added.
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  caPool,
	}, nil
}

// NewManagerHTTPClient creates an HTTP client for agent-to-manager requests,
// applying edge TLS settings when the manager URL uses HTTPS.
func NewManagerHTTPClient(cfg *Config, timeout time.Duration) (*http.Client, error) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("http.DefaultTransport is not *http.Transport")
	}
	transport := baseTransport.Clone()
	tlsConfig, err := buildManagerClientTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}
	return &http.Client{Transport: otelhttp.NewTransport(transport), Timeout: max(timeout, 0)}, nil
}

// PrepareManagerMTLSAssetsWithContext generates the manager CA when edge mTLS is
// enabled and no explicit manager CA file is configured.
func PrepareManagerMTLSAssetsWithContext(ctx context.Context, cfg *Config) error {
	if cfg == nil || NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled || strings.TrimSpace(cfg.EdgeMTLSCAFile) != "" {
		return nil
	}

	assetsDir, err := edgeMTLSAssetsDir(cfg, managerMTLSDirName)
	if err != nil {
		return err
	}
	if _, _, caErr := ensureManagerCA(ctx, assetsDir); caErr != nil {
		return caErr
	}

	cfg.EdgeMTLSCAFile = filepath.Join(assetsDir, generatedMTLSCACertFileName)
	return nil
}

// AvailableManagerMTLSCAPath resolves an existing manager CA certificate.
func AvailableManagerMTLSCAPath(cfg *Config) (string, error) {
	if cfg == nil {
		return "", errors.New("edge config is required")
	}
	if NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled {
		return "", errors.New("edge mTLS is disabled")
	}

	caPath := strings.TrimSpace(cfg.EdgeMTLSCAFile)
	if caPath == "" {
		assetsDir, err := edgeMTLSAssetsDir(cfg, managerMTLSDirName)
		if err != nil {
			return "", fmt.Errorf("resolve edge mTLS CA path: %w", err)
		}
		caPath = filepath.Join(assetsDir, generatedMTLSCACertFileName)
	}
	// os rather than acfs: the assets dir may be configured anywhere on the host.
	if _, statErr := os.Stat(caPath); statErr != nil {
		return "", fmt.Errorf("stat edge mTLS CA: %w", statErr)
	}
	return caPath, nil
}

// GenerateManagerClientMTLSAssetsWithContext ensures the generated CA and the environment's client
// certificate, reusing a client certificate that is still valid for the environment's identity.
func GenerateManagerClientMTLSAssetsWithContext(ctx context.Context, cfg *Config, envID, envName string) (*GeneratedMTLSAssets, error) {
	if !usesGeneratedManagerCA(cfg) {
		return nil, nil
	}
	safeEnvID := sanitizedEnvID(envID)
	if safeEnvID == "" {
		return nil, errors.New("environment ID is required")
	}

	assetsDir, err := edgeMTLSAssetsDir(cfg, managerMTLSDirName)
	if err != nil {
		return nil, err
	}
	ca, caGenerated, err := ensureManagerCA(ctx, assetsDir)
	if err != nil {
		return nil, err
	}

	clientLogicalDir := path.Join("/", generatedClientMTLSSubdir, safeEnvID)
	clientDir := filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID)
	if mkdirErr := acfs.MkdirAll(ctx, assetsDir, clientLogicalDir, utils.DirPerm); mkdirErr != nil {
		return nil, fmt.Errorf("failed to create client cert dir: %w", mkdirErr)
	}
	unlock, err := lockEdgeMTLSPath(ctx, clientDir, ".client.lock")
	if err != nil {
		return nil, err
	}
	defer unlock()

	certPath := filepath.Join(clientDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(clientDir, generatedMTLSClientKeyName)
	appURL := cmp.Or(strings.TrimSpace(cfg.AppURL), strings.TrimSpace(httpx.ManagerBaseURL(cfg.ManagerApiUrl)))
	uriSAN := certgen.BuildEdgeMTLSURISAN(appURL, safeEnvID)

	// The URI SAN is the stable identity, so renaming an environment keeps its certificate.
	existing, reason := loadClientCertificate(certPath, keyPath, time.Now())
	certIssued := reason != "" || !supportedGeneratedKey(existing.Leaf.PublicKey) ||
		existing.Leaf.CheckSignatureFrom(ca.Leaf) != nil || (uriSAN != nil && !certificateHasURISAN(existing.Leaf, uriSAN))
	if certIssued {
		// Client keys follow the CA: P-384 under a legacy ECDSA CA, otherwise ML-DSA-87.
		var clientKey crypto.Signer
		if _, ecdsaCA := ca.PrivateKey.(*ecdsa.PrivateKey); ecdsaCA {
			clientKey, err = certgen.GenerateP384PrivateKey()
		} else {
			clientKey, err = certgen.GenerateMLDSA87PrivateKey()
		}
		if err != nil {
			return nil, fmt.Errorf("failed to generate client private key: %w", err)
		}

		// The common name is display metadata: "<name>-<id>", capped at 64 characters.
		safeEnvName := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envName), "-")
		commonName := safeEnvID
		if name := strings.Trim(safeEnvName, "-_"); name != "" && len(safeEnvID) < 63 {
			if name = strings.Trim(name[:min(len(name), 63-len(safeEnvID))], "-_"); name != "" {
				commonName = name + "-" + safeEnvID
			}
		}
		dnsSANs := []string{"arcane-agent"}
		if name := strings.Trim(safeEnvName, "-_."); name != "" && uriSAN != nil {
			dnsSANs = append(dnsSANs, name+".agent."+uriSAN.Host)
		}

		template, templateErr := certgen.NewEdgeMTLSClientTemplate(commonName, uriSAN, dnsSANs)
		if templateErr != nil {
			return nil, templateErr
		}
		certDER, createErr := x509.CreateCertificate(rand.Reader, template, ca.Leaf, clientKey.Public(), ca.PrivateKey)
		if createErr != nil {
			return nil, fmt.Errorf("failed to create client certificate: %w", createErr)
		}
		if writeErr := atomic.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), utils.FilePerm); writeErr != nil {
			return nil, writeErr
		}

		// P-384 keys keep SEC1 framing so agents from before the ML-DSA migration can parse them.
		keyPEM := &pem.Block{Type: "PRIVATE KEY"}
		if ecKey, ok := clientKey.(*ecdsa.PrivateKey); ok {
			keyPEM.Type = "EC PRIVATE KEY"
			keyPEM.Bytes, err = x509.MarshalECPrivateKey(ecKey)
		} else {
			keyPEM.Bytes, err = x509.MarshalPKCS8PrivateKey(clientKey)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to marshal client private key: %w", err)
		}
		if writeErr := atomic.WriteFile(keyPath, pem.EncodeToMemory(keyPEM), 0o600); writeErr != nil {
			return nil, writeErr
		}
	}

	caPEM, err := acfs.ReadFile(ctx, assetsDir, "/"+generatedMTLSCACertFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated CA certificate: %w", err)
	}
	clientCertPEM, err := acfs.ReadFile(ctx, assetsDir, path.Join(clientLogicalDir, generatedMTLSClientCertName))
	if err != nil {
		return nil, fmt.Errorf("failed to read generated client certificate: %w", err)
	}
	clientKeyPEM, err := acfs.ReadFile(ctx, assetsDir, path.Join(clientLogicalDir, generatedMTLSClientKeyName))
	if err != nil {
		return nil, fmt.Errorf("failed to read generated client key: %w", err)
	}

	return &GeneratedMTLSAssets{
		HostDirHint: "./arcane-edge-certs",
		CertIssued:  certIssued,
		CAGenerated: caGenerated,
		Files: []GeneratedMTLSFile{
			{Name: generatedMTLSCACertFileName, Content: string(caPEM), ContainerPath: path.Join(generatedMTLSContainerDir, generatedMTLSCACertFileName), Permissions: "0644"},
			{Name: generatedMTLSClientCertName, Content: string(clientCertPEM), ContainerPath: path.Join(generatedMTLSContainerDir, generatedMTLSClientCertName), Permissions: "0644"},
			{Name: generatedMTLSClientKeyName, Content: string(clientKeyPEM), ContainerPath: path.Join(generatedMTLSContainerDir, generatedMTLSClientKeyName), Permissions: "0600"},
		},
	}, nil
}

// GeneratedManagerClientMTLSCertPath returns the manager-side generated client certificate path for an environment.
func GeneratedManagerClientMTLSCertPath(cfg *Config, envID string) (string, error) {
	assetsDir, err := edgeMTLSAssetsDir(cfg, managerMTLSDirName)
	if err != nil {
		return "", err
	}
	safeEnvID := sanitizedEnvID(envID)
	if safeEnvID == "" {
		return "", errors.New("environment ID is required")
	}
	return filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID, generatedMTLSClientCertName), nil
}

// EnsureAgentMTLSAssets downloads manager-generated client certificates when edge mTLS is
// enabled without configured client cert/key files, and renews them before they expire.
func EnsureAgentMTLSAssets(ctx context.Context, cfg *Config) error {
	if cfg == nil || NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled ||
		(strings.TrimSpace(cfg.EdgeMTLSCertFile) != "" && strings.TrimSpace(cfg.EdgeMTLSKeyFile) != "") {
		return nil
	}

	assetsDir, err := edgeMTLSAssetsDir(cfg, agentMTLSDirName)
	if err != nil {
		return err
	}
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)

	_, reason := loadClientCertificate(certPath, keyPath, time.Now())
	if reason != "" {
		if fileExists(certPath) {
			slog.WarnContext(ctx, "Existing edge mTLS assets need renewal; enrolling new assets", "reason", reason, "certPath", certPath)
		}
		if enrollErr := enrollAgentMTLSAssets(ctx, cfg, assetsDir, certPath, keyPath); enrollErr != nil {
			if NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) != EdgeMTLSModeRequired {
				slog.WarnContext(ctx, "Edge mTLS enrollment failed; proceeding without client certificate", "error", enrollErr)
				return nil
			}
			return enrollErr
		}
	}

	if reason != "" || !fileExists(filepath.Join(assetsDir, generatedMTLSEnrolledName)) {
		marker := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
		if writeErr := acfs.Write(ctx, assetsDir, "/"+generatedMTLSEnrolledName, marker, acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
			return fmt.Errorf("failed to write edge mTLS enrollment marker: %w", writeErr)
		}
	}
	cfg.EdgeMTLSCertFile = certPath
	cfg.EdgeMTLSKeyFile = keyPath
	if caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName); fileExists(caPath) && strings.TrimSpace(cfg.EdgeMTLSCAFile) == "" {
		cfg.EdgeMTLSCAFile = caPath
	}
	return nil
}

// enrollAgentMTLSAssets downloads and writes the manager-issued client assets. It stays separate
// from EnsureAgentMTLSAssets to keep that function under the cognitive-complexity limit.
func enrollAgentMTLSAssets(ctx context.Context, cfg *Config, assetsDir, certPath, keyPath string) error {
	managerBaseURL := strings.TrimRight(strings.TrimSpace(httpx.ManagerBaseURL(cfg.ManagerApiUrl)), "/")
	if managerBaseURL == "" {
		return errors.New("MANAGER_API_URL is required to enroll edge mTLS assets")
	}
	if !managerUsesTLS(cfg) {
		return errors.New("EDGE_MTLS_MODE requires MANAGER_API_URL to use https for certificate enrollment")
	}

	httpClient, clientErr := NewManagerHTTPClient(cfg, 30*time.Second)
	if clientErr != nil {
		return fmt.Errorf("failed to configure edge mTLS enrollment client: %w", clientErr)
	}
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, managerBaseURL+"/api/tunnel/mtls/enroll", http.NoBody)
	if reqErr != nil {
		return fmt.Errorf("failed to create edge mTLS enrollment request: %w", reqErr)
	}
	for header, value := range agentAuthCredentials(cfg.AgentToken) {
		req.Header.Set(header, value)
	}

	resp, doErr := httpClient.Do(req)
	if doErr != nil {
		return fmt.Errorf("edge mTLS enrollment request failed: %w", doErr)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxEnrollResponseBytes))
		return fmt.Errorf("edge mTLS enrollment failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var enrollResp enrollMTLSResponse
	if decodeErr := json.UnmarshalRead(io.LimitReader(resp.Body, maxEnrollResponseBytes), &enrollResp); decodeErr != nil {
		return fmt.Errorf("failed to decode edge mTLS enrollment response: %w", decodeErr)
	}
	if len(enrollResp.Files) == 0 {
		return errors.New("edge mTLS enrollment response did not include any files")
	}

	// The assets dir is the acfs confinement root for the writes below, so os creates it.
	if mkdirErr := os.MkdirAll(assetsDir, utils.DirPerm); mkdirErr != nil {
		return fmt.Errorf("failed to create edge mTLS asset dir: %w", mkdirErr)
	}
	for _, file := range enrollResp.Files {
		perm := kit.Ternary(strings.TrimSpace(file.Permissions) == "0600", 0o600, utils.FilePerm)
		if writeErr := acfs.Write(ctx, assetsDir, "/"+filepath.Base(file.Name), []byte(file.Content), acfs.WriteOptions{Mode: perm}); writeErr != nil {
			return fmt.Errorf("failed to write edge mTLS asset %s: %w", file.Name, writeErr)
		}
	}
	if _, writtenReason := loadClientCertificate(certPath, keyPath, time.Now()); writtenReason != "" {
		return fmt.Errorf("edge mTLS enrollment wrote unusable assets: %s", writtenReason)
	}
	return nil
}

// buildManagerClientTLSConfig returns the agent's TLS config for an https manager URL, adding the
// configured CA to the system roots and the configured client certificate.
func buildManagerClientTLSConfig(cfg *Config) (*tls.Config, error) {
	if cfg == nil || !managerUsesTLS(cfg) {
		return nil, nil
	}
	ctx := context.Background() //nolint:forbidigo // TLS configuration is built at startup without a request context.
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: strings.TrimSpace(cfg.EdgeMTLSServerName)}

	if strings.TrimSpace(cfg.EdgeMTLSCAFile) != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			slog.WarnContext(ctx, "Failed to load system certificate pool; falling back to configured edge mTLS CA only", "error", err)
			pool = x509.NewCertPool()
		}
		if appendErr := appendCAFile(pool, cfg.EdgeMTLSCAFile); appendErr != nil {
			return nil, fmt.Errorf("failed to load edge mTLS CA file: %w", appendErr)
		}
		tlsConfig.RootCAs = pool
	}

	certPath, keyPath := strings.TrimSpace(cfg.EdgeMTLSCertFile), strings.TrimSpace(cfg.EdgeMTLSKeyFile)
	if certPath == "" || keyPath == "" {
		return tlsConfig, nil
	}
	cert, reason := loadClientCertificate(certPath, keyPath, time.Now())
	if reason != "" {
		if NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeOptional {
			slog.WarnContext(ctx, "Ignoring unusable optional edge mTLS client certificate; falling back to token auth", "certPath", certPath, "reason", reason)
			return tlsConfig, nil
		}
		return nil, fmt.Errorf("edge mTLS client certificate is unusable: %s", reason)
	}
	// ML-DSA client certificates require TLS 1.3.
	if _, ok := cert.Leaf.PublicKey.(*mldsa.PublicKey); ok {
		tlsConfig.MinVersion = tls.VersionTLS13
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	return tlsConfig, nil
}

// ValidateAgentMTLSConfig validates the edge agent TLS configuration before the
// reverse tunnel client starts.
func ValidateAgentMTLSConfig(cfg *Config) error {
	if cfg == nil || NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled {
		return nil
	}
	if !managerUsesTLS(cfg) {
		return errors.New("EDGE_MTLS_MODE requires MANAGER_API_URL to use https")
	}
	_, err := buildManagerClientTLSConfig(cfg)
	return err
}

// ValidateManagerMTLSConfig validates a configured manager CA file. An empty CA file is
// allowed because PrepareManagerMTLSAssetsWithContext generates one.
func ValidateManagerMTLSConfig(cfg *Config) error {
	if cfg == nil || strings.TrimSpace(cfg.EdgeMTLSCAFile) == "" {
		return nil
	}
	_, err := BuildManagerServerTLSConfig(cfg)
	return err
}

// appendCAFile adds the PEM certificates in caFile to pool. The path may be configured
// anywhere on the host, so it is read with os rather than acfs.
func appendCAFile(pool *x509.CertPool, caFile string) error {
	caFile = strings.TrimSpace(caFile)
	if caFile == "" {
		return errors.New("CA file is required")
	}
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return errors.New("failed to parse PEM certificates")
	}
	return nil
}

func managerUsesTLS(cfg *Config) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(httpx.ManagerBaseURL(cfg.ManagerApiUrl))), "https://")
}

// usesGeneratedManagerCA reports whether edge mTLS runs on the Arcane-generated CA. The configured
// CA also counts once PrepareManagerMTLSAssetsWithContext has pointed it at the generated file.
func usesGeneratedManagerCA(cfg *Config) bool {
	if cfg == nil || NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled {
		return false
	}
	configuredCA := strings.TrimSpace(cfg.EdgeMTLSCAFile)
	if configuredCA == "" {
		return true
	}
	assetsDir, err := edgeMTLSAssetsDir(cfg, managerMTLSDirName)
	return err == nil && filepath.Clean(configuredCA) == filepath.Join(assetsDir, generatedMTLSCACertFileName)
}

func hasVerifiedPeerCertificate(state *tls.ConnectionState) bool {
	return state != nil && len(state.PeerCertificates) > 0 && len(state.VerifiedChains) > 0
}

// certificateHasURISAN reports whether cert carries expected as a URI SAN, ignoring case and a trailing dot in the host.
func certificateHasURISAN(cert *x509.Certificate, expected *url.URL) bool {
	return slices.ContainsFunc(cert.URIs, func(uri *url.URL) bool {
		return uri != nil && strings.EqualFold(uri.Scheme, expected.Scheme) && uri.Path == expected.Path &&
			strings.EqualFold(strings.TrimSuffix(uri.Host, "."), strings.TrimSuffix(expected.Host, "."))
	})
}

// sanitizedEnvID makes an environment ID safe for asset paths and certificate identities.
func sanitizedEnvID(envID string) string {
	return generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
}

// edgeMTLSAssetsDir returns the configured assets dir, else data/<name> under /app when it exists.
func edgeMTLSAssetsDir(cfg *Config, name string) (string, error) {
	if cfg == nil {
		return "", errors.New("edge config is required")
	}
	if configured := strings.TrimSpace(cfg.EdgeMTLSAssetsDir); configured != "" {
		return configured, nil
	}

	baseDir := filepath.Join("data", name)
	// Probes the fixed system path /app/data, which is not under any acfs root.
	if _, err := os.Stat("/app/data"); err == nil {
		baseDir = "/app/data/" + name
	}
	resolved, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve edge mTLS assets dir: %w", err)
	}
	return resolved, nil
}

// ensureManagerCA returns the generated CA, creating it unless the existing one decrypts,
// matches its key, and uses a supported algorithm.
func ensureManagerCA(ctx context.Context, assetsDir string) (tls.Certificate, bool, error) {
	// The assets dir is the acfs confinement root for callers, so os creates it.
	if err := os.MkdirAll(assetsDir, utils.DirPerm); err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to create edge mTLS assets dir: %w", err)
	}
	unlock, err := lockEdgeMTLSPath(ctx, assetsDir, ".ca.lock")
	if err != nil {
		return tls.Certificate{}, false, err
	}
	defer unlock()

	certPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	keyPath := filepath.Join(assetsDir, generatedMTLSCAKeyFileName)
	// os rather than acfs: the assets dir may be configured anywhere on the host.
	certPEM, certErr := os.ReadFile(certPath)
	rawKey, keyErr := os.ReadFile(keyPath)
	keyPEM := ""
	if ciphertext, encrypted := strings.CutPrefix(strings.TrimSpace(string(rawKey)), caKeyEncryptedPrefix); certErr == nil && keyErr == nil && encrypted {
		keyPEM, _ = libcrypto.Decrypt(ciphertext)
	}
	if ca, pairErr := tls.X509KeyPair(certPEM, []byte(keyPEM)); pairErr == nil && ca.Leaf.IsCA && supportedGeneratedKey(ca.Leaf.PublicKey) {
		return ca, false, nil
	}

	privateKey, err := certgen.GenerateMLDSA87PrivateKey()
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to generate CA private key: %w", err)
	}
	template, err := certgen.NewEdgeMTLSCATemplate()
	if err != nil {
		return tls.Certificate{}, false, err
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.PublicKey(), privateKey)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to create CA certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to parse CA certificate: %w", err)
	}
	if writeErr := atomic.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), utils.FilePerm); writeErr != nil {
		return tls.Certificate{}, false, writeErr
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to marshal CA private key: %w", err)
	}
	ciphertext, err := caKeyEncrypt(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})))
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to encrypt edge mTLS CA private key: %w", err)
	}
	if ciphertext == "" {
		return tls.Certificate{}, false, errors.New("failed to encrypt edge mTLS CA private key: encrypted payload is empty")
	}
	if writeErr := atomic.WriteFile(keyPath, []byte(caKeyEncryptedPrefix+ciphertext), 0o600); writeErr != nil {
		return tls.Certificate{}, false, writeErr
	}

	slog.InfoContext(ctx, "generated edge mTLS CA", "certPath", certPath)
	return tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: privateKey, Leaf: leaf}, true, nil
}

// loadClientCertificate loads a client key pair, or returns why it must be (re)issued: unreadable,
// mismatched, outside its validity window, or within agentMTLSRenewBefore of expiry.
func loadClientCertificate(certPath, keyPath string, now time.Time) (tls.Certificate, string) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	switch {
	case err != nil:
		return tls.Certificate{}, err.Error()
	case now.Before(cert.Leaf.NotBefore):
		return tls.Certificate{}, "certificate is not valid before " + cert.Leaf.NotBefore.UTC().Format(time.RFC3339)
	case !now.Before(cert.Leaf.NotAfter):
		return tls.Certificate{}, "certificate expired at " + cert.Leaf.NotAfter.UTC().Format(time.RFC3339)
	case now.Add(agentMTLSRenewBefore).After(cert.Leaf.NotAfter):
		return tls.Certificate{}, "certificate expires soon at " + cert.Leaf.NotAfter.UTC().Format(time.RFC3339)
	}
	return cert, ""
}

// supportedGeneratedKey reports whether a generated certificate uses ECDSA P-384 or ML-DSA-87.
func supportedGeneratedKey(publicKey crypto.PublicKey) bool {
	switch key := publicKey.(type) {
	case *ecdsa.PublicKey:
		return key.Curve == elliptic.P384()
	case *mldsa.PublicKey:
		return key.Parameters() == mldsa.MLDSA87()
	}
	return false
}

// lockEdgeMTLSPath serializes asset generation in this process and, through an O_EXCL lock
// file (which acfs cannot create), across processes sharing the directory.
func lockEdgeMTLSPath(ctx context.Context, dir, lockName string) (func(), error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve edge mTLS lock dir: %w", err)
	}
	lockPath := filepath.Join(absDir, lockName)

	deadline := time.Now().Add(managerCALockTimeout)
	for {
		if unlock, held := managerCALocks.TryLock(lockPath); held {
			file, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if openErr == nil {
				_, _ = fmt.Fprintf(file, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
				_ = file.Close()
				return func() {
					_ = os.Remove(lockPath)
					unlock()
				}, nil
			}
			expired := time.Now().After(deadline)
			removedStale := os.IsExist(openErr) && expired && removeStaleLock(lockPath)
			unlock()
			switch {
			case !os.IsExist(openErr):
				return nil, fmt.Errorf("failed to acquire edge mTLS CA lock: %w", openErr)
			case removedStale:
				// Retry at once with a fresh deadline now that the stale lock is gone.
				deadline = time.Now().Add(managerCALockTimeout)
				continue
			case expired:
				return nil, fmt.Errorf("timed out waiting for edge mTLS CA lock %s", lockPath)
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cancelled waiting for edge mTLS CA lock: %w", ctx.Err())
		case <-time.After(managerCALockPollInterval):
		}
	}
}

// removeStaleLock removes a lock file whose owning process is gone or that is far older than any holder.
func removeStaleLock(lockPath string) bool {
	content, err := os.ReadFile(lockPath)
	if err != nil {
		return false
	}
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return false
	}

	expired := false
	if len(fields) > 1 {
		if createdAt, parseErr := time.Parse(time.RFC3339Nano, fields[1]); parseErr == nil {
			expired = time.Since(createdAt) > 2*managerCALockTimeout
		}
	}
	if !expired {
		// Signal 0 only probes the process; EPERM means it exists under another user.
		if process, findErr := os.FindProcess(pid); findErr == nil {
			if signalErr := process.Signal(syscall.Signal(0)); signalErr == nil || errors.Is(signalErr, syscall.EPERM) {
				return false
			}
		}
	}

	removeErr := os.Remove(lockPath)
	return removeErr == nil || errors.Is(removeErr, os.ErrNotExist)
}

// fileExists reports whether localPath is a regular file. Paths may be configured anywhere on the host.
func fileExists(localPath string) bool {
	info, err := os.Stat(localPath)
	return err == nil && !info.IsDir()
}
