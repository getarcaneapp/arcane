package generate_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	gen "github.com/getarcaneapp/arcane/cli/v2/pkg/generate"
	"github.com/stretchr/testify/require"
)

func TestGenerateMTLSCommandWritesMLDSA87Assets(t *testing.T) {
	outDir := t.TempDir()

	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"mtls", "--out-dir", outDir, "--env-id", "env-123", "--app-url", "https://manager.example.com"})

	_, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	assertMLDSA87PrivateKey(t, filepath.Join(outDir, "ca.key"))
	assertMLDSA87PrivateKey(t, filepath.Join(outDir, "agent.key"))
	assertMLDSA87Certificate(t, filepath.Join(outDir, "ca.crt"))
	cert := assertMLDSA87Certificate(t, filepath.Join(outDir, "agent.crt"))

	require.False(t, len(cert.URIs) == 0 || cert.URIs[0].String() != "spiffe://manager.example.com/edge/env-123",
		"expected edge SPIFFE URI SAN, got %v", cert.URIs)

}

func TestGenerateTLSCommandWritesECDSAP384ServerCert(t *testing.T) {
	outDir := t.TempDir()

	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"tls", "--out-dir", outDir, "--common-name", "localhost", "--host", "localhost", "--host", "127.0.0.1", "--cert-name", "local-manager.crt", "--key-name", "local-manager.key"})

	_, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	assertECDSAP384PrivateKey(t, filepath.Join(outDir, "local-manager.key"))
	cert := assertECDSAP384Certificate(t, filepath.Join(outDir, "local-manager.crt"))

	require.False(t, len(cert.DNSNames) == 0 || cert.DNSNames[0] != "localhost",
		"expected localhost DNS SAN, got %v", cert.DNSNames)

	require.False(t, len(cert.IPAddresses) == 0 || !cert.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")),
		"expected 127.0.0.1 IP SAN, got %v", cert.IPAddresses)

}

func TestGenerateTLSCommandOverwritesCertificateAtomically(t *testing.T) {
	outDir := t.TempDir()

	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"tls", "--out-dir", outDir, "--common-name", "localhost", "--host", "localhost", "--cert-name", "server.crt", "--key-name", "server.key"})
	_, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	certPath := filepath.Join(outDir, "server.crt")
	keyPath := filepath.Join(outDir, "server.key")
	firstCert, err := os.ReadFile(certPath)

	require.NoError(t, err,
		"failed to read first cert: %v", err)

	cmd.SetArgs([]string{"tls", "--out-dir", outDir, "--common-name", "localhost", "--host", "localhost", "--cert-name", "server.crt", "--key-name", "server.key"})
	_, err = captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	secondCert, err := os.ReadFile(certPath)

	require.NoError(t, err,
		"failed to read second cert: %v", err)

	require.NotEqual(t, string(secondCert), string(firstCert),
		"expected certificate to be replaced on second generation")

	assertECDSAP384Certificate(t, certPath)
	assertECDSAP384PrivateKey(t, keyPath)

	tmpFiles, err := filepath.Glob(filepath.Join(outDir, "*.tmp-*"))

	require.NoError(t, err,
		"failed to glob temp files: %v", err)

	require.Empty(t, tmpFiles,
		"expected atomic write temp files to be cleaned up, got %v", tmpFiles)

}

func assertECDSAP384PrivateKey(t *testing.T, path string) {
	t.Helper()

	pemBytes, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read key %s: %v", path, err)

	block, _ := pem.Decode(pemBytes)

	require.NotNil(t, block,
		"failed to decode key PEM %s", path)

	require.Equal(t, "PRIVATE KEY", block.Type,
		"expected PRIVATE KEY for %s, got %s", path, block.Type)

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)

	require.NoError(t, err,
		"failed to parse private key %s: %v", path, err)

	key, ok := parsed.(*ecdsa.PrivateKey)

	require.True(t, ok,
		"expected ECDSA private key for %s", path)

	require.Equal(t, elliptic.P384(), key.Curve,
		"expected P-384 private key for %s", path)

}

func assertMLDSA87PrivateKey(t *testing.T, path string) {
	t.Helper()

	pemBytes, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read key %s: %v", path, err)

	block, _ := pem.Decode(pemBytes)

	require.NotNil(t, block,
		"failed to decode key PEM %s", path)

	require.Equal(t, "PRIVATE KEY", block.Type,
		"expected PRIVATE KEY for %s, got %s", path, block.Type)

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)

	require.NoError(t, err,
		"failed to parse private key %s: %v", path, err)

	key, ok := parsed.(*mldsa.PrivateKey)

	require.True(t, ok,
		"expected ML-DSA private key for %s", path)

	require.Equal(t, mldsa.MLDSA87(), key.PublicKey().Parameters(),
		"expected ML-DSA-87 private key for %s", path)

}

func assertMLDSA87Certificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()

	pemBytes, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read certificate %s: %v", path, err)

	block, _ := pem.Decode(pemBytes)

	require.NotNil(t, block,
		"failed to decode certificate PEM %s", path)

	cert, err := x509.ParseCertificate(block.Bytes)

	require.NoError(t, err,
		"failed to parse certificate %s: %v", path, err)

	publicKey, ok := cert.PublicKey.(*mldsa.PublicKey)

	require.True(t, ok,
		"expected ML-DSA public key for %s", path)

	require.Equal(t, mldsa.MLDSA87(), publicKey.Parameters(),
		"expected ML-DSA-87 certificate for %s", path)

	return cert
}

func assertECDSAP384Certificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()

	pemBytes, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read certificate %s: %v", path, err)

	block, _ := pem.Decode(pemBytes)

	require.NotNil(t, block,
		"failed to decode certificate PEM %s", path)

	cert, err := x509.ParseCertificate(block.Bytes)

	require.NoError(t, err,
		"failed to parse certificate %s: %v", path, err)

	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)

	require.True(t, ok,
		"expected ECDSA public key for %s", path)

	require.Equal(t, elliptic.P384(), publicKey.Curve,
		"expected P-384 certificate for %s", path)

	return cert
}
