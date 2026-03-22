package generate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	certValidity = 5 * 365 * 24 * time.Hour
)

var (
	mtlsOutDir    string
	mtlsEnvID     string
	tlsOutDir     string
	tlsCommonName string
	tlsHosts      []string
)

var mtlsCmd = &cobra.Command{
	Use:   "mtls",
	Short: "Generate Arcane edge mTLS assets",
	Long:  `Generate an Arcane-managed edge mTLS CA and agent client certificate bundle using ECDSA P-384.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateMTLSOutputInternal()
	},
}

var tlsCmd = &cobra.Command{
	Use:   "tls",
	Short: "Generate Arcane HTTPS TLS assets",
	Long:  `Generate a self-signed Arcane HTTPS server certificate bundle using ECDSA P-384.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateTLSOutputInternal()
	},
}

func init() {
	GenerateCmd.AddCommand(mtlsCmd)
	GenerateCmd.AddCommand(tlsCmd)

	mtlsCmd.Flags().StringVar(&mtlsOutDir, "out-dir", "./edge-mtls", "directory to write generated edge mTLS assets")
	mtlsCmd.Flags().StringVar(&mtlsEnvID, "env-id", "local-edge", "environment identifier to embed in the generated edge agent certificate")

	tlsCmd.Flags().StringVar(&tlsOutDir, "out-dir", "./arcane-tls", "directory to write generated Arcane TLS assets")
	tlsCmd.Flags().StringVar(&tlsCommonName, "common-name", "localhost", "certificate common name")
	tlsCmd.Flags().StringSliceVar(&tlsHosts, "host", []string{"localhost", "127.0.0.1"}, "DNS name or IP SAN to include; repeat flag to add more")
}

func generateMTLSOutputInternal() error {
	outDir, err := filepath.Abs(strings.TrimSpace(mtlsOutDir))
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	paths, err := generateEdgeMTLSBundleInternal(outDir, strings.TrimSpace(mtlsEnvID))
	if err != nil {
		return err
	}

	fmt.Println("Generated Arcane edge mTLS assets (ECDSA P-384)")
	fmt.Printf("CA cert: %s\n", paths.CACertPath)
	fmt.Printf("CA key: %s\n", paths.CAKeyPath)
	fmt.Printf("Agent cert: %s\n", paths.ClientCertPath)
	fmt.Printf("Agent key: %s\n", paths.ClientKeyPath)
	fmt.Println()
	fmt.Println("Manager env")
	fmt.Printf("EDGE_MTLS_MODE=required\nEDGE_MTLS_CA_FILE=%s\n", paths.CACertPath)
	fmt.Println()
	fmt.Println("Agent env")
	fmt.Printf("EDGE_MTLS_MODE=required\nEDGE_MTLS_CA_FILE=%s\nEDGE_MTLS_CERT_FILE=%s\nEDGE_MTLS_KEY_FILE=%s\n", paths.CACertPath, paths.ClientCertPath, paths.ClientKeyPath)
	return nil
}

func generateTLSOutputInternal() error {
	outDir, err := filepath.Abs(strings.TrimSpace(tlsOutDir))
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	paths, err := generateServerTLSBundleInternal(outDir, strings.TrimSpace(tlsCommonName), tlsHosts)
	if err != nil {
		return err
	}

	fmt.Println("Generated Arcane TLS assets (ECDSA P-384)")
	fmt.Printf("Server cert: %s\n", paths.CertPath)
	fmt.Printf("Server key: %s\n", paths.KeyPath)
	fmt.Println()
	fmt.Println("Manager env")
	fmt.Printf("TLS_ENABLED=true\nTLS_CERT_FILE=%s\nTLS_KEY_FILE=%s\n", paths.CertPath, paths.KeyPath)
	return nil
}

type edgeMTLSPaths struct {
	CACertPath     string
	CAKeyPath      string
	ClientCertPath string
	ClientKeyPath  string
}

type serverTLSPaths struct {
	CertPath string
	KeyPath  string
}

