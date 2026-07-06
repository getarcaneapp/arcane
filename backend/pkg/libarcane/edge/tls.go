package edge

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	certgen "github.com/getarcaneapp/arcane/cli/v2/pkg/generate"
	"go.getarcane.app/sys/atomic"
	libcrypto "go.getarcane.app/sys/crypto"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

const (
	defaultGeneratedMTLSDir     = "data/edge-mtls"
	defaultAgentMTLSDir         = "data/edge-mtls-agent"
	generatedMTLSContainerDir   = "/app/data/edge-mtls-agent"
	generatedMTLSCertValidity   = 5 * 365 * 24 * time.Hour
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
)

var generatedAssetNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

var managerCALocks sync.Map

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
	CertIssued  bool                `json:"-"`
	CAGenerated bool                `json:"-"`
	Reenrolled  bool                `json:"-"`
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

	// ClientAuth is intentionally VerifyClientCertIfGiven even when
	// EdgeMTLSMode is "required". Enforcement of certificate identity is done
	// per-request at the application layer so that the mTLS enrollment endpoint,
	// which agents must reach before they own a client certificate, remains
	// accessible. In proxy-terminated deployments the TLS state is not visible
	// here; identity is enforced by the upstream proxy.
	// If the handshake were set to RequireAndVerifyClientCert, bootstrap would fail.
	// TODO: reload ClientCAs when CA rotation support is added.
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
		return nil, &common.DefaultTransportTypeError{}
	}
	transport := baseTransport.Clone()
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

// PrepareManagerMTLSAssetsWithContext ensures Arcane-managed edge mTLS assets exist when
// edge mTLS is enabled and no explicit manager CA file is configured.
func PrepareManagerMTLSAssetsWithContext(ctx context.Context, cfg *Config) error {
	if !shouldAutoGenerateManagerCAInternal(cfg) {
		return nil
	}

	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return err
	}

	if _, _, _, err := ensureManagerCAInternal(ctx, assetsDir); err != nil {
		return err
	}

	cfg.EdgeMTLSCAFile = filepath.Join(assetsDir, generatedMTLSCACertFileName)
	return nil
}

// GeneratedManagerMTLSCAPath returns the configured or Arcane-managed manager CA path without creating assets.
func GeneratedManagerMTLSCAPath(cfg *Config) (string, error) {
	if cfg == nil {
		return "", errors.New("edge config is required")
	}
	if configured := strings.TrimSpace(cfg.EdgeMTLSCAFile); configured != "" {
		return configured, nil
	}
	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(assetsDir, generatedMTLSCACertFileName), nil
}

// GenerateManagerClientMTLSAssetsWithContext creates or loads the generated CA and per-environment client certificate bundle.
func GenerateManagerClientMTLSAssetsWithContext(ctx context.Context, cfg *Config, envID string, envName string) (*GeneratedMTLSAssets, error) {
	if !shouldUseGeneratedManagerCAInternal(cfg) {
		return nil, nil
	}
	if strings.TrimSpace(envID) == "" {
		return nil, errors.New("environment ID is required")
	}

	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return nil, err
	}
	caCertPath, _, caGenerated, err := ensureManagerCAInternal(ctx, assetsDir)
	if err != nil {
		return nil, err
	}
	appURL := edgeMTLSAppURLInternal(cfg)
	clientCertPath, clientKeyPath, certIssued, err := ensureClientCertificateInternal(ctx, assetsDir, envID, envName, appURL)
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
		CertIssued:  certIssued,
		CAGenerated: caGenerated,
		Files: []GeneratedMTLSFile{
			{Name: generatedMTLSCACertFileName, Content: string(caPEM), ContainerPath: filepath.ToSlash(filepath.Join(generatedMTLSContainerDir, generatedMTLSCACertFileName)), Permissions: "0644"},
			{Name: generatedMTLSClientCertName, Content: string(clientCertPEM), ContainerPath: filepath.ToSlash(filepath.Join(generatedMTLSContainerDir, generatedMTLSClientCertName)), Permissions: "0644"},
			{Name: generatedMTLSClientKeyName, Content: string(clientKeyPEM), ContainerPath: filepath.ToSlash(filepath.Join(generatedMTLSContainerDir, generatedMTLSClientKeyName)), Permissions: "0600"},
		},
	}, nil
}

