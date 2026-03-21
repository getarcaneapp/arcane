package edge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

const (
	defaultGeneratedMTLSDir     = "data/edge-mtls"
	defaultAgentMTLSDir         = "data/edge-mtls-agent"
	generatedMTLSContainerDir   = "/certs"
	generatedMTLSCertValidity   = 5 * 365 * 24 * time.Hour
	generatedClientMTLSSubdir   = "clients"
	generatedMTLSCACertFileName = "ca.crt"
	generatedMTLSCAKeyFileName  = "ca.key"
	generatedMTLSClientCertName = "agent.crt"
	generatedMTLSClientKeyName  = "agent.key"
)

var generatedAssetNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// GeneratedMTLSFile describes a generated file that should be copied to the edge agent host.
type GeneratedMTLSFile struct {
	Name          string `json:"name"`
	Content       string `json:"content"`
	ContainerPath string `json:"containerPath"`
	Permissions   string `json:"permissions"`
}

// GeneratedMTLSAssets contains manager-generated edge client certificates and snippet metadata.
type GeneratedMTLSAssets struct {
	Files       []GeneratedMTLSFile `json:"files"`
	HostDirHint string              `json:"hostDirHint"`
}

type enrollMTLSResponse struct {
	Files []GeneratedMTLSFile `json:"files"`
}

// BuildManagerServerTLSConfig returns the manager TLS configuration needed to
// support optional edge mTLS on the shared Arcane listener.
func BuildManagerServerTLSConfig(cfg *Config) (*tls.Config, error) {
	if cfg == nil || NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled {
		return nil, nil
	}

	caPool, err := loadCertPoolInternal(strings.TrimSpace(cfg.EdgeMTLSCAFile))
	if err != nil {
		return nil, fmt.Errorf("failed to load edge mTLS CA file: %w", err)
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  caPool,
	}, nil
}

// NewManagerHTTPClient creates an HTTP client for agent-to-manager requests,
// applying edge TLS settings when the manager URL uses HTTPS.
func NewManagerHTTPClient(cfg *Config, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig, err := buildManagerClientTLSConfigInternal(cfg)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

	client := &http.Client{Transport: transport}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client, nil
}

// ValidateAgentMTLSConfig validates the edge agent TLS configuration before the
// reverse tunnel client starts.
func ValidateAgentMTLSConfig(cfg *Config) error {
	return validateClientMTLSConfigInternal(cfg)
}

// ValidateManagerMTLSConfig validates the manager-side mTLS configuration used
// by edge tunnel endpoints.
func ValidateManagerMTLSConfig(cfg *Config) error {
	return validateServerMTLSConfigInternal(cfg)
}

// PrepareManagerMTLSAssets ensures Arcane-managed edge mTLS assets exist when
// edge mTLS is enabled and no explicit manager CA file is configured.
func PrepareManagerMTLSAssets(cfg *Config) error {
	if !shouldAutoGenerateManagerCAInternal(cfg) {
		return nil
	}

	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return err
	}

	if _, _, err := ensureManagerCAInternal(assetsDir); err != nil {
		return err
	}

	cfg.EdgeMTLSCAFile = filepath.Join(assetsDir, generatedMTLSCACertFileName)
	return nil
}

