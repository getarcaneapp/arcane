package notifications

import (
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/go-units"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"
)

type MessageFormat string

const (
	MessageFormatPlain        MessageFormat = "plain"
	MessageFormatMarkdown     MessageFormat = "markdown"
	MessageFormatNtfyMarkdown MessageFormat = "ntfy_markdown"
	MessageFormatSlack        MessageFormat = "slack"
	MessageFormatHTML         MessageFormat = "html"
)

func formatNotificationTitle(format MessageFormat, title string) string {
	switch format {
	case MessageFormatPlain:
		return title
	case MessageFormatMarkdown:
		return fmt.Sprintf("**%s**", title)
	case MessageFormatSlack:
		return fmt.Sprintf("*%s*", title)
	case MessageFormatHTML:
		return fmt.Sprintf("<b>%s</b>", title)
	default:
		return title
	}
}

func formatNotificationLabel(format MessageFormat, label string) string {
	switch format {
	case MessageFormatPlain:
		return label + ":"
	case MessageFormatMarkdown:
		return fmt.Sprintf("**%s:**", label)
	case MessageFormatSlack:
		return fmt.Sprintf("*%s:*", label)
	case MessageFormatHTML:
		return fmt.Sprintf("<b>%s:</b>", label)
	default:
		return label + ":"
	}
}

func formatNotificationCode(format MessageFormat, value string) string {
	switch format {
	case MessageFormatPlain:
		return value
	case MessageFormatHTML:
		return fmt.Sprintf("<code>%s</code>", value)
	case MessageFormatMarkdown, MessageFormatSlack:
		return "`" + value + "`"
	default:
		return value
	}
}

func BuildImageUpdateNotificationMessage(format MessageFormat, environmentName, imageRef string, updateInfo *types.ImageUpdateResponse) string {
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n\n", environmentName)
		fmt.Fprintf(&message, "📦 **%s**\n", imageRef)

		if updateInfo == nil {
			fmt.Fprintln(&message, "ℹ️ Update details unavailable")
			return message.String()
		}

		if !updateInfo.HasUpdate {
			fmt.Fprintln(&message, "✅ Already up to date")
			return message.String()
		}

		current, latest := updateInfo.CurrentVersion, updateInfo.LatestVersion
		if (updateInfo.UpdateType != "tag" || (current == "" && latest == "")) && (updateInfo.CurrentDigest != "" || updateInfo.LatestDigest != "") {
			current = shortenNotificationDigest(updateInfo.CurrentDigest)
			latest = shortenNotificationDigest(updateInfo.LatestDigest)
		}
		if !writeNtfyTransition(&message, current, latest, "") {
			fmt.Fprintln(&message, "⚠️ Update available")
		}

		return message.String()
	}

	updateStatus := kit.Ternary(updateInfo != nil && updateInfo.HasUpdate, "Update Available", "No Update")
	if format != MessageFormatPlain && updateStatus == "Update Available" {
		updateStatus = "⚠️ Update Available"
	}

	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitle(format, "🔔 Container Image Update Notification"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Image"), imageRef)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Status"), updateStatus)
	if updateInfo != nil {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Update Type"), updateInfo.UpdateType)
		if updateInfo.CurrentDigest != "" {
			fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Current Digest"), formatNotificationCode(format, updateInfo.CurrentDigest))
		}
		if updateInfo.LatestDigest != "" {
			fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Latest Digest"), formatNotificationCode(format, updateInfo.LatestDigest))
		}
	}

	return message.String()
}

func BuildContainerUpdateNotificationMessage(format MessageFormat, environmentName, containerName, imageRef, oldDigest, newDigest string) string {
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n\n", environmentName)
		fmt.Fprintf(&message, "🐳 **%s**\n", containerName)
		fmt.Fprintf(&message, "📦 `%s`\n", imageRef)

		if !writeNtfyTransition(
			&message,
			shortenNotificationDigest(oldDigest),
			shortenNotificationDigest(newDigest),
			"",
		) {
			fmt.Fprintln(&message, "✅ Updated successfully")
		}

		return message.String()
	}

	status := kit.Ternary(format != MessageFormatPlain, "✅ Updated Successfully", "Updated Successfully")

	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitle(format, "✅ Container Successfully Updated"))
	fmt.Fprintf(&message, "Your container has been updated with the latest image version.\n\n")
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Container"), containerName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Image"), imageRef)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Status"), status)
	if oldDigest != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Previous Version"), formatNotificationCode(format, oldDigest))
	}
	if newDigest != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Current Version"), formatNotificationCode(format, newDigest))
	}

	return message.String()
}