func managerMTLSEnrollmentStateInternal(cfg *Config, envID string, now time.Time) (bool, bool, error) {
	markerPath, err := managerMTLSEnrollmentMarkerPathInternal(cfg, envID)
	if err != nil {
		return false, false, err
	}
	enrolledAt, err := readMTLSEnrollmentMarkerInternal(markerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	if enrolledAt.IsZero() {
		return true, false, nil
	}
	return true, now.Sub(enrolledAt) < managerMTLSReenrollCooldown, nil
}

func recordManagerMTLSEnrollmentInternal(cfg *Config, envID string, now time.Time) error {
	markerPath, err := managerMTLSEnrollmentMarkerPathInternal(cfg, envID)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now()
	}
	return atomic.WriteFile(markerPath, []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
}

func managerMTLSEnrollmentMarkerPathInternal(cfg *Config, envID string) (string, error) {
	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return "", err
	}
	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	if safeEnvID == "" {
		return "", errors.New("environment ID is required")
	}
	return filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID, generatedMTLSEnrolledName), nil
}

func readMTLSEnrollmentMarkerInternal(path string) (time.Time, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return time.Time{}, nil
	}
	enrolledAt, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse edge mTLS enrollment marker %s: %w", path, err)
	}
	return enrolledAt, nil
}

// GeneratedManagerClientMTLSCertPath returns the manager-side generated client certificate path for an environment.
func GeneratedManagerClientMTLSCertPath(cfg *Config, envID string) (string, error) {
	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return "", err
	}

	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	if safeEnvID == "" {
		return "", errors.New("environment ID is required")
	}

	return filepath.Join(assetsDir, "clients", safeEnvID, generatedMTLSClientCertName), nil
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
		needsEnrollment, reason := agentMTLSAssetsNeedEnrollmentInternal(certPath, keyPath, time.Now())
		if !needsEnrollment {
			if !fileExistsInternal(filepath.Join(assetsDir, generatedMTLSEnrolledName)) {
				if err := atomic.WriteFile(filepath.Join(assetsDir, generatedMTLSEnrolledName), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
					return fmt.Errorf("failed to write edge mTLS enrollment marker: %w", err)
				}
			}
			setAgentMTLSAssetPathsInternal(cfg, assetsDir)
			return nil
		}
		slog.WarnContext(ctx, "Existing edge mTLS assets need renewal; enrolling new assets", "reason", reason, "cert_path", certPath)
	}

	if err := enrollAgentMTLSAssetsInternal(ctx, cfg, assetsDir, certPath, keyPath); err != nil {
		if NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) != EdgeMTLSModeRequired {
			slog.WarnContext(ctx, "Edge mTLS enrollment failed; proceeding without client certificate", "error", err)
			return nil
		}
		return err
	}
	return nil
}

