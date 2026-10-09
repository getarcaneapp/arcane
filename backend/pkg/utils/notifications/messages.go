package notifications

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/system"
	kit "go.getarcane.app/kit/pkg"
)

type MessageFormat string

const (
	MessageFormatPlain        MessageFormat = "plain"
	MessageFormatMarkdown     MessageFormat = "markdown"
	MessageFormatNtfyMarkdown MessageFormat = "ntfy_markdown"
	MessageFormatSlack        MessageFormat = "slack"
	MessageFormatHTML         MessageFormat = "html"
)

func formatNotificationTitleInternal(format MessageFormat, title string) string {
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

func formatNotificationLabelInternal(format MessageFormat, label string) string {
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

func formatNotificationCodeInternal(format MessageFormat, value string) string {
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

func BuildImageUpdateNotificationMessage(format MessageFormat, environmentName, imageRef string, updateInfo *imageupdate.Response) string {
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyImageUpdateNotificationMessageInternal(environmentName, imageRef, updateInfo)
	}

	updateStatus := kit.Ternary(updateInfo != nil && updateInfo.HasUpdate, "Update Available", "No Update")
	if format != MessageFormatPlain && updateStatus == "Update Available" {
		updateStatus = "⚠️ Update Available"
	}

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "🔔 Container Image Update Notification"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Image"), imageRef)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Status"), updateStatus)
	if updateInfo != nil {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Update Type"), updateInfo.UpdateType)
		if updateInfo.CurrentDigest != "" {
			fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Current Digest"), formatNotificationCodeInternal(format, updateInfo.CurrentDigest))
		}
		if updateInfo.LatestDigest != "" {
			fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Latest Digest"), formatNotificationCodeInternal(format, updateInfo.LatestDigest))
		}
	}

	return message.String()
}

func BuildContainerUpdateNotificationMessage(format MessageFormat, environmentName, containerName, imageRef, oldDigest, newDigest string) string {
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyContainerUpdateNotificationMessageInternal(environmentName, containerName, imageRef, oldDigest, newDigest)
	}

	status := kit.Ternary(format != MessageFormatPlain, "✅ Updated Successfully", "Updated Successfully")

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "✅ Container Successfully Updated"))
	fmt.Fprintf(&message, "Your container has been updated with the latest image version.\n\n")
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Container"), containerName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Image"), imageRef)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Status"), status)
	if oldDigest != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Previous Version"), formatNotificationCodeInternal(format, oldDigest))
	}
	if newDigest != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Current Version"), formatNotificationCodeInternal(format, newDigest))
	}

	return message.String()
}

func BuildBatchImageUpdateNotificationMessage(format MessageFormat, environmentName string, updates map[string]*imageupdate.Response) string {
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyBatchImageUpdateNotificationMessageInternal(environmentName, updates)
	}

	title := "Container Image Updates Available"
	description := fmt.Sprintf("%d container image(s) have updates available.", len(updates))
	if len(updates) == 1 {
		description = "1 container image has an update available."
	}

	imageRefs := slices.Sorted(maps.Keys(updates))

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n%s\n", formatNotificationTitleInternal(format, title), description)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabelInternal(format, "Environment"), environmentName)

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
			fmt.Fprintf(&message, "• **Current:** %s\n", formatNotificationCodeInternal(format, update.CurrentDigest))
			fmt.Fprintf(&message, "• **Latest:** %s\n\n", formatNotificationCodeInternal(format, update.LatestDigest))
		case MessageFormatSlack:
			fmt.Fprintf(&message, "*%s*\n", imageRef)
			fmt.Fprintf(&message, "• *Type:* %s\n", update.UpdateType)
			fmt.Fprintf(&message, "• *Current:* %s\n", formatNotificationCodeInternal(format, update.CurrentDigest))
			fmt.Fprintf(&message, "• *Latest:* %s\n\n", formatNotificationCodeInternal(format, update.LatestDigest))
		case MessageFormatHTML:
			fmt.Fprintf(&message, "<b>%s</b>\n", imageRef)
			fmt.Fprintf(&message, "• <b>Type:</b> %s\n", update.UpdateType)
			fmt.Fprintf(&message, "• <b>Current:</b> %s\n", formatNotificationCodeInternal(format, update.CurrentDigest))
			fmt.Fprintf(&message, "• <b>Latest:</b> %s\n\n", formatNotificationCodeInternal(format, update.LatestDigest))
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
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyBatchContainerUpdateNotificationMessageInternal(environmentName, entries)
	}

	title := "Containers Updated"
	description := fmt.Sprintf("%d container(s) were updated.", len(entries))
	if len(entries) == 1 {
		title = "Container Updated"
		description = "1 container was updated."
	}

	sorted := make([]ContainerUpdateBatchEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ContainerName < sorted[j].ContainerName })

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n%s\n", formatNotificationTitleInternal(format, title), description)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabelInternal(format, "Environment"), environmentName)

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
			fmt.Fprintf(&message, "• Previous Version: %s\n", formatNotificationCodeInternal(format, entry.OldDigest))
		}
		if entry.NewDigest != "" {
			fmt.Fprintf(&message, "• Current Version: %s\n\n", formatNotificationCodeInternal(format, entry.NewDigest))
		} else {
			fmt.Fprintf(&message, "\n")
		}
	}

	return message.String()
}