func BuildBatchImageUpdateNotificationMessage(format MessageFormat, environmentName string, updates map[string]*types.ImageUpdateResponse) string {
	imageRefs := slices.Sorted(maps.Keys(updates))
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n", environmentName)

		for _, imageRef := range imageRefs {
			update := updates[imageRef]

			fmt.Fprintf(&message, "\n📦 **%s**\n", imageRef)

			var current, latest string
			if update != nil {
				current, latest = update.CurrentVersion, update.LatestVersion
				if (update.UpdateType != "tag" || (current == "" && latest == "")) && (update.CurrentDigest != "" || update.LatestDigest != "") {
					current = shortenNotificationDigest(update.CurrentDigest)
					latest = shortenNotificationDigest(update.LatestDigest)
				}
			}
			if !writeNtfyTransition(&message, current, latest, "   ") {
				fmt.Fprintln(&message, "   ⚠️ Update available")
			}
		}

		return message.String()
	}

	title := "Container Image Updates Available"
	description := fmt.Sprintf("%d container image(s) have updates available.", len(updates))
	if len(updates) == 1 {
		description = "1 container image has an update available."
	}

	fmt.Fprintf(&message, "%s\n\n%s\n", formatNotificationTitle(format, title), description)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabel(format, "Environment"), environmentName)

	for _, imageRef := range imageRefs {
		update := updates[imageRef]
		switch format {
		case MessageFormatPlain:
			fmt.Fprintf(&message, "%s\n", imageRef)
			fmt.Fprintf(&message, "• Type: %s\n", update.UpdateType)
			fmt.Fprintf(&message, "• Current: %s\n", update.CurrentDigest)
			fmt.Fprintf(&message, "• Latest: %s\n\n", update.LatestDigest)
		case MessageFormatMarkdown:
			fmt.Fprintf(&message, "**%s**\n", imageRef)
			fmt.Fprintf(&message, "• **Type:** %s\n", update.UpdateType)
			fmt.Fprintf(&message, "• **Current:** %s\n", formatNotificationCode(format, update.CurrentDigest))
			fmt.Fprintf(&message, "• **Latest:** %s\n\n", formatNotificationCode(format, update.LatestDigest))
		case MessageFormatSlack:
			fmt.Fprintf(&message, "*%s*\n", imageRef)
			fmt.Fprintf(&message, "• *Type:* %s\n", update.UpdateType)
			fmt.Fprintf(&message, "• *Current:* %s\n", formatNotificationCode(format, update.CurrentDigest))
			fmt.Fprintf(&message, "• *Latest:* %s\n\n", formatNotificationCode(format, update.LatestDigest))
		case MessageFormatHTML:
			fmt.Fprintf(&message, "<b>%s</b>\n", imageRef)
			fmt.Fprintf(&message, "• <b>Type:</b> %s\n", update.UpdateType)
			fmt.Fprintf(&message, "• <b>Current:</b> %s\n", formatNotificationCode(format, update.CurrentDigest))
			fmt.Fprintf(&message, "• <b>Latest:</b> %s\n\n", formatNotificationCode(format, update.LatestDigest))
		}
	}

	return message.String()
}

// ContainerUpdateBatchEntry describes one container updated as part of a batch.
type ContainerUpdateBatchEntry struct {
	ContainerName string
	ImageRef      string
	OldDigest     string
	NewDigest     string
}

