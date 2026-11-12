package agent

import (
	"bytes"
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

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/acfs/types"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/fs"
	"kit.getarcane.app/fs/atomic"
	libcrypto "kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/pkg/httpx"
	certgen "getarcane.dev/cli/v3/pkg/generate"
)

const (
	managerMTLSDirName          = "agent-mtls"
	agentMTLSDirName            = "agent-mtls-client"
	generatedMTLSContainerDir   = "/app/data/" + agentMTLSDirName
	generatedClientMTLSSubdir   = "clients"
	generatedMTLSCACertFileName = "ca.crt"
	generatedMTLSCAKeyFileName  = "ca.key"
	generatedMTLSClientCertName = "agent.crt"
	generatedMTLSClientKeyName  = "agent.key"
	generatedMTLSPendingKeyName = "agent.key.pending"
	generatedMTLSEnrolledName   = ".enrolled"
	managerMTLSReenrollCooldown = 15 * time.Minute
	agentMTLSRenewBefore        = 30 * 24 * time.Hour
	maxEnrollResponseBytes      = 1 << 20
	managerCALockTimeout        = 2 * time.Minute
	managerCALockPollInterval   = 100 * time.Millisecond

	caKeyEncryptedPrefix = "ARCANE-ENC-V1:"
)

var (
	generatedAssetNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	managerCALocks              concurrency.KeyedMutex
	errEnrollUnreachable        = errors.New("agent mTLS enrollment request failed")
	ErrMTLSEnrollUnavailable    = errors.New("agent mTLS enrollment is not available")
	ErrMTLSEnrollCooldown       = errors.New("agent mTLS enrollment was recently completed; retry later")
	ErrMTLSEnrollInvalidCSR     = errors.New("agent mTLS enrollment requires a valid certificate signing request")
	caKeyEncrypt                = libcrypto.Encrypt
)

func NewMTLS(cfg MTLSConfig) *MTLS {
	return &MTLS{
		Mode:       NormalizeMTLSMode(cfg.Mode),
		CAFile:     strings.TrimSpace(cfg.CAFile),
		CertFile:   strings.TrimSpace(cfg.CertFile),
		KeyFile:    strings.TrimSpace(cfg.KeyFile),
		ServerName: strings.TrimSpace(cfg.ServerName),
		AssetsDir:  strings.TrimSpace(cfg.AssetsDir),
		AppURL:     cfg.AppURL,
		ManagerURL: cfg.ManagerURL,
	}
}

func (m *MTLS) Server(ctx context.Context) (*tls.Config, error) {
	if m.Mode == MTLSModeDisabled {
		return nil, nil
	}
	if m.CAFile == "" {
		assetsDir, err := m.assetsDir(ctx, managerMTLSDirName)
		if err != nil {
			return nil, err
		}
		if _, _, caErr := ensureManagerCA(ctx, assetsDir); caErr != nil {
			return nil, caErr
		}
		m.MTLSConfig = MTLSConfig{
			Mode:       m.Mode,
			CAFile:     filepath.Join(assetsDir, generatedMTLSCACertFileName),
			CertFile:   m.CertFile,
			KeyFile:    m.KeyFile,
			ServerName: m.ServerName,
			AssetsDir:  m.AssetsDir,
			AppURL:     m.AppURL,
			ManagerURL: m.ManagerURL,
		}
	}

	caPool := x509.NewCertPool()
	if err := appendCAFile(ctx, caPool, m.CAFile); err != nil {
		return nil, fmt.Errorf("failed to load agent mTLS CA file: %w", err)
	}
	// Agents enroll before owning a certificate, so even "required" mode verifies only given certificates.
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  caPool,
	}, nil
}

func (m *MTLS) CAPath(ctx context.Context) (string, error) {
	if m.Mode == MTLSModeDisabled {
		return "", errors.New("agent mTLS is disabled")
	}

	caPath := m.CAFile
	if caPath == "" {
		assetsDir, err := m.assetsDir(ctx, managerMTLSDirName)
		if err != nil {
			return "", fmt.Errorf("resolve agent mTLS CA path: %w", err)
		}
		caPath = filepath.Join(assetsDir, generatedMTLSCACertFileName)
	}
	caAbs, err := filepath.Abs(caPath)
	if err != nil {
		return "", fmt.Errorf("resolve agent mTLS CA path: %w", err)
	}
	if _, statErr := (acfs.Host{}).Stat(ctx, caAbs, types.StatOptions{FollowFinal: true}); statErr != nil {
		return "", fmt.Errorf("stat agent mTLS CA: %w", statErr)
	}
	return caPath, nil
}