// GenerateManagerClientMTLSAssets creates or loads the generated CA and per-environment client certificate bundle.
func GenerateManagerClientMTLSAssets(cfg *Config, envID string) (*GeneratedMTLSAssets, error) {
	if !shouldAutoGenerateManagerCAInternal(cfg) {
		return nil, nil
	}
	if strings.TrimSpace(envID) == "" {
		return nil, fmt.Errorf("environment ID is required")
	}

	if err := PrepareManagerMTLSAssets(cfg); err != nil {
		return nil, err
	}

	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return nil, err
	}
	caCertPath, _, err := ensureManagerCAInternal(assetsDir)
	if err != nil {
		return nil, err
	}
	clientCertPath, clientKeyPath, err := ensureClientCertificateInternal(assetsDir, envID)
	if err != nil {
		return nil, err
	}

	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated CA certificate: %w", err)
	}
	clientCertPEM, err := os.ReadFile(clientCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated client certificate: %w", err)
	}
	clientKeyPEM, err := os.ReadFile(clientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated client key: %w", err)
	}

	return &GeneratedMTLSAssets{
		HostDirHint: "./arcane-edge-certs",
		Files: []GeneratedMTLSFile{
			{Name: generatedMTLSCACertFileName, Content: string(caPEM), ContainerPath: filepath.ToSlash(filepath.Join(generatedMTLSContainerDir, generatedMTLSCACertFileName)), Permissions: "0644"},
			{Name: generatedMTLSClientCertName, Content: string(clientCertPEM), ContainerPath: filepath.ToSlash(filepath.Join(generatedMTLSContainerDir, generatedMTLSClientCertName)), Permissions: "0644"},
			{Name: generatedMTLSClientKeyName, Content: string(clientKeyPEM), ContainerPath: filepath.ToSlash(filepath.Join(generatedMTLSContainerDir, generatedMTLSClientKeyName)), Permissions: "0600"},
		},
	}, nil
}

