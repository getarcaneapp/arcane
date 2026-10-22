package templates

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/notifications"
)

func TestService_RenderEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	_, htmlBody, err := svc.ImageUpdate("Homelab Prod", "nginx:latest", &types.ImageUpdateResponse{
		HasUpdate:     true,
		UpdateType:    "digest",
		CurrentDigest: "sha256:current",
		LatestDigest:  "sha256:latest",
		CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
	}).RenderEmail()
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Homelab Prod")

	subject := notifications.BuildEmailSubject("Homelab Prod", "Container Update Available: nginx:latest")
	require.Equal(t, "[Homelab Prod] Container Update Available: nginx:latest", subject)
}

func TestService_RenderContainerUpdateEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	_, htmlBody, err := svc.ContainerUpdate("Lab Remote", "nginx", "nginx:latest", "sha256:old", "sha256:new").RenderEmail()
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Lab Remote")

	subject := notifications.BuildEmailSubject("Lab Remote", "Container Updated: nginx")
	require.Equal(t, "[Lab Remote] Container Updated: nginx", subject)
}

func TestService_RenderBatchEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	updates := map[string]*types.ImageUpdateResponse{
		"nginx:latest": {
			HasUpdate:     true,
			UpdateType:    "digest",
			CurrentDigest: "sha256:current",
			LatestDigest:  "sha256:latest",
			CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
		},
		"redis:latest": {
			HasUpdate:     true,
			UpdateType:    "minor",
			CurrentDigest: "sha256:redis-current",
			LatestDigest:  "sha256:redis-latest",
			CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
		},
	}

	_, htmlBody, err := svc.BatchImageUpdate("Edge Cluster A", updates).RenderEmail()
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Edge Cluster A")

	subject := notifications.BuildEmailSubject("Edge Cluster A", "2 Container Image Updates Available")
	require.Equal(t, "[Edge Cluster A] 2 Container Image Updates Available", subject)
}

func TestService_RenderVulnerabilitySummaryEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	_, htmlBody, err := svc.Vulnerability("Remote Alpha", types.NotificationDispatchVulnerabilityFound{
		CVEID:        "Daily Summary - 2026-01-09",
		ImageName:    "5 image(s) scanned, 2 with fixable vulnerabilities",
		FixedVersion: "7 fixable vulnerability record(s)",
		Severity:     "Critical:1 High:3 Medium:2 Low:1 Unknown:0",
		PkgName:      "CVE-2025-1234",
	}).RenderEmail()
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Remote Alpha")
}

func TestService_RenderPruneReportEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	_, htmlBody, err := svc.PruneReport("Cluster West", &types.PruneAllResult{
		SpaceReclaimed:           3825205248,
		ContainerSpaceReclaimed:  503316480,
		ImageSpaceReclaimed:      2449473536,
		VolumeSpaceReclaimed:     641728512,
		BuildCacheSpaceReclaimed: 230162432,
	}).RenderEmail()
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Cluster West")
	for _, size := range []string{"3.6 GiB", "480.0 MiB", "2.3 GiB", "612.0 MiB", "219.5 MiB"} {
		require.Contains(t, htmlBody, size)
	}
}