func (m *MTLS) IssueClient(ctx context.Context, opts IssueClientOptions) (*GeneratedMTLSAssets, error) {
	if !m.UsesGeneratedCA(ctx) {
		return nil, nil
	}
	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(opts.EnvironmentID), "_")
	if safeEnvID == "" {
		return nil, errors.New("environment ID is required")
	}
	var csr *x509.CertificateRequest
	if opts.CSR != "" {
		parsed, csrErr := parseEnrollmentCSR(opts.CSR)
		if csrErr != nil {
			return nil, csrErr
		}
		csr = parsed
	}

	assetsDir, err := m.assetsDir(ctx, managerMTLSDirName)
	if err != nil {
		return nil, err
	}
	ca, caGenerated, err := ensureManagerCA(ctx, assetsDir)
	if err != nil {
		return nil, err
	}

	clientLogicalDir := path.Join("/", generatedClientMTLSSubdir, safeEnvID)
	clientDir := filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID)
	if mkdirErr := (acfs.Root{Path: assetsDir}).MkdirAll(ctx, clientLogicalDir, fs.DirPerm); mkdirErr != nil {
		return nil, fmt.Errorf("failed to create client cert dir: %w", mkdirErr)
	}
	unlock, err := lockAgentMTLSPath(ctx, clientDir, ".client.lock")
	if err != nil {
		return nil, err
	}
	defer unlock()

	if removeErr := (acfs.Root{Path: assetsDir}).RemoveAll(ctx, path.Join(clientLogicalDir, generatedMTLSClientKeyName)); removeErr != nil {
		return nil, fmt.Errorf("failed to remove legacy client private key: %w", removeErr)
	}

	appURL := cmp.Or(strings.TrimSpace(m.AppURL), strings.TrimSpace(opts.FallbackAppURL), strings.TrimSpace(httpx.ManagerBaseURL(m.ManagerURL)))
	uriSAN := certgen.BuildAgentMTLSURISAN(appURL, safeEnvID)

	// The URI SAN is the stable identity, so renaming an environment keeps its certificate.
	stored, storedErr := m.ClientCertificate(ctx, safeEnvID)
	certIssued := csr != nil && (storedErr != nil ||
		!bytes.Equal(stored.RawSubjectPublicKeyInfo, csr.RawSubjectPublicKeyInfo) ||
		!time.Now().Add(agentMTLSRenewBefore).Before(stored.NotAfter) ||
		stored.CheckSignatureFrom(ca.Leaf) != nil || (uriSAN != nil && !certificateHasURISAN(stored, uriSAN)))
	if certIssued {
		// The common name is display metadata: "<name>-<id>", capped at 64 characters.
		safeEnvName := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(opts.EnvironmentName), "-")
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

		template, templateErr := certgen.NewAgentMTLSClientTemplate(commonName, uriSAN, dnsSANs)
		if templateErr != nil {
			return nil, templateErr
		}
		certDER, createErr := x509.CreateCertificate(rand.Reader, template, ca.Leaf, csr.PublicKey, ca.PrivateKey)
		if createErr != nil {
			return nil, fmt.Errorf("failed to create client certificate: %w", createErr)
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		if writeErr := atomic.WriteFile(filepath.Join(clientDir, generatedMTLSClientCertName), certPEM, fs.FilePerm); writeErr != nil {
			return nil, writeErr
		}
	}

	caPEM, err := acfs.Root{Path: assetsDir}.ReadFile(ctx, "/"+generatedMTLSCACertFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated CA certificate: %w", err)
	}
	files := []GeneratedMTLSFile{
		{Name: generatedMTLSCACertFileName, Content: string(caPEM), ContainerPath: path.Join(generatedMTLSContainerDir, generatedMTLSCACertFileName), Permissions: "0644"},
	}
	clientCertPEM, err := acfs.Root{Path: assetsDir}.ReadFile(ctx, path.Join(clientLogicalDir, generatedMTLSClientCertName))
	switch {
	case err == nil:
		files = append(files, GeneratedMTLSFile{
			Name:          generatedMTLSClientCertName,
			Content:       string(clientCertPEM),
			ContainerPath: path.Join(generatedMTLSContainerDir, generatedMTLSClientCertName),
			Permissions:   "0644",
		})
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("failed to read generated client certificate: %w", err)
	}

	return &GeneratedMTLSAssets{
		HostDirHint: "./arcane-agent-certs",
		CertIssued:  certIssued,
		CAGenerated: caGenerated,
		Files:       files,
	}, nil
}