// BuildBatchContainerUpdateNotificationMessage builds a formatted notification for a batch of container updates.
func BuildBatchContainerUpdateNotificationMessage(format MessageFormat, environmentName string, entries []ContainerUpdateBatchEntry) string {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b ContainerUpdateBatchEntry) int { return strings.Compare(a.ContainerName, b.ContainerName) })
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n", environmentName)

		for _, entry := range sorted {
			fmt.Fprintf(&message, "\n🐳 **%s**\n", entry.ContainerName)
			fmt.Fprintf(&message, "   📦 `%s`\n", entry.ImageRef)

			if !writeNtfyTransition(
				&message,
				shortenNotificationDigest(entry.OldDigest),
				shortenNotificationDigest(entry.NewDigest),
				"   ",
			) {
				fmt.Fprintln(&message, "   ✅ Updated successfully")
			}
		}

		return message.String()
	}

	title := "Containers Updated"
	description := fmt.Sprintf("%d container(s) were updated.", len(entries))
	if len(entries) == 1 {
		title = "Container Updated"
		description = "1 container was updated."
	}

	fmt.Fprintf(&message, "%s\n\n%s\n", formatNotificationTitle(format, title), description)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabel(format, "Environment"), environmentName)

	for _, entry := range sorted {
		switch format {
		case MessageFormatPlain:
			fmt.Fprintf(&message, "%s\n", entry.ContainerName)
		case MessageFormatMarkdown:
			fmt.Fprintf(&message, "**%s**\n", entry.ContainerName)
		case MessageFormatSlack:
			fmt.Fprintf(&message, "*%s*\n", entry.ContainerName)
		case MessageFormatHTML:
			fmt.Fprintf(&message, "<b>%s</b>\n", entry.ContainerName)
		}
		fmt.Fprintf(&message, "• Image: %s\n", entry.ImageRef)
		if entry.OldDigest != "" {
			fmt.Fprintf(&message, "• Previous Version: %s\n", formatNotificationCode(format, entry.OldDigest))
		}
		if entry.NewDigest != "" {
			fmt.Fprintf(&message, "• Current Version: %s\n\n", formatNotificationCode(format, entry.NewDigest))
		} else {
			fmt.Fprintf(&message, "\n")
		}
	}

	return message.String()
}

func BuildVulnerabilitySummaryNotificationMessage(format MessageFormat, environmentName, summaryLabel, overview, fixableCount, severityBreakdown, sampleCVEs string) string {
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n", environmentName)

		if strings.TrimSpace(summaryLabel) != "" {
			fmt.Fprintf(&message, "\n📊 **%s**\n", summaryLabel)
		}
		if strings.TrimSpace(overview) != "" {
			fmt.Fprintf(&message, "📦 %s\n", overview)
		}
		if strings.TrimSpace(fixableCount) != "" {
			fmt.Fprintf(&message, "🛠️ %s\n", fixableCount)
		}
		if strings.TrimSpace(severityBreakdown) != "" {
			severityItems := strings.Fields(severityBreakdown)
			fmt.Fprintf(&message, "🚨 %s\n", strings.Join(severityItems, " • "))
		}
		if strings.TrimSpace(sampleCVEs) != "" {
			fmt.Fprintf(&message, "🔎 `%s`\n", sampleCVEs)
		}

		return message.String()
	}

	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitle(format, "📊 Daily Vulnerability Summary"))

	if strings.TrimSpace(summaryLabel) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Summary"), summaryLabel)
	}
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Environment"), environmentName)
	if strings.TrimSpace(overview) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Overview"), overview)
	}
	if strings.TrimSpace(fixableCount) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Fixable Vulnerabilities"), fixableCount)
	}
	if strings.TrimSpace(severityBreakdown) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Severity Breakdown"), severityBreakdown)
	}
	if strings.TrimSpace(sampleCVEs) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Sample CVEs"), sampleCVEs)
	}

	return message.String()
}