func generateEdgeMTLSBundleInternal(outDir, envID string) (*edgeMTLSPaths, error) {
	if envID == "" {
		return nil, fmt.Errorf("env ID is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}
	caTemplate, err := newCertificateTemplateInternal("Arcane Edge mTLS CA")
	if err != nil {
		return nil, err
	}
	caTemplate.Subject = pkix.Name{CommonName: "Arcane Edge mTLS CA", Organization: []string{"Arcane"}}
	caTemplate.IsCA = true
	caTemplate.BasicConstraintsValid = true
	caTemplate.MaxPathLen = 1
	caTemplate.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature

	caDER, err := x509.CreateCertificate(crand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client private key: %w", err)
	}
	clientTemplate, err := newCertificateTemplateInternal(fmt.Sprintf("arcane-edge-%s", envID))
	if err != nil {
		return nil, err
	}
	clientTemplate.Subject = pkix.Name{
		CommonName:   fmt.Sprintf("arcane-edge-%s", envID),
		Organization: []string{"Arcane Edge Agents"},
	}
	clientTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	clientTemplate.KeyUsage = x509.KeyUsageDigitalSignature

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated CA certificate: %w", err)
	}
	clientDER, err := x509.CreateCertificate(crand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create client certificate: %w", err)
	}

	paths := &edgeMTLSPaths{
		CACertPath:     filepath.Join(outDir, "ca.crt"),
		CAKeyPath:      filepath.Join(outDir, "ca.key"),
		ClientCertPath: filepath.Join(outDir, "agent.crt"),
		ClientKeyPath:  filepath.Join(outDir, "agent.key"),
	}
	if err := writeCertificateBundleInternal(paths.CACertPath, paths.CAKeyPath, caDER, caKey); err != nil {
		return nil, err
	}
	if err := writeCertificateBundleInternal(paths.ClientCertPath, paths.ClientKeyPath, clientDER, clientKey); err != nil {
		return nil, err
	}

	return paths, nil
}

func generateServerTLSBundleInternal(outDir, commonName string, hosts []string) (*serverTLSPaths, error) {
	if commonName == "" {
		return nil, fmt.Errorf("common name is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server private key: %w", err)
	}

	template, err := newCertificateTemplateInternal(commonName)
	if err != nil {
		return nil, err
	}
	template.Subject = pkix.Name{CommonName: commonName, Organization: []string{"Arcane"}}
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	template.KeyUsage = x509.KeyUsageDigitalSignature

	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
			continue
		}
		template.DNSNames = append(template.DNSNames, host)
	}
	if len(template.DNSNames) == 0 && len(template.IPAddresses) == 0 {
		template.DNSNames = []string{commonName}
	}

	certDER, err := x509.CreateCertificate(crand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create server certificate: %w", err)
	}

	paths := &serverTLSPaths{
		CertPath: filepath.Join(outDir, "server.crt"),
		KeyPath:  filepath.Join(outDir, "server.key"),
	}
	if err := writeCertificateBundleInternal(paths.CertPath, paths.KeyPath, certDER, privateKey); err != nil {
		return nil, err
	}
	return paths, nil
}

func newCertificateTemplateInternal(commonName string) (*x509.Certificate, error) {
	serial, err := randomSerialInternal()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(certValidity),
	}, nil
}

func randomSerialInternal() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := crand.Int(crand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate serial: %w", err)
	}
	return serial, nil
}

func writeCertificateBundleInternal(certPath, keyPath string, certDER []byte, privateKey *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal EC private key: %w", err)
	}
	if err := writePEMFileInternal(certPath, "CERTIFICATE", certDER, 0o644); err != nil {
		return err
	}
	if err := writePEMFileInternal(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return err
	}
	return nil
}

func writePEMFileInternal(path, blockType string, bytes []byte, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: bytes}); err != nil {
		return fmt.Errorf("failed to write PEM file %s: %w", path, err)
	}
	return nil
}