func (m *MTLS) IssueEnrollment(ctx context.Context, opts IssueClientOptions) (*MTLSEnrollment, error) {
	csr, err := parseEnrollmentCSR(opts.CSR)
	if err != nil {
		return nil, err
	}
	assetsDir, err := m.assetsDir(ctx, managerMTLSDirName)
	if err != nil {
		return nil, err
	}
	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(opts.EnvironmentID), "_")
	if safeEnvID == "" {
		return nil, errors.New("environment ID is required")
	}
	markerPath := filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID, generatedMTLSEnrolledName)
	marker, readErr := acfs.Root{Path: assetsDir}.ReadFile(ctx, path.Join("/", generatedClientMTLSSubdir, safeEnvID, generatedMTLSEnrolledName))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read agent mTLS enrollment marker: %w", readErr)
	}
	now := time.Now()
	enrolledMarker := strings.TrimSpace(string(marker))
	enrolledAt, parseErr := time.Parse(time.RFC3339Nano, enrolledMarker)
	if enrolledMarker != "" && parseErr != nil {
		return nil, fmt.Errorf("failed to parse agent mTLS enrollment marker %s: %w", markerPath, parseErr)
	}

	if enrolledMarker != "" && now.Sub(enrolledAt) < managerMTLSReenrollCooldown {
		current, currentErr := m.ClientCertificate(ctx, safeEnvID)
		if currentErr != nil || !bytes.Equal(current.RawSubjectPublicKeyInfo, csr.RawSubjectPublicKeyInfo) {
			return nil, ErrMTLSEnrollCooldown
		}
		cached, cacheErr := m.IssueClient(ctx, opts)
		if cacheErr != nil || cached == nil {
			return nil, errors.Join(ErrMTLSEnrollCooldown, cacheErr)
		}
		return &MTLSEnrollment{Assets: cached, Cached: true}, nil
	}

	assets, err := m.IssueClient(ctx, opts)
	switch {
	case err != nil:
		return nil, fmt.Errorf("failed to generate agent mTLS assets: %w", err)
	case assets == nil:
		return nil, ErrMTLSEnrollUnavailable
	}
	if writeErr := atomic.WriteFile(markerPath, []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0o600); writeErr != nil {
		return nil, fmt.Errorf("failed to record agent mTLS enrollment state: %w", writeErr)
	}
	return &MTLSEnrollment{Assets: assets, Reenrolled: readErr == nil}, nil
}

func (m *MTLS) ClientCertificate(ctx context.Context, envID string) (*x509.Certificate, error) {
	if m.Mode == MTLSModeDisabled {
		return nil, errors.New("agent mTLS is disabled")
	}
	assetsDir, err := m.assetsDir(ctx, managerMTLSDirName)
	if err != nil {
		return nil, err
	}
	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	if safeEnvID == "" {
		return nil, errors.New("environment ID is required")
	}
	certPEM, err := acfs.Root{Path: assetsDir}.ReadFile(ctx, path.Join("/", generatedClientMTLSSubdir, safeEnvID, generatedMTLSClientCertName))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("agent mTLS client certificate is not PEM encoded")
	}
	return x509.ParseCertificate(block.Bytes)
}

