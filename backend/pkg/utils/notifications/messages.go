package notifications

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getarcaneapp/arcane/types/imageupdate"
	"github.com/getarcaneapp/arcane/types/system"
)

type MessageFormat string

const (
	MessageFormatPlain    MessageFormat = "plain"
	MessageFormatMarkdown MessageFormat = "markdown"
	MessageFormatSlack    MessageFormat = "slack"
	MessageFormatHTML     MessageFormat = "html"
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
		return fmt.Sprintf("`%s`", value)
	default:
		return value
	}
}

func BuildImageUpdateNotificationMessage(format MessageFormat, environmentName, imageRef string, updateInfo *imageupdate.Response) string {
	updateStatus := "No Update"
	if updateInfo != nil && updateInfo.HasUpdate {
		updateStatus = "Update Available"
	}
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
	status := "Updated Successfully"
	if format != MessageFormatPlain {
		status = "✅ Updated Successfully"
	}

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
	title := "Container Image Updates Available"
	description := fmt.Sprintf("%d container image(s) have updates available.", len(updates))
	if len(updates) == 1 {
		description = "1 container image has an update available."
	}

	imageRefs := make([]string, 0, len(updates))
	for imageRef := range updates {
		imageRefs = append(imageRefs, imageRef)
	}
	sort.Strings(imageRefs)

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

func BuildVulnerabilitySummaryNotificationMessage(format MessageFormat, environmentName, summaryLabel, overview, fixableCount, severityBreakdown, sampleCVEs string) string {
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
	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "🧹 System Prune Report"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n\n", formatNotificationLabelInternal(format, "Total Space Reclaimed"), formatBytesInternal(result.SpaceReclaimed))
	fmt.Fprintf(&message, "%s\n", formatNotificationLabelInternal(format, "Breakdown"))
	fmt.Fprintf(&message, "- Containers: %s\n", formatBytesInternal(result.ContainerSpaceReclaimed))
	fmt.Fprintf(&message, "- Images: %s\n", formatBytesInternal(result.ImageSpaceReclaimed))
	fmt.Fprintf(&message, "- Volumes: %s\n", formatBytesInternal(result.VolumeSpaceReclaimed))
	fmt.Fprintf(&message, "- Build Cache: %s\n", formatBytesInternal(result.BuildCacheSpaceReclaimed))
	return message.String()
}

func BuildAutoHealNotificationMessage(format MessageFormat, environmentName, containerName string) string {
	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", formatNotificationTitleInternal(format, "Auto Heal"))
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Environment"), environmentName)
	fmt.Fprintf(&message, "%s %s\n", formatNotificationLabelInternal(format, "Container"), containerName)
	fmt.Fprintf(&message, "%s Automatically restarted because it was unhealthy.\n", formatNotificationLabelInternal(format, "Status"))
	return message.String()
}

func formatBytesInternal(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func BuildEmailSubject(environmentName, subject string) string {
	trimmedEnvironmentName := strings.TrimSpace(environmentName)
	if trimmedEnvironmentName == "" {
		return subject
	}

	return fmt.Sprintf("[%s] %s", SanitizeForEmail(trimmedEnvironmentName), subject)
}
