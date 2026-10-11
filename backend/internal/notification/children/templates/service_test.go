package templates

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

func TestService_RenderEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	htmlBody, textBody, err := svc.renderEmailTemplateInternal("Homelab Prod", "nginx:latest", &imageupdate.Response{
		HasUpdate:     true,
		UpdateType:    "digest",
		CurrentDigest: "sha256:current",
		LatestDigest:  "sha256:latest",
		CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Homelab Prod")
	require.Contains(t, textBody, "Homelab Prod")

	subject := notifications.BuildEmailSubject("Homelab Prod", "Container Update Available: nginx:latest")
	require.Equal(t, "[Homelab Prod] Container Update Available: nginx:latest", subject)
}

func TestService_RenderContainerUpdateEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	htmlBody, textBody, err := svc.renderContainerUpdateEmailTemplateInternal("Lab Remote", "nginx", "nginx:latest", "sha256:old", "sha256:new")
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Lab Remote")
	require.Contains(t, textBody, "Lab Remote")

	subject := notifications.BuildEmailSubject("Lab Remote", "Container Updated: nginx")
	require.Equal(t, "[Lab Remote] Container Updated: nginx", subject)
}

func TestService_RenderBatchEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	updates := map[string]*imageupdate.Response{
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

	htmlBody, textBody, err := svc.renderBatchEmailTemplateInternal("Edge Cluster A", updates)
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Edge Cluster A")
	require.Contains(t, textBody, "Edge Cluster A")

	subject := notifications.BuildEmailSubject("Edge Cluster A", "2 Container Image Updates Available")
	require.Equal(t, "[Edge Cluster A] 2 Container Image Updates Available", subject)
}

func TestService_RenderVulnerabilitySummaryEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	htmlBody, textBody, err := svc.renderVulnerabilitySummaryEmailTemplateInternal("Remote Alpha", notification.DispatchVulnerabilityFound{
		CVEID:        "Daily Summary - 2026-01-09",
		ImageName:    "5 image(s) scanned, 2 with fixable vulnerabilities",
		FixedVersion: "7 fixable vulnerability record(s)",
		Severity:     "Critical:1 High:3 Medium:2 Low:1 Unknown:0",
		PkgName:      "CVE-2025-1234",
	})
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Remote Alpha")
	require.Contains(t, textBody, "Remote Alpha")
}

func TestService_RenderPruneReportEmailTemplate_IncludesEnvironment(t *testing.T) {
	svc := NewService(func() string { return "http://localhost:3000" })

	htmlBody, textBody, err := svc.renderPruneReportEmailTemplateInternal("Cluster West", &system.PruneAllResult{
		SpaceReclaimed:           3825205248,
		ContainerSpaceReclaimed:  503316480,
		ImageSpaceReclaimed:      2449473536,
		VolumeSpaceReclaimed:     641728512,
		BuildCacheSpaceReclaimed: 230162432,
	})
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Cluster West")
	require.Contains(t, textBody, "Cluster West")
	for _, size := range []string{"3.6 GiB", "480.0 MiB", "2.3 GiB", "612.0 MiB", "219.5 MiB"} {
		require.Contains(t, htmlBody, size)
		require.Contains(t, textBody, size)
	}
}