func (m *MTLS) ManagerClient(ctx context.Context, opts ManagerClientOptions) (*http.Client, error) {
	if opts.Enroll {
		if err := m.enrollClient(ctx, opts.AgentToken); err != nil {
			return nil, err
		}
		if m.Mode != MTLSModeDisabled && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(httpx.ManagerBaseURL(m.ManagerURL))), "https://") {
			return nil, errors.New("ARCANE_AGENT_MTLS_MODE requires ARCANE_MANAGER_API_URL to use https")
		}
	}
	tlsConfig, err := m.clientTLS(ctx)
	if err != nil {
		return nil, err
	}
	return httpx.NewHTTPClient(httpx.ClientConfig{
		TLSClientConfig: tlsConfig,
		Timeout:         opts.Timeout,
		Instrument:      true,
	}), nil
}

func (m *MTLS) ResetClient(ctx context.Context) error {
	assetsDir, err := m.assetsDir(ctx, agentMTLSDirName)
	if err != nil {
		return err
	}
	if _, statErr := (acfs.Host{}).Stat(ctx, assetsDir, types.StatOptions{FollowFinal: true}); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	for _, name := range []string{generatedMTLSClientCertName, generatedMTLSClientKeyName, generatedMTLSPendingKeyName} {
		if removeErr := (acfs.Root{Path: assetsDir}).RemoveAll(ctx, "/"+name); removeErr != nil {
			return fmt.Errorf("failed to remove agent mTLS %s: %w", name, removeErr)
		}
	}
	return nil
}

func (m *MTLS) enrollClient(ctx context.Context, agentToken string) error {
	if m.Mode == MTLSModeDisabled {
		return nil
	}

	assetsDir, err := m.assetsDir(ctx, agentMTLSDirName)
	if err != nil {
		return err
	}
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	if m.CertFile != "" && m.KeyFile != "" && (filepath.Clean(m.CertFile) != certPath || filepath.Clean(m.KeyFile) != keyPath) {
		return nil
	}

	_, reason := loadClientCertificate(certPath, keyPath, time.Now(), agentMTLSRenewBefore)
	if reason != "" {
		if fileExists(ctx, certPath) {
			slog.WarnContext(ctx, "Existing agent mTLS assets need renewal; enrolling new assets", "reason", reason, "certPath", certPath)
		}
		if enrollErr := m.downloadClientAssets(ctx, agentToken, assetsDir, certPath, keyPath); enrollErr != nil {
			_, currentReason := loadClientCertificate(certPath, keyPath, time.Now(), 0)
			switch {
			case currentReason == "" && errors.Is(enrollErr, errEnrollUnreachable):
				slog.WarnContext(ctx, "Agent mTLS renewal failed; keeping the current client certificate until it expires", "error", enrollErr, "certPath", certPath)
			case m.Mode != MTLSModeRequired:
				slog.WarnContext(ctx, "Agent mTLS enrollment failed; proceeding without client certificate", "error", enrollErr)
				return nil
			default:
				return enrollErr
			}
		}
	}

	if reason != "" || !fileExists(ctx, filepath.Join(assetsDir, generatedMTLSEnrolledName)) {
		marker := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
		if writeErr := (acfs.Root{Path: assetsDir}).Write(ctx, "/"+generatedMTLSEnrolledName, marker, acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
			return fmt.Errorf("failed to write agent mTLS enrollment marker: %w", writeErr)
		}
	}
	caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	m.MTLSConfig = MTLSConfig{
		Mode:       m.Mode,
		CAFile:     cmp.Or(m.CAFile, kit.Ternary(fileExists(ctx, caPath), caPath, "")),
		CertFile:   certPath,
		KeyFile:    keyPath,
		ServerName: m.ServerName,
		AssetsDir:  m.AssetsDir,
		AppURL:     m.AppURL,
		ManagerURL: m.ManagerURL,
	}
	return nil
}

