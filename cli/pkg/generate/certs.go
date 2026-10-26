package generate

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.getarcane.app/acfs/atomic"
	kit "go.getarcane.app/kit/pkg"
)

const (
	certValidity = 5 * 365 * 24 * time.Hour
)

func newMTLSCmd() *cobra.Command {
	var opts struct {
		outDir string
		envID  string
		appURL string
	}
	cmd := &cobra.Command{
		Use:   "mtls",
		Short: "Generate Arcane agent mTLS assets",
		Long:  `Generate an Arcane-managed agent mTLS CA and agent client certificate bundle using ML-DSA-87.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateMTLSOutput(cmd, opts.outDir, opts.envID, opts.appURL)
		},
	}
	cmd.Flags().StringVar(&opts.outDir, "out-dir", "./agent-mtls", "directory to write generated agent mTLS assets")
	cmd.Flags().StringVar(&opts.envID, "env-id", "local-agent", "environment identifier to embed in the generated agent certificate")
	cmd.Flags().StringVar(&opts.appURL, "app-url", "http://localhost:3552", "Arcane manager app URL to embed as the agent mTLS SPIFFE trust domain")
	return cmd
}

func newTLSCmd() *cobra.Command {
	var opts struct {
		outDir     string
		commonName string
		hosts      []string
		certName   string
		keyName    string
	}
	cmd := &cobra.Command{
		Use:   "tls",
		Short: "Generate Arcane HTTPS TLS assets",
		Long:  `Generate a self-signed Arcane HTTPS server certificate bundle using ECDSA P-384.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateTLSOutput(cmd, opts.outDir, opts.commonName, opts.hosts, opts.certName, opts.keyName)
		},
	}
	cmd.Flags().StringVar(&opts.outDir, "out-dir", "./arcane-tls", "directory to write generated Arcane TLS assets")
	cmd.Flags().StringVar(&opts.commonName, "common-name", "localhost", "certificate common name")
	cmd.Flags().StringSliceVar(&opts.hosts, "host", []string{"localhost", "127.0.0.1"}, "DNS name or IP SAN to include; repeat flag to add more")
	cmd.Flags().StringVar(&opts.certName, "cert-name", "server.crt", "certificate file name to write under out-dir")
	cmd.Flags().StringVar(&opts.keyName, "key-name", "server.key", "private key file name to write under out-dir")
	return cmd
}

func generateMTLSOutput(cmd *cobra.Command, mtlsOutDir, mtlsEnvID, mtlsAppURL string) error {
	w := cmd.OutOrStdout()
	outDir, err := filepath.Abs(strings.TrimSpace(mtlsOutDir))
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	paths, err := generateAgentMTLSBundle(outDir, strings.TrimSpace(mtlsEnvID), strings.TrimSpace(mtlsAppURL))
	if err != nil {
		return err
	}

	if jsonOutput(cmd) {
		return printJSON(cmd, paths)
	}
	_, _ = fmt.Fprintln(w, "Generated Arcane agent mTLS assets (ML-DSA-87)")
	_, _ = fmt.Fprintf(w, "CA cert: %s\n", paths.CACertPath)
	_, _ = fmt.Fprintf(w, "CA key: %s\n", paths.CAKeyPath)
	_, _ = fmt.Fprintf(w, "Agent cert: %s\n", paths.ClientCertPath)
	_, _ = fmt.Fprintf(w, "Agent key: %s\n", paths.ClientKeyPath)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Manager env")
	_, _ = fmt.Fprintf(w, "ARCANE_AGENT_MTLS_MODE=required\nARCANE_AGENT_MTLS_CA_FILE=%s\n", paths.CACertPath)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Agent env")
	_, _ = fmt.Fprintf(w, "ARCANE_AGENT_MTLS_MODE=required\nARCANE_AGENT_MTLS_CA_FILE=%s\nARCANE_AGENT_MTLS_CERT_FILE=%s\nARCANE_AGENT_MTLS_KEY_FILE=%s\n",
		paths.CACertPath, paths.ClientCertPath, paths.ClientKeyPath)
	return nil
}

