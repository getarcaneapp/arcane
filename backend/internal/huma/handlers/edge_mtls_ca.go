package handlers

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	typesenvironment "github.com/getarcaneapp/arcane/types/environment"
)

const edgeMTLSCertificateExpiryWarningWindow = 30 * 24 * time.Hour

var generatedEdgeMTLSAssetNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func generatedEdgeMTLSCAPathInternal(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config not available")
	}
	if edge.NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == edge.EdgeMTLSModeDisabled {
		return "", fmt.Errorf("edge mTLS is disabled")
	}

	configuredCAPath := strings.TrimSpace(cfg.EdgeMTLSCAFile)
	if configuredCAPath != "" {
		if _, err := os.Stat(configuredCAPath); err != nil {
			return "", fmt.Errorf("stat configured edge mTLS CA: %w", err)
		}
		return configuredCAPath, nil
	}

	edgeCfg := &edge.Config{
		EdgeMTLSMode:         cfg.EdgeMTLSMode,
		EdgeMTLSAutoGenerate: cfg.EdgeMTLSAutoGenerate,
		EdgeMTLSCAFile:       cfg.EdgeMTLSCAFile,
		EdgeMTLSAssetsDir:    cfg.EdgeMTLSAssetsDir,
	}

	if err := edge.PrepareManagerMTLSAssets(edgeCfg); err != nil {
		return "", fmt.Errorf("prepare manager edge mTLS assets: %w", err)
	}

	caPath := strings.TrimSpace(edgeCfg.EdgeMTLSCAFile)
	if caPath == "" {
		return "", fmt.Errorf("generated edge mTLS CA is not available")
	}
	if _, err := os.Stat(caPath); err != nil {
		return "", fmt.Errorf("stat generated edge mTLS CA: %w", err)
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

	assetsDir := strings.TrimSpace(cfg.EdgeMTLSAssetsDir)
	if assetsDir == "" {
		baseDir := "data/edge-mtls"
		if _, err := os.Stat("/app/data"); err == nil {
			baseDir = "/app/data/edge-mtls"
		}

		resolved, err := filepath.Abs(baseDir)
		if err != nil {
			return "", fmt.Errorf("resolve edge mTLS assets dir: %w", err)
		}
		assetsDir = resolved
	}

	safeEnvID := generatedEdgeMTLSAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	if safeEnvID == "" {
		return "", fmt.Errorf("environment id is required")
	}

	certPath := filepath.Join(assetsDir, "clients", safeEnvID, "agent.crt")
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
	remaining := time.Until(expiresAt)
	daysRemaining := int(remaining.Hours() / 24)
	if remaining > 0 {
		daysRemaining++
	} else {
		daysRemaining = 0
	}

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