func (m *MTLS) downloadClientAssets(ctx context.Context, agentToken, assetsDir, certPath, keyPath string) error {
	managerBaseURL := strings.TrimRight(strings.TrimSpace(httpx.ManagerBaseURL(m.ManagerURL)), "/")
	if managerBaseURL == "" {
		return errors.New("ARCANE_MANAGER_API_URL is required to enroll agent mTLS assets")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(httpx.ManagerBaseURL(m.ManagerURL))), "https://") {
		return errors.New("ARCANE_AGENT_MTLS_MODE requires ARCANE_MANAGER_API_URL to use https for certificate enrollment")
	}

	httpClient, clientErr := (&MTLS{
		Mode:       m.Mode,
		CAFile:     m.CAFile,
		ServerName: m.ServerName,
		ManagerURL: m.ManagerURL,
	}).ManagerClient(ctx, ManagerClientOptions{Timeout: 30 * time.Second})
	if clientErr != nil {
		return fmt.Errorf("failed to configure agent mTLS enrollment client: %w", clientErr)
	}
	if mkdirErr := (acfs.Host{}).MkdirAll(ctx, assetsDir, fs.DirPerm); mkdirErr != nil {
		return fmt.Errorf("failed to create agent mTLS asset dir: %w", mkdirErr)
	}
	keyPEM, csrPEM, keyErr := pendingEnrollmentKey(ctx, assetsDir)
	if keyErr != nil {
		return keyErr
	}
	payload, marshalErr := json.Marshal(EnrollMTLSRequest{CSR: string(csrPEM)})
	if marshalErr != nil {
		return fmt.Errorf("failed to encode agent mTLS enrollment request: %w", marshalErr)
	}
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, managerBaseURL+"/api/tunnel/mtls/enroll", bytes.NewReader(payload))
	if reqErr != nil {
		return fmt.Errorf("failed to create agent mTLS enrollment request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	for header, value := range agentAuthCredentials(agentToken) {
		req.Header.Set(header, value)
	}

	resp, doErr := httpClient.Do(req)
	if doErr != nil {
		_, certRejected := errors.AsType[*tls.CertificateVerificationError](doErr)
		_, alert := errors.AsType[tls.AlertError](doErr)
		if certRejected || alert {
			return fmt.Errorf("agent mTLS enrollment handshake failed: %w", doErr)
		}
		return fmt.Errorf("%w: %w", errEnrollUnreachable, doErr)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxEnrollResponseBytes))
		return fmt.Errorf("agent mTLS enrollment failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var enrollResp EnrollMTLSResponse
	if decodeErr := json.UnmarshalRead(io.LimitReader(resp.Body, maxEnrollResponseBytes), &enrollResp); decodeErr != nil {
		return fmt.Errorf("failed to decode agent mTLS enrollment response: %w", decodeErr)
	}
	certIndex := slices.IndexFunc(enrollResp.Files, func(file GeneratedMTLSFile) bool {
		return file.Name == generatedMTLSClientCertName
	})
	if certIndex < 0 {
		return errors.New("agent mTLS enrollment response did not include a client certificate")
	}
	if _, pairErr := tls.X509KeyPair([]byte(enrollResp.Files[certIndex].Content), keyPEM); pairErr != nil {
		return fmt.Errorf("agent mTLS enrollment returned a certificate that was not issued for this agent's key; the manager may predate CSR enrollment: %w", pairErr)
	}

	assets := []GeneratedMTLSFile{
		{Name: generatedMTLSClientKeyName, Content: string(keyPEM)},
		enrollResp.Files[certIndex],
	}
	if caIndex := slices.IndexFunc(enrollResp.Files, func(file GeneratedMTLSFile) bool {
		return file.Name == generatedMTLSCACertFileName
	}); caIndex >= 0 {
		assets = append(assets, enrollResp.Files[caIndex])
	}
	for _, file := range assets {
		perm := kit.Ternary(file.Name == generatedMTLSClientKeyName, 0o600, fs.FilePerm)
		if writeErr := (acfs.Root{Path: assetsDir}).Write(ctx, "/"+file.Name, []byte(file.Content), acfs.WriteOptions{Mode: perm}); writeErr != nil {
			return fmt.Errorf("failed to write agent mTLS asset %s: %w", file.Name, writeErr)
		}
	}
	if _, writtenReason := loadClientCertificate(certPath, keyPath, time.Now(), 0); writtenReason != "" {
		return fmt.Errorf("agent mTLS enrollment wrote unusable assets: %s", writtenReason)
	}
	if removeErr := (acfs.Root{Path: assetsDir}).RemoveAll(ctx, "/"+generatedMTLSPendingKeyName); removeErr != nil {
		return fmt.Errorf("failed to remove pending agent mTLS key: %w", removeErr)
	}
	return nil
}

