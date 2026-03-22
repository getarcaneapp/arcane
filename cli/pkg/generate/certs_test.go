package generate_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	gen "github.com/getarcaneapp/arcane/cli/pkg/generate"
)

func TestGenerateMTLSCommandWritesECDSAP384Assets(t *testing.T) {
	outDir := t.TempDir()

	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"mtls", "--out-dir", outDir, "--env-id", "env-123"})

	_, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	assertECDSAP384PrivateKey(t, filepath.Join(outDir, "ca.key"))
	assertECDSAP384PrivateKey(t, filepath.Join(outDir, "agent.key"))
	assertECDSAP384Certificate(t, filepath.Join(outDir, "ca.crt"))
	assertECDSAP384Certificate(t, filepath.Join(outDir, "agent.crt"))
}

func TestGenerateTLSCommandWritesECDSAP384ServerCert(t *testing.T) {
	outDir := t.TempDir()

	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"tls", "--out-dir", outDir, "--common-name", "localhost", "--host", "localhost", "--host", "127.0.0.1"})

	_, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	assertECDSAP384PrivateKey(t, filepath.Join(outDir, "server.key"))
	cert := assertECDSAP384Certificate(t, filepath.Join(outDir, "server.crt"))

	if len(cert.DNSNames) == 0 || cert.DNSNames[0] != "localhost" {
		t.Fatalf("expected localhost DNS SAN, got %v", cert.DNSNames)
	}
	if len(cert.IPAddresses) == 0 || !cert.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("expected 127.0.0.1 IP SAN, got %v", cert.IPAddresses)
	}
}

func assertECDSAP384PrivateKey(t *testing.T, path string) {
	t.Helper()

	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read key %s: %v", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("failed to decode key PEM %s", path)
	}
	if block.Type != "EC PRIVATE KEY" {
		t.Fatalf("expected EC PRIVATE KEY for %s, got %s", path, block.Type)
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse EC private key %s: %v", path, err)
	}
	if key.Curve != elliptic.P384() {
		t.Fatalf("expected P-384 private key for %s", path)
	}
}

func assertECDSAP384Certificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()

	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read certificate %s: %v", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("failed to decode certificate PEM %s", path)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate %s: %v", path, err)
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("expected ECDSA public key for %s", path)
	}
	if publicKey.Curve != elliptic.P384() {
		t.Fatalf("expected P-384 certificate for %s", path)
	}
	return cert
}