func enrollAgentMTLSAssetsInternal(ctx context.Context, cfg *Config, assetsDir, certPath, keyPath string) error {
	managerBaseURL := strings.TrimRight(strings.TrimSpace(cfg.GetManagerBaseURL()), "/")
	if managerBaseURL == "" {
		return errors.New("MANAGER_API_URL is required to enroll edge mTLS assets")
	}
	if !managerUsesTLSInternal(cfg) {
		return errors.New("EDGE_MTLS_MODE requires MANAGER_API_URL to use https for certificate enrollment")
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
	req.Header.Set(HeaderAuthorization, "Bearer "+cfg.AgentToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("edge mTLS enrollment request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxEnrollResponseBytes))
		return fmt.Errorf("edge mTLS enrollment failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var enrollResp enrollMTLSResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxEnrollResponseBytes)).Decode(&enrollResp); err != nil {
		return fmt.Errorf("failed to decode edge mTLS enrollment response: %w", err)
	}
	if len(enrollResp.Files) == 0 {
		return errors.New("edge mTLS enrollment response did not include any files")
	}

	if err := os.MkdirAll(assetsDir, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create edge mTLS asset dir: %w", err)
	}
	for _, file := range enrollResp.Files {
		fileName := filepath.Base(file.Name)
		targetPath := filepath.Join(assetsDir, fileName)
		perm := common.FilePerm
		if strings.TrimSpace(file.Permissions) == "0600" {
			perm = 0o600
		}
		if err := atomic.WriteFile(targetPath, []byte(file.Content), perm); err != nil {
			return fmt.Errorf("failed to write edge mTLS asset %s: %w", file.Name, err)
		}
	}
	needsEnrollment, reason := agentMTLSAssetsNeedEnrollmentInternal(certPath, keyPath, time.Now())
	if needsEnrollment {
		return fmt.Errorf("edge mTLS enrollment wrote unusable assets: %s", reason)
	}
	if err := atomic.WriteFile(filepath.Join(assetsDir, generatedMTLSEnrolledName), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write edge mTLS enrollment marker: %w", err)
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
		mode := NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode)
		certPath := strings.TrimSpace(cfg.EdgeMTLSCertFile)
		keyPath := strings.TrimSpace(cfg.EdgeMTLSKeyFile)
		needsEnrollment, reason := agentMTLSAssetsNeedEnrollmentInternal(certPath, keyPath, time.Now())
		if needsEnrollment {
			err := fmt.Errorf("edge mTLS client certificate is unusable: %s", reason)
			if mode == EdgeMTLSModeOptional {
				slog.Warn("Ignoring unusable optional edge mTLS client certificate; falling back to token auth", "cert_path", certPath, "error", err.Error())
				return tlsConfig, nil
			}
			return nil, err
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			if mode == EdgeMTLSModeOptional {
				slog.Warn("Failed to load optional edge mTLS client certificate; falling back to token auth", "cert_path", certPath, "error", err.Error())
				return tlsConfig, nil
			}
			return nil, fmt.Errorf("failed to load edge mTLS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// ValidateAgentMTLSConfig validates the edge agent TLS configuration before the
// reverse tunnel client starts.
func ValidateAgentMTLSConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	mode := NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode)
	if mode == EdgeMTLSModeDisabled {
		return nil
	}

	if !managerUsesTLSInternal(cfg) {
		return errors.New("EDGE_MTLS_MODE requires MANAGER_API_URL to use https")
	}

	_, err := buildManagerClientTLSConfigInternal(cfg)
	return err
}

// ValidateManagerMTLSConfig validates the manager-side mTLS configuration used
// by edge tunnel endpoints.
func ValidateManagerMTLSConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	mode := NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode)
	if mode == EdgeMTLSModeDisabled {
		return nil
	}

	if strings.TrimSpace(cfg.EdgeMTLSCAFile) == "" {
		return nil
	}

	_, err := loadCertPoolInternal(cfg.EdgeMTLSCAFile)
	if err != nil {
		return fmt.Errorf("failed to load edge mTLS CA file: %w", err)
	}
	return nil
}

func loadCertPoolInternal(caFile string) (*x509.CertPool, error) {
	caFile = strings.TrimSpace(caFile)
	if caFile == "" {
		return nil, errors.New("CA file is required")
	}

	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("failed to parse PEM certificates")
	}

	return pool, nil
}

func loadSystemOrCustomCertPoolInternal(caFile string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		slog.Warn("Failed to load system certificate pool; falling back to configured edge mTLS CA only", "error", err)
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
		return nil, errors.New("failed to parse PEM certificates")
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

func shouldUseGeneratedManagerCAInternal(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	if NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == EdgeMTLSModeDisabled {
		return false
	}
	configuredCA := strings.TrimSpace(cfg.EdgeMTLSCAFile)
	if configuredCA == "" {
		return true
	}
	assetsDir, err := edgeMTLSAssetsDirInternal(cfg)
	if err != nil {
		return false
	}
	generatedCA := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	return filepath.Clean(configuredCA) == filepath.Clean(generatedCA)
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
	if hasVerifiedEdgeMTLSRequestInternal(req) {
		return "mtls"
	}
	return "token"
}

func hasVerifiedEdgeMTLSRequestInternal(req *http.Request) bool {
	if req == nil {
		return false
	}
	return req.TLS != nil && hasVerifiedPeerCertificateInternal(req.TLS)
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

func verifiedPeerCertificateEnvironmentIDMatchesInternal(state *tls.ConnectionState, envID string, trustDomain string) error {
	if !hasVerifiedPeerCertificateInternal(state) {
		return nil
	}
	expectedPath := expectedEdgeMTLSURIPathInternal(envID)
	if expectedPath == "" {
		return errors.New("environment ID is required for edge mTLS certificate identity check")
	}
	trustDomain = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(trustDomain, ".")))
	if trustDomain == "" {
		return errors.New("edge mTLS trust domain is required for certificate identity check")
	}
	leaf := state.VerifiedChains[0][0]
	for _, uri := range leaf.URIs {
		if uri == nil {
			continue
		}
		if uri.Scheme == "spiffe" && strings.EqualFold(strings.TrimSuffix(uri.Host, "."), trustDomain) && uri.Path == expectedPath {
			return nil
		}
	}
	return fmt.Errorf("verified edge mTLS client certificate does not match environment %s", strings.TrimSpace(envID))
}

func edgeMTLSAppURLInternal(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	if appURL := strings.TrimSpace(cfg.AppURL); appURL != "" {
		return appURL
	}
	return strings.TrimSpace(cfg.GetManagerBaseURL())
}

func edgeMTLSTrustDomainInternal(cfg *Config) string {
	return certgen.EdgeMTLSTrustDomain(edgeMTLSAppURLInternal(cfg))
}

func expectedEdgeMTLSURIPathInternal(envID string) string {
	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	if safeEnvID == "" {
		return ""
	}
	return "/edge/" + safeEnvID
}

func edgeMTLSAssetsDirInternal(cfg *Config) (string, error) {
	if cfg == nil {
		return "", errors.New("edge config is required")
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
		return "", errors.New("edge config is required")
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

func ensureManagerCAInternal(ctx context.Context, assetsDir string) (string, string, bool, error) {
	if err := os.MkdirAll(assetsDir, common.DirPerm); err != nil {
		return "", "", false, fmt.Errorf("failed to create edge mTLS assets dir: %w", err)
	}

	unlock, err := lockEdgeMTLSPathInternal(ctx, assetsDir, ".ca.lock")
	if err != nil {
		return "", "", false, err
	}
	defer unlock()

	caCertPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	caKeyPath := filepath.Join(assetsDir, generatedMTLSCAKeyFileName)
	if generatedCAReadyInternal(caCertPath, caKeyPath) {
		return caCertPath, caKeyPath, false, nil
	}
	_ = os.Remove(caCertPath)
	_ = os.Remove(caKeyPath)

	privateKey, err := certgen.GenerateP384PrivateKey()
	if err != nil {
		return "", "", false, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	template, err := certgen.NewEdgeMTLSCATemplate()
	if err != nil {
		return "", "", false, err
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	if err := writePEMFileInternal(caCertPath, "CERTIFICATE", certDER, common.FilePerm); err != nil {
		return "", "", false, err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to marshal CA private key: %w", err)
	}
	if err := writeCAKeyFileInternal(caKeyPath, caKeyDER); err != nil {
		return "", "", false, err
	}

	slog.Info("generated edge mTLS CA", "cert_path", caCertPath)
	return caCertPath, caKeyPath, true, nil
}

func generatedCAReadyInternal(caCertPath string, caKeyPath string) bool {
	if !fileExistsInternal(caCertPath) || !fileExistsInternal(caKeyPath) {
		return false
	}
	if err := validateGeneratedCAInternal(caCertPath, caKeyPath); err != nil {
		return false
	}
	return true
}

func ensureClientCertificateInternal(ctx context.Context, assetsDir string, envID string, envName string, appURL string) (string, string, bool, error) {
	caCertPath, caKeyPath, _, err := ensureManagerCAInternal(ctx, assetsDir)
	if err != nil {
		return "", "", false, err
	}

	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to read CA certificate: %w", err)
	}
	caKeyPEM, err := readCAKeyPEMInternal(caKeyPath)
	if err != nil {
		return "", "", false, err
	}

	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return "", "", false, errors.New("failed to parse CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return "", "", false, errors.New("failed to parse CA private key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to parse CA private key: %w", err)
	}

	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	clientDir := filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID)
	if err := os.MkdirAll(clientDir, common.DirPerm); err != nil {
		return "", "", false, fmt.Errorf("failed to create client cert dir: %w", err)
	}
	unlock, err := lockEdgeMTLSPathInternal(ctx, clientDir, ".client.lock")
	if err != nil {
		return "", "", false, err
	}
	defer unlock()

	clientCertPath := filepath.Join(clientDir, generatedMTLSClientCertName)
	clientKeyPath := filepath.Join(clientDir, generatedMTLSClientKeyName)
	expectedCommonName := buildGeneratedClientCommonNameInternal(envName, safeEnvID)
	expectedURISAN := certgen.BuildEdgeMTLSURISAN(appURL, safeEnvID)
	if fileExistsInternal(clientCertPath) && fileExistsInternal(clientKeyPath) {
		// The URI SAN is the stable edge identity. Common Name is display metadata
		// for newly issued certs only, so environment renames must not rotate keys.
		if err := validateGeneratedClientCertificateInternal(clientCertPath, clientKeyPath, "", expectedURISAN); err == nil {
			return clientCertPath, clientKeyPath, false, nil
		}
		_ = os.Remove(clientCertPath)
		_ = os.Remove(clientKeyPath)
	}

	privateKey, err := certgen.GenerateP384PrivateKey()
	if err != nil {
		return "", "", false, fmt.Errorf("failed to generate client private key: %w", err)
	}
	uriSAN, dnsSANs := buildGeneratedClientSANsInternal(envName, safeEnvID, appURL)
	template, err := certgen.NewEdgeMTLSClientTemplate(expectedCommonName, uriSAN, dnsSANs)
	if err != nil {
		return "", "", false, err
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &privateKey.PublicKey, caKey)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to create client certificate: %w", err)
	}

	if err := writePEMFileInternal(clientCertPath, "CERTIFICATE", certDER, common.FilePerm); err != nil {
		return "", "", false, err
	}
	clientKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to marshal client private key: %w", err)
	}
	if err := writePEMFileInternal(clientKeyPath, "EC PRIVATE KEY", clientKeyDER, 0o600); err != nil {
		return "", "", false, err
	}

	return clientCertPath, clientKeyPath, true, nil
}

func buildGeneratedClientCommonNameInternal(envName string, safeEnvID string) string {
	safeEnvID = strings.TrimSpace(safeEnvID)
	if safeEnvID == "" {
		return ""
	}

	safeEnvName := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envName), "-")
	safeEnvName = strings.Trim(safeEnvName, "-_")
	if safeEnvName == "" {
		return safeEnvID
	}

	const maxCommonNameLength = 64

	maxEnvNameLength := maxCommonNameLength - len(safeEnvID) - 1
	if maxEnvNameLength <= 0 {
		return safeEnvID
	}
	if len(safeEnvName) > maxEnvNameLength {
		safeEnvName = strings.Trim(safeEnvName[:maxEnvNameLength], "-_")
		if safeEnvName == "" {
			return safeEnvID
		}
	}

	return fmt.Sprintf("%s-%s", safeEnvName, safeEnvID)
}