func BuildVulnerabilitySummaryNotificationMessage(format MessageFormat, environmentName, summaryLabel, overview, fixableCount, severityBreakdown, sampleCVEs string) string {
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyVulnerabilitySummaryNotificationMessageInternal(
			environmentName,
			summaryLabel,
			overview,
			fixableCount,
			severityBreakdown,
			sampleCVEs,
		)
	}

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "📊 Daily Vulnerability Summary"))

	if strings.TrimSpace(summaryLabel) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Summary"), summaryLabel)
	}
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	if strings.TrimSpace(overview) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Overview"), overview)
	}
	if strings.TrimSpace(fixableCount) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Fixable Vulnerabilities"), fixableCount)
	}
	if strings.TrimSpace(severityBreakdown) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Severity Breakdown"), severityBreakdown)
	}
	if strings.TrimSpace(sampleCVEs) != "" {
		fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Sample CVEs"), sampleCVEs)
	}

	return message.String()
}

func BuildPruneReportNotificationMessage(format MessageFormat, environmentName string, result *system.PruneAllResult) string {
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyPruneReportNotificationMessageInternal(environmentName, result)
	}

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "🧹 System Prune Report"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabelInternal(format, "Total Space Reclaimed"), FormatBytes(result.SpaceReclaimed))
	fmt.Fprintf(&message, "%s\n", formatNotificationLabelInternal(format, "Breakdown"))
	fmt.Fprintf(&message, "- Containers: %s\n", FormatBytes(result.ContainerSpaceReclaimed))
	fmt.Fprintf(&message, "- Images: %s\n", FormatBytes(result.ImageSpaceReclaimed))
	fmt.Fprintf(&message, "- Volumes: %s\n", FormatBytes(result.VolumeSpaceReclaimed))
	fmt.Fprintf(&message, "- Build Cache: %s\n", FormatBytes(result.BuildCacheSpaceReclaimed))
	return message.String()
}

func BuildAutoHealNotificationMessage(format MessageFormat, environmentName, containerName string) string {
	if format == MessageFormatNtfyMarkdown {
		return buildNtfyAutoHealNotificationMessageInternal(environmentName, containerName)
	}

	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "Auto Heal"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Container"), containerName)
	fmt.Fprintf(&message, "%s Automatically restarted because it was unhealthy.\n", formatNotificationLabelInternal(format, "Status"))
	return message.String()
}

func shortenNotificationDigestInternal(value string) string {
	trimmed := strings.TrimSpace(value)
	algorithm, digest, found := strings.Cut(trimmed, ":")
	if !found || algorithm == "" || len(digest) <= 6 {
		return trimmed
	}

	return algorithm + ":" + digest[:6] + "..."
}

func ntfyUpdateTransitionInternal(update *imageupdate.Response) (string, string) {
	if update == nil {
		return "", ""
	}

	if update.UpdateType == "tag" && (update.CurrentVersion != "" || update.LatestVersion != "") {
		return update.CurrentVersion, update.LatestVersion
	}

	if update.CurrentDigest != "" || update.LatestDigest != "" {
		return shortenNotificationDigestInternal(update.CurrentDigest),
			shortenNotificationDigestInternal(update.LatestDigest)
	}

	return update.CurrentVersion, update.LatestVersion
}

