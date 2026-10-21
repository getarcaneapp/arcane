package templates

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

// Service builds the per-event notification content and renders email templates.
type Service struct {
	appURL func() string
}

func NewService(appURL func() string) *Service {
	return &Service{appURL: appURL}
}

func (s *Service) ImageUpdate(environmentName, imageRef string, updateInfo *types.ImageUpdateResponse) notifications.Content {
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildImageUpdateNotificationMessage(format, environmentName, imageRef, updateInfo)
		}),
		Title:     "Container Image Update",
		NtfyTitle: "📦 Container Image Update",
		RenderEmail: func() (string, string, error) {
			htmlBody, err := renderEmail(emailData{
				Title: "Container Image Update Available",
				Rows: []emailRow{
					{Label: "Environment", Value: environmentName},
					{Label: "Image", Value: imageRef},
					{Label: "Update Type", Value: updateInfo.UpdateType},
					{Label: "Current Digest", Value: updateInfo.CurrentDigest},
					{Label: "Latest Digest", Value: updateInfo.LatestDigest},
					{Label: "Checked", Value: updateInfo.CheckTime.Format(time.RFC1123)},
				},
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Container Update Available: "+notifications.SanitizeForEmail(imageRef))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) ContainerUpdate(environmentName, containerName, imageRef, oldDigest, newDigest string) notifications.Content {
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildContainerUpdateNotificationMessage(format, environmentName, containerName, imageRef, oldDigest, newDigest)
		}),
		Title:     "Container Updated",
		NtfyTitle: "✅ Container Updated",
		RenderEmail: func() (string, string, error) {
			htmlBody, err := renderEmail(emailData{
				Title: "Container Updated",
				Rows: []emailRow{
					{Label: "Environment", Value: environmentName},
					{Label: "Container", Value: containerName},
					{Label: "Image", Value: imageRef},
					{Label: "Old Digest", Value: oldDigest},
					{Label: "New Digest", Value: newDigest},
					{Label: "Updated", Value: time.Now().Format(time.RFC1123)},
				},
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Container Updated: "+notifications.SanitizeForEmail(containerName))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) Vulnerability(environmentName string, payload types.NotificationDispatchVulnerabilityFound) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildVulnerabilitySummaryNotificationMessage(
				format,
				environmentName,
				payload.CVEID,
				payload.ImageName,
				payload.FixedVersion,
				payload.Severity,
				payload.PkgName,
			)
		}),
		Title:        defaultTitle,
		NtfyTitle:    "🛡️ Daily Vulnerability Summary",
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			htmlBody, err := renderEmail(emailData{
				Title: "Daily Vulnerability Summary",
				Intro: payload.CVEID,
				Rows: []emailRow{
					{Label: "Environment", Value: environmentName},
					{Label: "Overview", Value: payload.ImageName},
					{Label: "Fixable Vulnerabilities", Value: payload.FixedVersion},
					{Label: "Severity Breakdown", Value: payload.Severity},
					{Label: "Sample CVEs", Value: payload.PkgName},
				},
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render summary email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary: "+notifications.SanitizeForEmail(payload.CVEID))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) BatchImageUpdate(environmentName string, updates map[string]*types.ImageUpdateResponse) notifications.Content {
	ntfyTitle := fmt.Sprintf("📦 %d Container Image Updates", len(updates))
	if len(updates) == 1 {
		ntfyTitle = "📦 1 Container Image Update"
	}

	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildBatchImageUpdateNotificationMessage(format, environmentName, updates)
		}),
		Title:     "Container Image Updates Available",
		NtfyTitle: ntfyTitle,
		RenderEmail: func() (string, string, error) {
			rows := []emailRow{{Label: "Environment", Value: environmentName}}
			for _, imageRef := range slices.Sorted(maps.Keys(updates)) {
				rows = append(rows, emailRow{
					Label: imageRef,
					Value: updates[imageRef].UpdateType,
				})
			}
			updateCount := len(updates)
			plural := kit.Ternary(updateCount > 1, "s", "")
			htmlBody, err := renderEmail(emailData{
				Title:  fmt.Sprintf("%d Image Update%s Available", updateCount, plural),
				Intro:  "Checked " + time.Now().Format(time.RFC1123),
				Rows:   rows,
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("%d Image Update%s Available", updateCount, plural))
			return subject, htmlBody, nil
		},
	}
}