// EnsureAgentMTLSAssets downloads manager-generated client certificates when
// edge mTLS is enabled and explicit client cert/key files are not configured.
func EnsureAgentMTLSAssets(ctx context.Context, cfg *Config) error {
	if !shouldAutoEnrollAgentMTLSInternal(cfg) {
		return nil
	}
	if hasClientCertificateInternal(cfg) {
		return nil
	}

	assetsDir, err := edgeAgentMTLSAssetsDirInternal(cfg)
	if err != nil {
		return err
	}
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	if fileExistsInternal(certPath) && fileExistsInternal(keyPath) {
		setAgentMTLSAssetPathsInternal(cfg, assetsDir)
		return nil
	}

	managerBaseURL := strings.TrimRight(strings.TrimSpace(cfg.GetManagerBaseURL()), "/")
	if managerBaseURL == "" {
		return fmt.Errorf("MANAGER_API_URL is required to enroll edge mTLS assets")
	}
	if !managerUsesTLSInternal(cfg) {
		return fmt.Errorf("EDGE_MTLS_MODE requires MANAGER_API_URL to use https for certificate enrollment")
	}

	httpClient, err := NewManagerHTTPClient(cfg, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to configure edge mTLS enrollment client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, managerBaseURL+"/api/tunnel/mtls/enroll", nil)
	if err != nil {
		return fmt.Errorf("failed to create edge mTLS enrollment request: %w", err)
	}
	req.Header.Set(HeaderAgentToken, cfg.AgentToken)
	req.Header.Set(HeaderAPIKey, cfg.AgentToken)

	resp, err := httpClient.Do(req) //nolint:gosec // intentional request to configured manager endpoint
	if err != nil {
		return fmt.Errorf("edge mTLS enrollment request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("edge mTLS enrollment failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var enrollResp enrollMTLSResponse
	if err := json.NewDecoder(resp.Body).Decode(&enrollResp); err != nil {
		return fmt.Errorf("failed to decode edge mTLS enrollment response: %w", err)
	}
	if len(enrollResp.Files) == 0 {
		return fmt.Errorf("edge mTLS enrollment response did not include any files")
	}

	if err := os.MkdirAll(assetsDir, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create edge mTLS asset dir: %w", err)
	}
	for _, file := range enrollResp.Files {
		targetPath := filepath.Join(assetsDir, filepath.Base(file.Name))
		perm := common.FilePerm
		if strings.TrimSpace(file.Permissions) == "0600" {
			perm = 0o600
		}
		if err := os.WriteFile(targetPath, []byte(file.Content), perm); err != nil {
			return fmt.Errorf("failed to write edge mTLS asset %s: %w", file.Name, err)
		}
	}

	setAgentMTLSAssetPathsInternal(cfg, assetsDir)
	return nil
}

func buildManagerClientTLSConfigInternal(cfg *Config) (*tls.Config, error) {
	if cfg == nil || !managerUsesTLSInternal(cfg) {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if serverName := strings.TrimSpace(cfg.EdgeMTLSServerName); serverName != "" {
		tlsConfig.ServerName = serverName
	}

	if caFile := strings.TrimSpace(cfg.EdgeMTLSCAFile); caFile != "" {
		pool, err := loadSystemOrCustomCertPoolInternal(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load edge mTLS CA file: %w", err)
		}
		tlsConfig.RootCAs = pool
	}

	if hasClientCertificateInternal(cfg) {
		cert, err := tls.LoadX509KeyPair(strings.TrimSpace(cfg.EdgeMTLSCertFile), strings.TrimSpace(cfg.EdgeMTLSKeyFile))
		if err != nil {
			return nil, fmt.Errorf("failed to load edge mTLS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

func validateClientMTLSConfigInternal(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	mode := NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode)
	if mode == EdgeMTLSModeDisabled {
		return nil
	}

	if !managerUsesTLSInternal(cfg) {
		return fmt.Errorf("EDGE_MTLS_MODE requires MANAGER_API_URL to use https")
	}

	if mode == EdgeMTLSModeRequired && !hasClientCertificateInternal(cfg) {
		return fmt.Errorf("EDGE_MTLS_MODE=required requires EDGE_MTLS_CERT_FILE and EDGE_MTLS_KEY_FILE")
	}

	_, err := buildManagerClientTLSConfigInternal(cfg)
	return err
}

func validateServerMTLSConfigInternal(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	mode := NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode)
	if mode == EdgeMTLSModeDisabled {
		return nil
	}

	if strings.TrimSpace(cfg.EdgeMTLSCAFile) == "" {
		return fmt.Errorf("EDGE_MTLS_MODE=%s requires EDGE_MTLS_CA_FILE on the manager", mode)
	}

	_, err := BuildManagerServerTLSConfig(cfg)
	return err
}

func loadCertPoolInternal(caFile string) (*x509.CertPool, error) {
	caFile = strings.TrimSpace(caFile)
	if caFile == "" {
		return nil, fmt.Errorf("CA file is required")
	}

	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("failed to parse PEM certificates")
	}

	return pool, nil
}

func loadSystemOrCustomCertPoolInternal(caFile string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	caFile = strings.TrimSpace(caFile)
	if caFile == "" {
		return pool, nil
	}

	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("failed to parse PEM certificates")
	}
	return pool, nil
}

func hasClientCertificateInternal(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	return strings.TrimSpace(cfg.EdgeMTLSCertFile) != "" && strings.TrimSpace(cfg.EdgeMTLSKeyFile) != ""
}

func managerUsesTLSInternal(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	baseURL := strings.TrimSpace(cfg.GetManagerBaseURL())
	return strings.HasPrefix(strings.ToLower(baseURL), "https://")
}

func shouldAutoGenerateManagerCAInternal(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	return NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) != EdgeMTLSModeDisabled &&
		strings.TrimSpace(cfg.EdgeMTLSCAFile) == ""
}

func shouldAutoEnrollAgentMTLSInternal(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	return NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) != EdgeMTLSModeDisabled &&
		!hasClientCertificateInternal(cfg)
}

func setAgentMTLSAssetPathsInternal(cfg *Config, assetsDir string) {
	if cfg == nil {
		return
	}

	cfg.EdgeMTLSCertFile = filepath.Join(assetsDir, generatedMTLSClientCertName)
	cfg.EdgeMTLSKeyFile = filepath.Join(assetsDir, generatedMTLSClientKeyName)

	caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	if fileExistsInternal(caPath) && strings.TrimSpace(cfg.EdgeMTLSCAFile) == "" {
		cfg.EdgeMTLSCAFile = caPath
	}
}

func requestSecurityModeInternal(req *http.Request) string {
	if req == nil || req.TLS == nil {
		return "token"
	}

	if hasVerifiedPeerCertificateInternal(req.TLS) {
		return "mtls"
	}

	return "token"
}

func grpcContextSecurityModeInternal(pctx peer.Peer) string {
	if tlsInfo, ok := pctx.AuthInfo.(credentials.TLSInfo); ok && hasVerifiedPeerCertificateInternal(&tlsInfo.State) {
		return "mtls"
	}
	return "token"
}

func hasVerifiedPeerCertificateInternal(state *tls.ConnectionState) bool {
	if state == nil {
		return false
	}
	return len(state.PeerCertificates) > 0 && len(state.VerifiedChains) > 0
}

func edgeMTLSAssetsDirInternal(cfg *Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("edge config is required")
	}

	if configured := strings.TrimSpace(cfg.EdgeMTLSAssetsDir); configured != "" {
		return configured, nil
	}

	baseDir := defaultGeneratedMTLSDir
	if _, err := os.Stat("/app/data"); err == nil {
		baseDir = "/app/data/edge-mtls"
	}

	resolved, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve edge mTLS assets dir: %w", err)
	}
	return resolved, nil
}