// buildGeneratedClientSANsInternal returns the URI and DNS Subject Alternative
// Names to embed in a generated edge agent client certificate. The URI SAN
// provides a stable machine-readable identity; DNS SANs improve interop with
// stricter verifiers. Returns a nil URI if safeEnvID is empty.
func buildGeneratedClientSANsInternal(envName string, safeEnvID string, appURL string) (*url.URL, []string) {
	safeEnvID = strings.TrimSpace(safeEnvID)
	if safeEnvID == "" {
		return nil, nil
	}

	uriSAN := certgen.BuildEdgeMTLSURISAN(appURL, safeEnvID)
	trustDomain := certgen.EdgeMTLSTrustDomain(appURL)

	dnsSANs := []string{"arcane-agent"}
	safeEnvName := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envName), "-")
	safeEnvName = strings.Trim(safeEnvName, "-_.")
	if safeEnvName != "" && trustDomain != "" {
		dnsSANs = append(dnsSANs, safeEnvName+".agent."+trustDomain)
	}

	return uriSAN, dnsSANs
}

func validateGeneratedCAInternal(certPath, keyPath string) error {
	cert, err := readCertificateInternal(certPath)
	if err != nil {
		return err
	}
	if !cert.IsCA {
		return errors.New("generated CA certificate is not a CA")
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P384() {
		return errors.New("generated CA certificate is not ECDSA P-384")
	}
	keyPEM, err := readCAKeyPEMInternal(keyPath)
	if err != nil {
		return err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to parse CA private key PEM %s", keyPath)
	}
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA private key %s: %w", keyPath, err)
	}
	if privateKey.Curve != elliptic.P384() {
		return errors.New("generated CA private key is not ECDSA P-384")
	}
	if err := validateCertificateKeyPairInternal(cert, privateKey, "generated CA"); err != nil {
		return err
	}
	return nil
}

