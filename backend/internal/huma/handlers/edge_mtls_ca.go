package handlers

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	typesenvironment "github.com/getarcaneapp/arcane/types/environment"
)

const edgeMTLSCertificateExpiryWarningWindow = 30 * 24 * time.Hour

func generatedEdgeMTLSCAPathInternal(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config not available")
	}
	if edge.NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == edge.EdgeMTLSModeDisabled {
		return "", fmt.Errorf("edge mTLS is disabled")
	}

	edgeCfg := &edge.Config{
		EdgeMTLSMode:      cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:    cfg.EdgeMTLSCAFile,
		EdgeMTLSAssetsDir: cfg.EdgeMTLSAssetsDir,
		AppURL:            cfg.GetAppURL(),
	}
	caPath, err := edge.GeneratedManagerMTLSCAPath(edgeCfg)
	if err != nil {
		return "", fmt.Errorf("resolve edge mTLS CA path: %w", err)
	}
	if _, err := os.Stat(caPath); err != nil {
		return "", fmt.Errorf("stat edge mTLS CA: %w", err)
	}

	return caPath, nil
}

func hasGeneratedEdgeMTLSCAInternal(cfg *config.Config) bool {
	_, err := generatedEdgeMTLSCAPathInternal(cfg)
	return err == nil
}

func generatedEdgeMTLSClientCertPathInternal(cfg *config.Config, envID string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config not available")
	}
	if edge.NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == edge.EdgeMTLSModeDisabled {
		return "", fmt.Errorf("edge mTLS is disabled")
	}

	edgeCfg := &edge.Config{
		EdgeMTLSAssetsDir: cfg.EdgeMTLSAssetsDir,
	}

	certPath, err := edge.GeneratedManagerClientMTLSCertPath(edgeCfg, envID)
	if err != nil {
		return "", fmt.Errorf("resolve generated edge mTLS client certificate path: %w", err)
	}
	if _, err := os.Stat(certPath); err != nil {
		return "", fmt.Errorf("stat generated edge mTLS client certificate: %w", err)
	}

	return certPath, nil
}

func readGeneratedEdgeMTLSCertificateInfoInternal(cfg *config.Config, envID string) (*typesenvironment.EdgeMTLSCertificate, error) {
	certPath, err := generatedEdgeMTLSClientCertPathInternal(cfg, envID)
	if err != nil {
		return nil, err
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read generated edge mTLS client certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("decode generated edge mTLS client certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse generated edge mTLS client certificate: %w", err)
	}

	expiresAt := cert.NotAfter.UTC()
	now := time.Now().UTC()
	remaining := expiresAt.Sub(now)
	daysRemaining := edgeMTLSCertificateDaysRemainingInternal(now, expiresAt)

	info := &typesenvironment.EdgeMTLSCertificate{
		ExpiresAt:     &expiresAt,
		DaysRemaining: &daysRemaining,
		Expired:       now.After(expiresAt),
		ExpiringSoon:  now.Before(expiresAt) && remaining <= edgeMTLSCertificateExpiryWarningWindow,
	}

	if commonName := strings.TrimSpace(cert.Subject.CommonName); commonName != "" {
		info.CommonName = &commonName
	}

	return info, nil
}

func edgeMTLSCertificateDaysRemainingInternal(now time.Time, expiresAt time.Time) int {
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours() / 24))
}