func pendingEnrollmentKey(ctx context.Context, assetsDir string) ([]byte, []byte, error) {
	keyPEM, readErr := acfs.Root{Path: assetsDir}.ReadFile(ctx, "/"+generatedMTLSPendingKeyName)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("failed to read pending agent mTLS key: %w", readErr)
	}
	var key crypto.Signer
	if block, _ := pem.Decode(keyPEM); block != nil {
		if parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
			if signer, ok := parsed.(crypto.Signer); ok && supportedGeneratedKey(signer.Public()) {
				key = signer
			}
		}
	}
	if key == nil {
		generated, genErr := certgen.GenerateMLDSA87PrivateKey()
		if genErr != nil {
			return nil, nil, fmt.Errorf("failed to generate agent mTLS private key: %w", genErr)
		}
		keyDER, marshalErr := x509.MarshalPKCS8PrivateKey(generated)
		if marshalErr != nil {
			return nil, nil, fmt.Errorf("failed to marshal agent mTLS private key: %w", marshalErr)
		}
		key = generated
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
		if writeErr := (acfs.Root{Path: assetsDir}).Write(ctx, "/"+generatedMTLSPendingKeyName, keyPEM, acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
			return nil, nil, fmt.Errorf("failed to write pending agent mTLS key: %w", writeErr)
		}
	}
	csrDER, csrErr := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if csrErr != nil {
		return nil, nil, fmt.Errorf("failed to create agent mTLS certificate signing request: %w", csrErr)
	}
	return keyPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), nil
}

func parseEnrollmentCSR(csrPEM string) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, ErrMTLSEnrollInvalidCSR
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMTLSEnrollInvalidCSR, err)
	}
	if sigErr := csr.CheckSignature(); sigErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrMTLSEnrollInvalidCSR, sigErr)
	}
	if !supportedGeneratedKey(csr.PublicKey) {
		return nil, fmt.Errorf("%w: unsupported key algorithm", ErrMTLSEnrollInvalidCSR)
	}
	return csr, nil
}

func (m *MTLS) clientTLS(ctx context.Context) (*tls.Config, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(httpx.ManagerBaseURL(m.ManagerURL))), "https://") {
		return nil, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.ServerName}

	if m.CAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			slog.WarnContext(ctx, "Failed to load system certificate pool; falling back to configured agent mTLS CA only", "error", err)
			pool = x509.NewCertPool()
		}
		if appendErr := appendCAFile(ctx, pool, m.CAFile); appendErr != nil {
			return nil, fmt.Errorf("failed to load agent mTLS CA file: %w", appendErr)
		}
		tlsConfig.RootCAs = pool
	}

	if m.CertFile == "" || m.KeyFile == "" {
		return tlsConfig, nil
	}
	cert, reason := loadClientCertificate(m.CertFile, m.KeyFile, time.Now(), 0)
	if reason != "" {
		if m.Mode == MTLSModeOptional {
			slog.WarnContext(ctx, "Ignoring unusable optional agent mTLS client certificate; falling back to token auth", "certPath", m.CertFile, "reason", reason)
			return tlsConfig, nil
		}
		return nil, fmt.Errorf("agent mTLS client certificate is unusable: %s", reason)
	}
	// ML-DSA client certificates require TLS 1.3.
	if _, ok := cert.Leaf.PublicKey.(*mldsa.PublicKey); ok {
		tlsConfig.MinVersion = tls.VersionTLS13
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	return tlsConfig, nil
}

func (m *MTLS) UsesGeneratedCA(ctx context.Context) bool {
	if m.Mode == MTLSModeDisabled {
		return false
	}
	if m.CAFile == "" {
		return true
	}
	assetsDir, err := m.assetsDir(ctx, managerMTLSDirName)
	caFile, absErr := filepath.Abs(m.CAFile)
	return err == nil && absErr == nil && caFile == filepath.Join(assetsDir, generatedMTLSCACertFileName)
}