func generateTLSOutput(cmd *cobra.Command, tlsOutDir, tlsCommonName string, tlsHosts []string, tlsCertName, tlsKeyName string) error {
	w := cmd.OutOrStdout()
	outDir, err := filepath.Abs(strings.TrimSpace(tlsOutDir))
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	paths, err := generateServerTLSBundle(outDir, strings.TrimSpace(tlsCommonName), tlsHosts, strings.TrimSpace(tlsCertName), strings.TrimSpace(tlsKeyName))
	if err != nil {
		return err
	}

	if jsonOutput(cmd) {
		return printJSON(cmd, paths)
	}
	_, _ = fmt.Fprintln(w, "Generated Arcane TLS assets (ECDSA P-384)")
	_, _ = fmt.Fprintf(w, "Server cert: %s\n", paths.CertPath)
	_, _ = fmt.Fprintf(w, "Server key: %s\n", paths.KeyPath)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Manager env")
	_, _ = fmt.Fprintf(w, "ARCANE_TLS_ENABLED=true\nARCANE_TLS_CERT_FILE=%s\nARCANE_TLS_KEY_FILE=%s\n", paths.CertPath, paths.KeyPath)
	return nil
}

type agentMTLSPaths struct {
	CACertPath     string `json:"cACertPath"`
	CAKeyPath      string `json:"cAKeyPath"`
	ClientCertPath string `json:"clientCertPath"`
	ClientKeyPath  string `json:"clientKeyPath"`
}

type serverTLSPaths struct {
	CertPath string `json:"certPath"`
	KeyPath  string `json:"keyPath"`
}

func generateAgentMTLSBundle(outDir, envID, appURL string) (*agentMTLSPaths, error) {
	if envID == "" {
		return nil, errors.New("env ID is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	caKey, err := GenerateMLDSA87PrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}
	caTemplate, err := NewAgentMTLSCATemplate()
	if err != nil {
		return nil, err
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.PublicKey(), caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	clientKey, err := GenerateMLDSA87PrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client private key: %w", err)
	}
	trustDomain := AgentMTLSTrustDomain(appURL)
	dnsSANs := []string{"arcane-agent"}
	if trustDomain != "" {
		dnsSANs = append(dnsSANs, "arcane-agent."+trustDomain)
	}
	clientTemplate, err := NewAgentMTLSClientTemplate("arcane-agent-"+envID, BuildAgentMTLSURISAN(appURL, envID), dnsSANs)
	if err != nil {
		return nil, err
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated CA certificate: %w", err)
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, clientKey.PublicKey(), caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create client certificate: %w", err)
	}

	paths := &agentMTLSPaths{
		CACertPath:     filepath.Join(outDir, "ca.crt"),
		CAKeyPath:      filepath.Join(outDir, "ca.key"),
		ClientCertPath: filepath.Join(outDir, "agent.crt"),
		ClientKeyPath:  filepath.Join(outDir, "agent.key"),
	}
	if writeCertificateBundleErr := writeCertificateBundle(paths.CACertPath, paths.CAKeyPath, caDER, caKey); writeCertificateBundleErr != nil {
		return nil, writeCertificateBundleErr
	}
	if writeClientBundleErr := writeCertificateBundle(paths.ClientCertPath, paths.ClientKeyPath, clientDER, clientKey); writeClientBundleErr != nil {
		return nil, writeClientBundleErr
	}

	return paths, nil
}

func generateServerTLSBundle(outDir, commonName string, hosts []string, certName, keyName string) (*serverTLSPaths, error) {
	if commonName == "" {
		return nil, errors.New("common name is required")
	}
	certName = filepath.Base(strings.TrimSpace(certName))
	if certName == "." || certName == "" {
		return nil, errors.New("certificate file name is required")
	}
	keyName = filepath.Base(strings.TrimSpace(keyName))
	if keyName == "." || keyName == "" {
		return nil, errors.New("key file name is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	privateKey, err := GenerateP384PrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate server private key: %w", err)
	}

	template, err := NewServerTLSTemplate(commonName, hosts)
	if err != nil {
		return nil, err
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create server certificate: %w", err)
	}

	paths := &serverTLSPaths{
		CertPath: filepath.Join(outDir, certName),
		KeyPath:  filepath.Join(outDir, keyName),
	}
	if writeCertificateBundleErr := writeCertificateBundle(paths.CertPath, paths.KeyPath, certDER, privateKey); writeCertificateBundleErr != nil {
		return nil, writeCertificateBundleErr
	}
	return paths, nil
}

// GenerateP384PrivateKey generates an ECDSA P-384 private key.
func GenerateP384PrivateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
}

// GenerateMLDSA87PrivateKey generates an ML-DSA-87 private key.
func GenerateMLDSA87PrivateKey() (*mldsa.PrivateKey, error) {
	return mldsa.GenerateKey(mldsa.MLDSA87())
}

// NewAgentMTLSCATemplate builds Arcane's agent mTLS CA certificate template.
func NewAgentMTLSCATemplate() (*x509.Certificate, error) {
	template, err := newCertificateTemplate("Arcane Agent mTLS CA")
	if err != nil {
		return nil, err
	}
	template.Subject = pkix.Name{CommonName: "Arcane Agent mTLS CA", Organization: []string{"Arcane"}}
	template.IsCA = true
	template.BasicConstraintsValid = true
	template.MaxPathLen = 1
	template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
	return template, nil
}

// NewAgentMTLSClientTemplate builds an agent client certificate template.
func NewAgentMTLSClientTemplate(commonName string, uriSAN *url.URL, dnsSANs []string) (*x509.Certificate, error) {
	commonName = strings.TrimSpace(commonName)
	if commonName == "" {
		return nil, errors.New("common name is required")
	}
	template, err := newCertificateTemplate(commonName)
	if err != nil {
		return nil, err
	}
	template.NotBefore = time.Now().UTC().Add(-24 * time.Hour)
	template.Subject = pkix.Name{
		CommonName:   commonName,
		Organization: []string{"Arcane Agents"},
	}
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	template.KeyUsage = x509.KeyUsageDigitalSignature
	if uriSAN != nil {
		template.URIs = []*url.URL{uriSAN}
	}
	template.DNSNames = kit.TrimNonEmpty(dnsSANs)
	return template, nil
}

// NewServerTLSTemplate builds a self-signed Arcane HTTPS server certificate template.
func NewServerTLSTemplate(commonName string, hosts []string) (*x509.Certificate, error) {
	commonName = strings.TrimSpace(commonName)
	if commonName == "" {
		return nil, errors.New("common name is required")
	}
	template, err := newCertificateTemplate(commonName)
	if err != nil {
		return nil, err
	}
	template.Subject = pkix.Name{CommonName: commonName, Organization: []string{"Arcane"}}
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	template.KeyUsage = x509.KeyUsageDigitalSignature

	for _, host := range kit.TrimNonEmpty(hosts) {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
			continue
		}
		template.DNSNames = append(template.DNSNames, host)
	}
	if len(template.DNSNames) == 0 && len(template.IPAddresses) == 0 {
		template.DNSNames = []string{commonName}
	}
	return template, nil
}