func BuildPruneReportNotificationMessage(format MessageFormat, environmentName string, result *types.PruneAllResult) string {
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n", environmentName)
		fmt.Fprintf(
			&message,
			"💾 **%s** reclaimed\n\n",
			FormatBytes(result.SpaceReclaimed),
		)
		fmt.Fprintf(&message, "📦 Images: %s\n", FormatBytes(result.ImageSpaceReclaimed))
		fmt.Fprintf(&message, "🧱 Containers: %s\n", FormatBytes(result.ContainerSpaceReclaimed))
		fmt.Fprintf(&message, "💿 Volumes: %s\n", FormatBytes(result.VolumeSpaceReclaimed))
		fmt.Fprintf(&message, "🛠️ Build cache: %s\n", FormatBytes(result.BuildCacheSpaceReclaimed))
		if len(result.Errors) > 0 {
			fmt.Fprintf(&message, "\n⚠️ **%d error(s)**\n", len(result.Errors))
			for _, pruneErr := range result.Errors {
				fmt.Fprintf(&message, "- %s\n", pruneErr)
			}
		}

		return message.String()
	}

	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitle(format, "🧹 System Prune Report"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabel(format, "Total Space Reclaimed"), FormatBytes(result.SpaceReclaimed))
	fmt.Fprintf(&message, "%s\n", formatNotificationLabel(format, "Breakdown"))
	fmt.Fprintf(&message, "- Containers: %s\n", FormatBytes(result.ContainerSpaceReclaimed))
	fmt.Fprintf(&message, "- Images: %s\n", FormatBytes(result.ImageSpaceReclaimed))
	fmt.Fprintf(&message, "- Volumes: %s\n", FormatBytes(result.VolumeSpaceReclaimed))
	fmt.Fprintf(&message, "- Build Cache: %s\n", FormatBytes(result.BuildCacheSpaceReclaimed))
	if len(result.Errors) > 0 {
		fmt.Fprintf(&message, "\n%s\n", formatNotificationLabel(format, "Errors"))
		for _, pruneErr := range result.Errors {
			fmt.Fprintf(&message, "- %s\n", pruneErr)
		}
	}
	return message.String()
}

func BuildAutoHealNotificationMessage(format MessageFormat, environmentName, containerName string) string {
	var message strings.Builder

	if format == MessageFormatNtfyMarkdown {
		fmt.Fprintf(&message, "🖥️ %s\n\n", environmentName)
		fmt.Fprintf(&message, "🐳 **%s**\n", containerName)
		fmt.Fprintln(&message, "🔄 Restarted automatically")
		fmt.Fprintln(&message, "⚠️ Reason: container was unhealthy")

		return message.String()
	}

	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitle(format, "Auto Heal"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabel(format, "Container"), containerName)
	fmt.Fprintf(&message, "%s Automatically restarted because it was unhealthy.\n", formatNotificationLabel(format, "Status"))
	return message.String()
}

func shortenNotificationDigest(value string) string {
	trimmed := strings.TrimSpace(value)
	algorithm, digest, found := strings.Cut(trimmed, ":")
	if !found {
		return trimmed
	}

	var expectedLength int
	switch algorithm {
	case "sha256":
		expectedLength = 64
	case "sha384":
		expectedLength = 96
	case "sha512":
		expectedLength = 128
	default:
		return trimmed
	}
	if len(digest) != expectedLength {
		return trimmed
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return trimmed
	}

	return algorithm + ":" + digest[:6] + "..."
}

func writeNtfyTransition(message *strings.Builder, current, latest, indent string) bool {
	if current == "" && latest == "" {
		return false
	}

	switch {
	case current != "" && latest != "":
		fmt.Fprintf(
			message,
			"%s🔄 `%s` → `%s`\n",
			indent,
			current,
			latest,
		)
	case latest != "":
		fmt.Fprintf(message, "%s🔄 `%s`\n", indent, latest)
	default:
		fmt.Fprintf(message, "%s🔄 `%s`\n", indent, current)
	}

	return true
}

func FormatBytes(bytes uint64) string {
	if bytes < units.KiB {
		return fmt.Sprintf("%d B", bytes)
	}

	return units.CustomSize("%.1f %s", float64(bytes), units.KiB, []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"})
}

func BuildEmailSubject(environmentName, subject string) string {
	trimmedEnvironmentName := strings.TrimSpace(environmentName)
	if trimmedEnvironmentName == "" {
		return subject
	}

	return fmt.Sprintf("[%s] %s", SanitizeForEmail(trimmedEnvironmentName), subject)
}