func (m *MTLS) assetsDir(ctx context.Context, name string) (string, error) {
	baseDir := filepath.Join("data", name)
	if _, statErr := (acfs.Host{}).Stat(ctx, "/app/data", types.StatOptions{FollowFinal: true}); m.AssetsDir == "" && statErr == nil {
		baseDir = "/app/data/" + name
	}
	resolved, err := filepath.Abs(cmp.Or(m.AssetsDir, baseDir))
	if err != nil {
		return "", fmt.Errorf("failed to resolve agent mTLS assets dir: %w", err)
	}
	return resolved, nil
}

func appendCAFile(ctx context.Context, pool *x509.CertPool, caFile string) error {
	caFile = strings.TrimSpace(caFile)
	if caFile == "" {
		return errors.New("CA file is required")
	}
	caAbs, err := filepath.Abs(caFile)
	if err != nil {
		return err
	}
	pemBytes, err := acfs.Host{}.ReadFile(ctx, caAbs)
	if err != nil {
		return err
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return errors.New("failed to parse PEM certificates")
	}
	return nil
}

func hasVerifiedPeerCertificate(state *tls.ConnectionState) bool {
	return state != nil && len(state.PeerCertificates) > 0 && len(state.VerifiedChains) > 0
}

func certificateHasURISAN(cert *x509.Certificate, expected *url.URL) bool {
	return slices.ContainsFunc(cert.URIs, func(uri *url.URL) bool {
		return uri != nil && strings.EqualFold(uri.Scheme, expected.Scheme) && uri.Path == expected.Path &&
			strings.EqualFold(strings.TrimSuffix(uri.Host, "."), strings.TrimSuffix(expected.Host, "."))
	})
}

func ensureManagerCA(ctx context.Context, assetsDir string) (tls.Certificate, bool, error) {
	if err := (acfs.Host{}).MkdirAll(ctx, assetsDir, fs.DirPerm); err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to create agent mTLS assets dir: %w", err)
	}
	unlock, err := lockAgentMTLSPath(ctx, assetsDir, ".ca.lock")
	if err != nil {
		return tls.Certificate{}, false, err
	}
	defer unlock()

	certPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)
	keyPath := filepath.Join(assetsDir, generatedMTLSCAKeyFileName)
	certPEM, certErr := acfs.Root{Path: assetsDir}.ReadFile(ctx, "/"+generatedMTLSCACertFileName)
	rawKey, keyErr := acfs.Root{Path: assetsDir}.ReadFile(ctx, "/"+generatedMTLSCAKeyFileName)
	for _, readErr := range []error{certErr, keyErr} {
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return tls.Certificate{}, false, fmt.Errorf("failed to read agent mTLS CA: %w", readErr)
		}
	}
	ciphertext, encrypted := strings.CutPrefix(strings.TrimSpace(string(rawKey)), caKeyEncryptedPrefix)
	switch {
	case certErr != nil && keyErr != nil:
	case certErr != nil || keyErr != nil:
		slog.WarnContext(ctx, "Agent mTLS CA is incomplete; regenerating it", "certPath", certPath, "keyPath", keyPath)
	case !encrypted:
		slog.WarnContext(ctx, "Agent mTLS CA key is not in the supported encrypted format; regenerating the CA", "keyPath", keyPath)
	default:
		keyPEM, decryptErr := libcrypto.Decrypt(ciphertext)
		if decryptErr != nil {
			return tls.Certificate{}, false, fmt.Errorf("failed to decrypt agent mTLS CA private key %s: %w", keyPath, decryptErr)
		}
		ca, pairErr := tls.X509KeyPair(certPEM, []byte(keyPEM))
		if pairErr != nil {
			return tls.Certificate{}, false, fmt.Errorf("failed to load agent mTLS CA key pair from %s: %w", assetsDir, pairErr)
		}
		if ca.Leaf.IsCA && supportedGeneratedKey(ca.Leaf.PublicKey) {
			return ca, false, nil
		}
		slog.WarnContext(ctx, "Agent mTLS CA uses an unsupported algorithm; regenerating it", "certPath", certPath)
	}

	privateKey, err := certgen.GenerateMLDSA87PrivateKey()
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to generate CA private key: %w", err)
	}
	template, err := certgen.NewAgentMTLSCATemplate()
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
	if writeErr := atomic.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), fs.FilePerm); writeErr != nil {
		return tls.Certificate{}, false, writeErr
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to marshal CA private key: %w", err)
	}
	encryptedKey, err := caKeyEncrypt(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})))
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("failed to encrypt agent mTLS CA private key: %w", err)
	}
	if encryptedKey == "" {
		return tls.Certificate{}, false, errors.New("failed to encrypt agent mTLS CA private key: encrypted payload is empty")
	}
	if writeErr := atomic.WriteFile(keyPath, []byte(caKeyEncryptedPrefix+encryptedKey), 0o600); writeErr != nil {
		return tls.Certificate{}, false, writeErr
	}

	slog.InfoContext(ctx, "generated agent mTLS CA", "certPath", certPath)
	return tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: privateKey, Leaf: leaf}, true, nil
}

