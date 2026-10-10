package notifications

import (
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/stretchr/testify/require"
)

func TestNtfyNotificationMessagesVisualLayout(t *testing.T) {
	currentDigest := "sha256:6ef4b819f722fccdc036af611c4774cfdc2de821ab74fdd48bbf4c9d6f8973da"
	latestDigest := "sha256:4c599cf0818965782f93f652bd843e92714992e84d6b88ad17713dc0951cda1f"

	tests := []struct {
		name        string
		build       func() string
		contains    []string
		notContains []string
	}{
		{
			name: "single image update",
			build: func() string {
				return BuildImageUpdateNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					"binwiederhier/ntfy:latest",
					&imageupdate.Response{
						HasUpdate:     true,
						UpdateType:    "digest",
						CurrentDigest: currentDigest,
						LatestDigest:  latestDigest,
					},
				)
			},
			contains: []string{
				"🖥️ Local Docker",
				"📦 **binwiederhier/ntfy:latest**",
				"🔄 `sha256:6ef4b8...` → `sha256:4c599c...`",
			},
			notContains: []string{
				"Container Image Update",
				"Update Type:",
				currentDigest,
				latestDigest,
			},
		},
		{
			name: "batch image updates",
			build: func() string {
				return BuildBatchImageUpdateNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					map[string]*imageupdate.Response{
						"binwiederhier/ntfy:latest": {
							HasUpdate:     true,
							UpdateType:    "digest",
							CurrentDigest: currentDigest,
							LatestDigest:  latestDigest,
						},
						"postgres:18": {
							HasUpdate:      true,
							UpdateType:     "tag",
							CurrentVersion: "17",
							LatestVersion:  "18",
						},
					},
				)
			},
			contains: []string{
				"📦 **binwiederhier/ntfy:latest**",
				"🔄 `sha256:6ef4b8...` → `sha256:4c599c...`",
				"📦 **postgres:18**",
				"🔄 `17` → `18`",
			},
			notContains: []string{
				"Container Image Updates Available",
				"Type:",
			},
		},
		{
			name: "single container update",
			build: func() string {
				return BuildContainerUpdateNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					"jellyfin",
					"jellyfin/jellyfin:latest",
					currentDigest,
					latestDigest,
				)
			},
			contains: []string{
				"🐳 **jellyfin**",
				"📦 `jellyfin/jellyfin:latest`",
				"🔄 `sha256:6ef4b8...` → `sha256:4c599c...`",
			},
			notContains: []string{
				"Container Updated",
				"Previous Version:",
				"Current Version:",
			},
		},
		{
			name: "batch container updates",
			build: func() string {
				return BuildBatchContainerUpdateNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					[]ContainerUpdateBatchEntry{
						{
							ContainerName: "jellyfin",
							ImageRef:      "jellyfin/jellyfin:latest",
							OldDigest:     currentDigest,
							NewDigest:     latestDigest,
						},
						{
							ContainerName: "ntfy",
							ImageRef:      "binwiederhier/ntfy:latest",
							OldDigest:     currentDigest,
							NewDigest:     latestDigest,
						},
					},
				)
			},
			contains: []string{
				"🐳 **jellyfin**",
				"🐳 **ntfy**",
				"🔄 `sha256:6ef4b8...` → `sha256:4c599c...`",
			},
			notContains: []string{
				"Containers Updated",
				"Previous Version:",
			},
		},
		{
			name: "single container update with image tags",
			build: func() string {
				return BuildContainerUpdateNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					"nginx",
					"nginx:1.27-alpine",
					"nginx:1.26-alpine",
					"nginx:1.27-alpine",
				)
			},
			contains: []string{
				"📦 `nginx:1.27-alpine`",
				"🔄 `nginx:1.26-alpine` → `nginx:1.27-alpine`",
			},
			notContains: []string{
				"nginx:1.26-a...",
				"nginx:1.27-a...",
			},
		},
		{
			name: "batch container updates with image tags",
			build: func() string {
				return BuildBatchContainerUpdateNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					[]ContainerUpdateBatchEntry{
						{
							ContainerName: "nginx",
							ImageRef:      "nginx:1.27-alpine",
							OldDigest:     "nginx:1.26-alpine",
							NewDigest:     "nginx:1.27-alpine",
						},
						{
							ContainerName: "private-registry",
							ImageRef:      "registry.example.com:5000/example/app:1.27-alpine",
							OldDigest:     "registry.example.com:5000/example/app:1.26-alpine",
							NewDigest:     "registry.example.com:5000/example/app:1.27-alpine",
						},
					},
				)
			},
			contains: []string{
				"🐳 **nginx**",
				"🔄 `nginx:1.26-alpine` → `nginx:1.27-alpine`",
				"🐳 **private-registry**",
				"🔄 `registry.example.com:5000/example/app:1.26-alpine` → `registry.example.com:5000/example/app:1.27-alpine`",
			},
			notContains: []string{
				"alpine...",
			},
		},
		{
			name: "vulnerability summary",
			build: func() string {
				return BuildVulnerabilitySummaryNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					"Daily Summary - 2026-10-07",
					"5 image(s) scanned, 2 with fixable vulnerabilities",
					"7 fixable vulnerability record(s)",
					"Critical:1 High:3 Medium:2 Low:1 Unknown:0",
					"CVE-2025-1234, CVE-2025-5678",
				)
			},
			contains: []string{
				"📊 **Daily Summary - 2026-10-07**",
				"📦 5 image(s) scanned, 2 with fixable vulnerabilities",
				"🛠️ 7 fixable vulnerability record(s)",
				"🚨 Critical:1 • High:3 • Medium:2 • Low:1 • Unknown:0",
				"🔎 `CVE-2025-1234, CVE-2025-5678`",
			},
			notContains: []string{
				"Daily Vulnerability Summary",
				"Severity Breakdown:",
			},
		},
		{
			name: "prune report",
			build: func() string {
				return BuildPruneReportNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					&system.PruneAllResult{
						SpaceReclaimed:           3825205248,
						ContainerSpaceReclaimed:  503316480,
						ImageSpaceReclaimed:      2449473536,
						VolumeSpaceReclaimed:     641728512,
						BuildCacheSpaceReclaimed: 230162432,
					},
				)
			},
			contains: []string{
				"💾 **3.6 GiB** reclaimed",
				"📦 Images: 2.3 GiB",
				"🧱 Containers: 480.0 MiB",
				"💿 Volumes: 612.0 MiB",
				"🛠️ Build cache: 219.5 MiB",
			},
			notContains: []string{
				"System Prune Report",
				"Breakdown:",
			},
		},
		{
			name: "auto heal",
			build: func() string {
				return BuildAutoHealNotificationMessage(
					MessageFormatNtfyMarkdown,
					"Local Docker",
					"jellyfin",
				)
			},
			contains: []string{
				"🐳 **jellyfin**",
				"🔄 Restarted automatically",
				"⚠️ Reason: container was unhealthy",
			},
			notContains: []string{
				"Container Auto-Healed",
				"Status:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := tt.build()

			require.True(
				t,
				strings.HasPrefix(message, "🖥️ Local Docker"),
			)

			for _, expected := range tt.contains {
				require.Contains(t, message, expected)
			}

			for _, unexpected := range tt.notContains {
				require.NotContains(t, message, unexpected)
			}
		})
	}
}

func TestNtfyImageUpdateTagUsesVersions(t *testing.T) {
	message := BuildImageUpdateNotificationMessage(
		MessageFormatNtfyMarkdown,
		"Local Docker",
		"example/app:1.2",
		&imageupdate.Response{
			HasUpdate:      true,
			UpdateType:     "tag",
			CurrentVersion: "1.2.3",
			LatestVersion:  "1.3.0",
			CurrentDigest:  "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			LatestDigest:   "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		},
	)

	require.Contains(t, message, "🔄 `1.2.3` → `1.3.0`")
	require.NotContains(t, message, "sha256:")
}