func edgeAgentMTLSAssetsDirInternal(cfg *Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("edge config is required")
	}
	if configured := strings.TrimSpace(cfg.EdgeMTLSAssetsDir); configured != "" {
		return configured, nil
	}

	baseDir := defaultAgentMTLSDir
	if _, err := os.Stat("/app/data"); err == nil {
		baseDir = "/app/data/edge-mtls-agent"
	}

	resolved, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve edge agent mTLS assets dir: %w", err)
	}
	return resolved, nil
}

func ensureManagerCAInternal(assetsDir string) (string, string, error) {
	if err := os.MkdirAll(assetsDir, common.DirPerm); err != nil {
		return "", "", fmt.Errorf("failed to create edge mTLS assets dir: %w", err)
	}

	caCertPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	caKeyPath := filepath.Join(assetsDir, generatedMTLSCAKeyFileName)
	if fileExistsInternal(caCertPath) && fileExistsInternal(caKeyPath) {
		if err := validateGeneratedCAInternal(caCertPath, caKeyPath); err == nil {
			return caCertPath, caKeyPath, nil
		}
		_ = os.Remove(caCertPath)
		_ = os.Remove(caKeyPath)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate CA private key: %w", err)
	}

	serial, err := randomSerialNumberInternal()
	if err != nil {
		return "", "", err
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Arcane Edge mTLS CA", Organization: []string{"Arcane"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(generatedMTLSCertValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create CA certificate: %w", err)
	}

	if err := writePEMFileInternal(caCertPath, "CERTIFICATE", certDER, common.FilePerm); err != nil {
		return "", "", err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal CA private key: %w", err)
	}
	if err := writePEMFileInternal(caKeyPath, "EC PRIVATE KEY", caKeyDER, 0o600); err != nil {
		return "", "", err
	}

	return caCertPath, caKeyPath, nil
}