func loadClientCertificate(certPath, keyPath string, now time.Time, renewBefore time.Duration) (tls.Certificate, string) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	switch {
	case err != nil:
		return tls.Certificate{}, err.Error()
	case now.Before(cert.Leaf.NotBefore):
		return tls.Certificate{}, "certificate is not valid before " + cert.Leaf.NotBefore.UTC().Format(time.RFC3339)
	case !now.Before(cert.Leaf.NotAfter):
		return tls.Certificate{}, "certificate expired at " + cert.Leaf.NotAfter.UTC().Format(time.RFC3339)
	case renewBefore > 0 && now.Add(renewBefore).After(cert.Leaf.NotAfter):
		return tls.Certificate{}, "certificate expires soon at " + cert.Leaf.NotAfter.UTC().Format(time.RFC3339)
	}
	return cert, ""
}

func supportedGeneratedKey(publicKey crypto.PublicKey) bool {
	switch key := publicKey.(type) {
	case *ecdsa.PublicKey:
		return key.Curve == elliptic.P384()
	case *mldsa.PublicKey:
		return key.Parameters() == mldsa.MLDSA87()
	}
	return false
}

func lockAgentMTLSPath(ctx context.Context, dir, lockName string) (func(), error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent mTLS lock dir: %w", err)
	}
	lockPath := filepath.Join(absDir, lockName)

	deadline := time.Now().Add(managerCALockTimeout)
	for {
		if unlock, held := managerCALocks.TryLock(lockPath); held {
			file, openErr := acfs.Host{}.OpenFile(ctx, lockPath, acfs.OpenOptions{
				Mode:      0o600,
				Write:     true,
				Create:    true,
				Exclusive: true,
			})
			if openErr == nil {
				_, _ = fmt.Fprintf(file, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
				_ = file.Close()
				return func() {
					_ = acfs.Host{}.Remove(context.WithoutCancel(ctx), lockPath)
					unlock()
				}, nil
			}
			expired := time.Now().After(deadline)
			removedStale := errors.Is(openErr, os.ErrExist) && expired && removeStaleLock(ctx, lockPath)
			unlock()
			switch {
			case !errors.Is(openErr, os.ErrExist):
				return nil, fmt.Errorf("failed to acquire agent mTLS CA lock: %w", openErr)
			case removedStale:
				// Retry at once with a fresh deadline now that the stale lock is gone.
				deadline = time.Now().Add(managerCALockTimeout)
				continue
			case expired:
				return nil, fmt.Errorf("timed out waiting for agent mTLS CA lock %s", lockPath)
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cancelled waiting for agent mTLS CA lock: %w", ctx.Err())
		case <-time.After(managerCALockPollInterval):
		}
	}
}

func removeStaleLock(ctx context.Context, lockPath string) bool {
	content, err := acfs.Host{}.ReadFile(ctx, lockPath)
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

	removeErr := acfs.Host{}.Remove(ctx, lockPath)
	return removeErr == nil || errors.Is(removeErr, os.ErrNotExist)
}

func fileExists(ctx context.Context, localPath string) bool {
	entry, err := acfs.Host{}.Stat(ctx, localPath, types.StatOptions{FollowFinal: true})
	return err == nil && !entry.IsDirectory
}