// AgentMTLSTrustDomain returns the SPIFFE trust domain derived from an Arcane app URL.
func AgentMTLSTrustDomain(appURL string) string {
	appURL = strings.TrimSpace(appURL)
	if appURL == "" {
		return ""
	}
	parseURL := appURL
	if !strings.Contains(parseURL, "://") {
		parseURL = "https://" + parseURL
	}
	parsed, err := url.Parse(parseURL)
	if err == nil {
		if host := strings.TrimSpace(parsed.Hostname()); host != "" {
			return strings.ToLower(strings.TrimSuffix(host, "."))
		}
	}
	return strings.ToLower(strings.TrimSuffix(appURL, "."))
}

// BuildAgentMTLSURISAN returns the SPIFFE URI SAN for an agent environment ID.
func BuildAgentMTLSURISAN(appURL, envID string) *url.URL {
	trustDomain := AgentMTLSTrustDomain(appURL)
	envID = strings.TrimSpace(envID)
	if trustDomain == "" || envID == "" {
		return nil
	}
	return &url.URL{Scheme: "spiffe", Host: trustDomain, Path: "/agent/" + envID}
}

func newCertificateTemplate(commonName string) (*x509.Certificate, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate serial: %w", err)
	}

	now := time.Now().UTC()
	return &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(certValidity),
	}, nil
}

func writeCertificateBundle(certPath, keyPath string, certDER []byte, privateKey crypto.PrivateKey) error {
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if certPEM == nil {
		return fmt.Errorf("failed to encode PEM file %s", certPath)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if keyPEM == nil {
		return fmt.Errorf("failed to encode PEM file %s", keyPath)
	}
	if writeErr := atomic.WriteFile(certPath, certPEM, 0o644); writeErr != nil {
		return writeErr
	}
	return atomic.WriteFile(keyPath, keyPEM, 0o600)
}