func ensureClientCertificateInternal(assetsDir string, envID string) (string, string, error) {
	caCertPath, caKeyPath, err := ensureManagerCAInternal(assetsDir)
	if err != nil {
		return "", "", err
	}

	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read CA certificate: %w", err)
	}
	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read CA private key: %w", err)
	}

	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return "", "", fmt.Errorf("failed to parse CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return "", "", fmt.Errorf("failed to parse CA private key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse CA private key: %w", err)
	}

	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	clientDir := filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID)
	if err := os.MkdirAll(clientDir, common.DirPerm); err != nil {
		return "", "", fmt.Errorf("failed to create client cert dir: %w", err)
	}

	clientCertPath := filepath.Join(clientDir, generatedMTLSClientCertName)
	clientKeyPath := filepath.Join(clientDir, generatedMTLSClientKeyName)
	if fileExistsInternal(clientCertPath) && fileExistsInternal(clientKeyPath) {
		if err := validateGeneratedClientCertificateInternal(clientCertPath, clientKeyPath); err == nil {
			return clientCertPath, clientKeyPath, nil
		}
		_ = os.Remove(clientCertPath)
		_ = os.Remove(clientKeyPath)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate client private key: %w", err)
	}
	serial, err := randomSerialNumberInternal()
	if err != nil {
		return "", "", err
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("arcane-edge-%s", safeEnvID),
			Organization: []string{"Arcane Edge Agents"},
		},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(generatedMTLSCertValidity),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &privateKey.PublicKey, caKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create client certificate: %w", err)
	}

	if err := writePEMFileInternal(clientCertPath, "CERTIFICATE", certDER, common.FilePerm); err != nil {
		return "", "", err
	}
	clientKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal client private key: %w", err)
	}
	if err := writePEMFileInternal(clientKeyPath, "EC PRIVATE KEY", clientKeyDER, 0o600); err != nil {
		return "", "", err
	}

	return clientCertPath, clientKeyPath, nil
}

func randomSerialNumberInternal() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate serial: %w", err)
	}
	return serial, nil
}

func validateGeneratedCAInternal(certPath, keyPath string) error {
	cert, err := readCertificateInternal(certPath)
	if err != nil {
		return err
	}
	if !cert.IsCA {
		return fmt.Errorf("generated CA certificate is not a CA")
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P384() {
		return fmt.Errorf("generated CA certificate is not ECDSA P-384")
	}
	privateKey, err := readECPrivateKeyInternal(keyPath)
	if err != nil {
		return err
	}
	if privateKey.Curve != elliptic.P384() {
		return fmt.Errorf("generated CA private key is not ECDSA P-384")
	}
	if err := validateCertificateKeyPairInternal(cert, privateKey, "generated CA"); err != nil {
		return err
	}
	return nil
}

func validateGeneratedClientCertificateInternal(certPath, keyPath string) error {
	cert, err := readCertificateInternal(certPath)
	if err != nil {
		return err
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P384() {
		return fmt.Errorf("generated client certificate is not ECDSA P-384")
	}
	privateKey, err := readECPrivateKeyInternal(keyPath)
	if err != nil {
		return err
	}
	if privateKey.Curve != elliptic.P384() {
		return fmt.Errorf("generated client private key is not ECDSA P-384")
	}
	if err := validateCertificateKeyPairInternal(cert, privateKey, "generated client"); err != nil {
		return err
	}
	return nil
}

func validateCertificateKeyPairInternal(cert *x509.Certificate, privateKey *ecdsa.PrivateKey, label string) error {
	if cert == nil {
		return fmt.Errorf("%s certificate is required", label)
	}
	if privateKey == nil {
		return fmt.Errorf("%s private key is required", label)
	}

	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%s certificate public key is not ECDSA", label)
	}

	certPublicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal %s certificate public key: %w", label, err)
	}
	privatePublicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal %s private key public key: %w", label, err)
	}
	if string(certPublicKeyDER) != string(privatePublicKeyDER) {
		return fmt.Errorf("%s certificate public key does not match private key", label)
	}

	return nil
}

func readCertificateInternal(path string) (*x509.Certificate, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate %s: %w", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate %s: %w", path, err)
	}
	return cert, nil
}

func readECPrivateKeyInternal(path string) (*ecdsa.PrivateKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key %s: %w", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to parse private key PEM %s", path)
	}
	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EC private key %s: %w", path, err)
	}
	return privateKey, nil
}

func writePEMFileInternal(path string, blockType string, bytes []byte, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("failed to open PEM file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: bytes}); err != nil {
		return fmt.Errorf("failed to write PEM file %s: %w", path, err)
	}
	return nil
}

func fileExistsInternal(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