func (s *Service) BatchContainerUpdate(environmentName string, entries []notifications.ContainerUpdateBatchEntry) notifications.Content {
	ntfyTitle := fmt.Sprintf("✅ %d Containers Updated", len(entries))
	if len(entries) == 1 {
		ntfyTitle = "✅ 1 Container Updated"
	}

	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildBatchContainerUpdateNotificationMessage(format, environmentName, entries)
		}),
		Title:     "Containers Updated",
		NtfyTitle: ntfyTitle,
		RenderEmail: func() (string, string, error) {
			sorted := slices.SortedFunc(slices.Values(entries), func(a, b notifications.ContainerUpdateBatchEntry) int {
				return cmp.Compare(a.ContainerName, b.ContainerName)
			})
			rows := []emailRow{{Label: "Environment", Value: environmentName}}
			for _, entry := range sorted {
				rows = append(rows, emailRow{
					Label: entry.ContainerName,
					Value: fmt.Sprintf("%s (%s → %s)", entry.ImageRef, entry.OldDigest, entry.NewDigest),
				})
			}
			updateCount := len(entries)
			plural := kit.Ternary(updateCount > 1, "s", "")
			htmlBody, err := renderEmail(emailData{
				Title:  fmt.Sprintf("%d Container%s Updated", updateCount, plural),
				Intro:  "Completed " + time.Now().Format(time.RFC1123),
				Rows:   rows,
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("%d Container%s Updated", updateCount, plural))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) PruneReport(environmentName string, result *types.PruneAllResult) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "System Prune Report")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildPruneReportNotificationMessage(format, environmentName, result)
		}),
		Title:        defaultTitle,
		NtfyTitle:    kit.Ternary(len(result.Errors) > 0, "⚠️ System Prune Finished with Errors", "🧹 System Prune Complete"),
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			htmlBody, err := renderEmail(emailData{
				Title: "System Prune Report",
				Rows: []emailRow{
					{Label: "Environment", Value: environmentName},
					{Label: "Status", Value: kit.Ternary(len(result.Errors) > 0, "Finished with errors: "+strings.Join(result.Errors, "; "), "Completed")},
					{Label: "Total Reclaimed", Value: notifications.FormatBytes(result.SpaceReclaimed)},
					{Label: "Containers", Value: notifications.FormatBytes(result.ContainerSpaceReclaimed)},
					{Label: "Images", Value: notifications.FormatBytes(result.ImageSpaceReclaimed)},
					{Label: "Volumes", Value: notifications.FormatBytes(result.VolumeSpaceReclaimed)},
					{Label: "Build Cache", Value: notifications.FormatBytes(result.BuildCacheSpaceReclaimed)},
					{Label: "Time", Value: time.Now().Format(time.RFC1123)},
				},
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf(
				"System Prune Report: %s Reclaimed%s",
				notifications.FormatBytes(result.SpaceReclaimed),
				kit.Ternary(len(result.Errors) > 0, fmt.Sprintf(", %d Error(s)", len(result.Errors)), ""),
			))
			return subject, htmlBody, nil
		},
	}
}

func (s *Service) AutoHeal(environmentName, containerName string) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "Auto Heal")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildAutoHealNotificationMessage(format, environmentName, containerName)
		}),
		Title:        defaultTitle,
		NtfyTitle:    "❤️‍🩹 Container Auto-Healed",
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			htmlBody, err := renderEmail(emailData{
				Title: "Container Auto-Healed",
				Intro: "Automatically restarted because it was unhealthy.",
				Rows: []emailRow{
					{Label: "Environment", Value: environmentName},
					{Label: "Container", Value: containerName},
				},
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render email: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Auto Heal: Container '%s' Restarted", containerName))
			return subject, htmlBody, nil
		},
	}
}

// TestEmail builds the provider test email content.
func (s *Service) TestEmail(environmentName string) notifications.Content {
	return notifications.Content{
		RenderEmail: func() (string, string, error) {
			htmlBody, err := renderEmail(emailData{
				Title:  "Test Email",
				Intro:  "Your email setup is working correctly!",
				Rows:   []emailRow{{Label: "Environment", Value: environmentName}},
				AppURL: s.appURL(),
			})
			if err != nil {
				return "", "", fmt.Errorf("failed to render test email: %w", err)
			}
			return notifications.BuildEmailSubject(environmentName, "Test Email from Arcane"), htmlBody, nil
		},
	}
}