func validateGeneratedClientCertificateInternal(certPath, keyPath string, expectedCommonName string, expectedURISAN *url.URL) error {
	cert, err := readCertificateInternal(certPath)
	if err != nil {
		return err
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return errors.New("generated client certificate is not currently valid")
	}
	if strings.TrimSpace(expectedCommonName) != "" && cert.Subject.CommonName != expectedCommonName {
		return fmt.Errorf("generated client certificate common name %q does not match expected %q", cert.Subject.CommonName, expectedCommonName)
	}
	if expectedURISAN != nil && !certificateHasURISANInternal(cert, expectedURISAN) {
		return fmt.Errorf("generated client certificate URI SAN does not match expected %s", expectedURISAN.String())
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P384() {
		return errors.New("generated client certificate is not ECDSA P-384")
	}
	privateKey, err := readECPrivateKeyInternal(keyPath)
	if err != nil {
		return err
	}
	if privateKey.Curve != elliptic.P384() {
		return errors.New("generated client private key is not ECDSA P-384")
	}
	if err := validateCertificateKeyPairInternal(cert, privateKey, "generated client"); err != nil {
		return err
	}
	return nil
}

func certificateHasURISANInternal(cert *x509.Certificate, expected *url.URL) bool {
	if cert == nil || expected == nil {
		return false
	}
	for _, uri := range cert.URIs {
		if uri == nil {
			continue
		}
		if strings.EqualFold(uri.Scheme, expected.Scheme) &&
			strings.EqualFold(strings.TrimSuffix(uri.Host, "."), strings.TrimSuffix(expected.Host, ".")) &&
			uri.Path == expected.Path {
			return true
		}
	}
	return false
}

func agentMTLSAssetsNeedEnrollmentInternal(certPath string, keyPath string, now time.Time) (bool, string) {
	if !fileExistsInternal(certPath) || !fileExistsInternal(keyPath) {
		return true, "certificate or key is missing"
	}
	cert, err := readCertificateInternal(certPath)
	if err != nil {
		return true, err.Error()
	}
	privateKey, err := readECPrivateKeyInternal(keyPath)
	if err != nil {
		return true, err.Error()
	}
	if err := validateCertificateKeyPairInternal(cert, privateKey, "edge mTLS client"); err != nil {
		return true, err.Error()
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.Before(cert.NotBefore) {
		return true, "certificate is not valid before " + cert.NotBefore.UTC().Format(time.RFC3339)
	}
	if !now.Before(cert.NotAfter) {
		return true, "certificate expired at " + cert.NotAfter.UTC().Format(time.RFC3339)
	}
	if now.Add(agentMTLSRenewBefore).After(cert.NotAfter) {
		return true, "certificate expires soon at " + cert.NotAfter.UTC().Format(time.RFC3339)
	}
	return false, ""
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
	if !bytes.Equal(certPublicKeyDER, privatePublicKeyDER) {
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

func lockEdgeMTLSPathInternal(ctx context.Context, dir string, lockName string) (func(), error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve edge mTLS lock dir: %w", err)
	}
	lockName = strings.TrimSpace(lockName)
	if lockName == "" {
		lockName = ".lock"
	}

	lockPath := filepath.Join(absDir, lockName)
	muValue, _ := managerCALocks.LoadOrStore(lockPath, &sync.Mutex{})
	mu, ok := muValue.(*sync.Mutex)
	if !ok {
		return nil, &common.ManagerCALockTypeError{}
	}

	deadline := time.Now().Add(managerCALockTimeout)
	for {
		if !mu.TryLock() {
			if err := waitForEdgeMTLSLockPollInternal(ctx); err != nil {
				return nil, err
			}
			continue
		}
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = file.Close()
			return func() {
				_ = os.Remove(lockPath)
				mu.Unlock()
			}, nil
		}
		if !os.IsExist(err) {
			mu.Unlock()
			return nil, fmt.Errorf("failed to acquire edge mTLS CA lock: %w", err)
		}
		if time.Now().After(deadline) {
			if removeStaleEdgeMTLSLockInternal(lockPath) {
				deadline = time.Now().Add(managerCALockTimeout)
				continue
			}
			mu.Unlock()
			return nil, fmt.Errorf("timed out waiting for edge mTLS CA lock %s", lockPath)
		}
		mu.Unlock()
		if err := waitForEdgeMTLSLockPollInternal(ctx); err != nil {
			return nil, err
		}
	}
}

func waitForEdgeMTLSLockPollInternal(ctx context.Context) error {
	timer := time.NewTimer(managerCALockPollInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("cancelled waiting for edge mTLS CA lock: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func removeStaleEdgeMTLSLockInternal(lockPath string) bool {
	info, err := readEdgeMTLSLockInfoInternal(lockPath)
	if err != nil {
		return false
	}
	if !info.createdAt.IsZero() && time.Since(info.createdAt) > 2*managerCALockTimeout {
		return removeEdgeMTLSLockFileInternal(lockPath)
	}
	if edgeMTLSLockPIDAliveInternal(info.pid) {
		return false
	}
	return removeEdgeMTLSLockFileInternal(lockPath)
}

type edgeMTLSLockInfo struct {
	pid       int
	createdAt time.Time
}

func readEdgeMTLSLockInfoInternal(lockPath string) (*edgeMTLSLockInfo, error) {
	content, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return nil, errors.New("edge mTLS lock does not contain a PID")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return nil, fmt.Errorf("parse edge mTLS lock PID: %w", err)
	}
	if pid <= 0 {
		return nil, errors.New("edge mTLS lock PID must be positive")
	}
	info := &edgeMTLSLockInfo{pid: pid}
	if len(fields) > 1 {
		createdAt, parseErr := time.Parse(time.RFC3339Nano, fields[1])
		if parseErr == nil {
			info.createdAt = createdAt
		}
	}
	return info, nil
}

func removeEdgeMTLSLockFileInternal(lockPath string) bool {
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

func edgeMTLSLockPIDAliveInternal(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func writePEMFileInternal(path string, blockType string, bytes []byte, perm os.FileMode) error {
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: bytes})
	if pemBytes == nil {
		return fmt.Errorf("failed to encode PEM file %s", path)
	}
	return atomic.WriteFile(path, pemBytes, perm)
}

// caKeyEncryptedPrefix marks files written with libcrypto envelope encryption.
// The payload after the prefix is the base64 ciphertext returned by
// libcrypto.Encrypt of the plain PEM-encoded CA private key.
const caKeyEncryptedPrefix = "ARCANE-ENC-V1:"

var caKeyEncryptInternal = libcrypto.Encrypt

// writeCAKeyFileInternal writes the edge CA private key to disk using envelope
// encryption via libcrypto.
func writeCAKeyFileInternal(path string, derBytes []byte) error {
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: derBytes})
	if pemBytes == nil {
		return errors.New("failed to encode CA private key to PEM")
	}

	ciphertext, err := caKeyEncryptInternal(string(pemBytes))
	if err != nil {
		return fmt.Errorf("failed to encrypt edge mTLS CA private key: %w", err)
	}
	if ciphertext == "" {
		return errors.New("failed to encrypt edge mTLS CA private key: encrypted payload is empty")
	}

	return atomic.WriteFile(path, []byte(caKeyEncryptedPrefix+ciphertext), 0o600)
}

// readCAKeyPEMInternal returns the plain PEM bytes of the edge CA private key,
// reading a libcrypto-envelope-encrypted file written by writeCAKeyFileInternal.
func readCAKeyPEMInternal(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA private key %s: %w", path, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, caKeyEncryptedPrefix) {
		return nil, fmt.Errorf("CA private key %s is not in the expected encrypted envelope format", path)
	}
	ciphertext := strings.TrimPrefix(trimmed, caKeyEncryptedPrefix)
	plaintext, err := libcrypto.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt CA private key %s: %w", path, err)
	}
	return []byte(plaintext), nil
}

func fileExistsInternal(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