func writeNtfyTransitionInternal(message *strings.Builder, current, latest, indent string) bool {
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

func byteMagnitudeInternal(bytes uint64) (float64, int) {
	const unit = 1024
	if bytes < unit {
		return float64(bytes), -1
	}

	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return float64(bytes) / float64(div), exp
}

func formatNtfyBytesInternal(bytes uint64) string {
	value, exp := byteMagnitudeInternal(bytes)
	if exp < 0 {
		return fmt.Sprintf("%d B", bytes)
	}

	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	return fmt.Sprintf("%.1f %s", value, units[exp])
}

func buildNtfyImageUpdateNotificationMessageInternal(
	environmentName,
	imageRef string,
	updateInfo *imageupdate.Response,
) string {
	var message strings.Builder

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

	current, latest := ntfyUpdateTransitionInternal(updateInfo)
	if !writeNtfyTransitionInternal(&message, current, latest, "") {
		fmt.Fprintln(&message, "⚠️ Update available")
	}

	return message.String()
}

func buildNtfyContainerUpdateNotificationMessageInternal(
	environmentName,
	containerName,
	imageRef,
	oldDigest,
	newDigest string,
) string {
	var message strings.Builder

	fmt.Fprintf(&message, "🖥️ %s\n\n", environmentName)
	fmt.Fprintf(&message, "🐳 **%s**\n", containerName)
	fmt.Fprintf(&message, "📦 `%s`\n", imageRef)

	if !writeNtfyTransitionInternal(
		&message,
		shortenNotificationDigestInternal(oldDigest),
		shortenNotificationDigestInternal(newDigest),
		"",
	) {
		fmt.Fprintln(&message, "✅ Updated successfully")
	}

	return message.String()
}

func buildNtfyBatchImageUpdateNotificationMessageInternal(
	environmentName string,
	updates map[string]*imageupdate.Response,
) string {
	imageRefs := slices.Sorted(maps.Keys(updates))

	var message strings.Builder
	fmt.Fprintf(&message, "🖥️ %s\n", environmentName)

	for _, imageRef := range imageRefs {
		update := updates[imageRef]

		fmt.Fprintf(&message, "\n📦 **%s**\n", imageRef)

		current, latest := ntfyUpdateTransitionInternal(update)
		if !writeNtfyTransitionInternal(&message, current, latest, "   ") {
			fmt.Fprintln(&message, "   ⚠️ Update available")
		}
	}

	return message.String()
}

func buildNtfyBatchContainerUpdateNotificationMessageInternal(
	environmentName string,
	entries []ContainerUpdateBatchEntry,
) string {
	sorted := make([]ContainerUpdateBatchEntry, len(entries))
	copy(sorted, entries)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ContainerName < sorted[j].ContainerName
	})

	var message strings.Builder
	fmt.Fprintf(&message, "🖥️ %s\n", environmentName)

	for _, entry := range sorted {
		fmt.Fprintf(&message, "\n🐳 **%s**\n", entry.ContainerName)
		fmt.Fprintf(&message, "   📦 `%s`\n", entry.ImageRef)

		if !writeNtfyTransitionInternal(
			&message,
			shortenNotificationDigestInternal(entry.OldDigest),
			shortenNotificationDigestInternal(entry.NewDigest),
			"   ",
		) {
			fmt.Fprintln(&message, "   ✅ Updated successfully")
		}
	}

	return message.String()
}

func buildNtfyVulnerabilitySummaryNotificationMessageInternal(
	environmentName,
	summaryLabel,
	overview,
	fixableCount,
	severityBreakdown,
	sampleCVEs string,
) string {
	var message strings.Builder

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

func buildNtfyPruneReportNotificationMessageInternal(
	environmentName string,
	result *system.PruneAllResult,
) string {
	var message strings.Builder

	fmt.Fprintf(&message, "🖥️ %s\n", environmentName)
	fmt.Fprintf(
		&message,
		"💾 **%s** reclaimed\n\n",
		formatNtfyBytesInternal(result.SpaceReclaimed),
	)
	fmt.Fprintf(&message, "📦 Images: %s\n", formatNtfyBytesInternal(result.ImageSpaceReclaimed))
	fmt.Fprintf(&message, "🧱 Containers: %s\n", formatNtfyBytesInternal(result.ContainerSpaceReclaimed))
	fmt.Fprintf(&message, "💿 Volumes: %s\n", formatNtfyBytesInternal(result.VolumeSpaceReclaimed))
	fmt.Fprintf(&message, "🛠️ Build cache: %s\n", formatNtfyBytesInternal(result.BuildCacheSpaceReclaimed))

	return message.String()
}

func buildNtfyAutoHealNotificationMessageInternal(
	environmentName,
	containerName string,
) string {
	var message strings.Builder

	fmt.Fprintf(&message, "🖥️ %s\n\n", environmentName)
	fmt.Fprintf(&message, "🐳 **%s**\n", containerName)
	fmt.Fprintln(&message, "🔄 Restarted automatically")
	fmt.Fprintln(&message, "⚠️ Reason: container was unhealthy")

	return message.String()
}

func FormatBytes(bytes uint64) string {
	value, exp := byteMagnitudeInternal(bytes)
	if exp < 0 {
		return fmt.Sprintf("%d B", bytes)
	}

	return fmt.Sprintf("%.1f %cB", value, "KMGTPE"[exp])
}

func BuildEmailSubject(environmentName, subject string) string {
	trimmedEnvironmentName := strings.TrimSpace(environmentName)
	if trimmedEnvironmentName == "" {
		return subject
	}

	return fmt.Sprintf("[%s] %s", SanitizeForEmail(trimmedEnvironmentName), subject)
}
