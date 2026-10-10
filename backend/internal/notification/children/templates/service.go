package templates

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
)

const logoURLPath = "/api/app-images/logo-email"

// Service builds the per-event notification content and renders email templates.
type Service struct {
	appURL func() string
}

func NewService(appURL func() string) *Service {
	return &Service{appURL: appURL}
}

func (s *Service) ImageUpdate(environmentName, imageRef string, updateInfo *imageupdate.Response) notifications.Content {
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildImageUpdateNotificationMessage(format, environmentName, imageRef, updateInfo)
		}),
		Title:     "Container Image Update",
		NtfyTitle: "📦 Container Image Update",
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderEmailTemplateInternal(environmentName, imageRef, updateInfo)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
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
			htmlBody, _, err := s.renderContainerUpdateEmailTemplateInternal(environmentName, containerName, imageRef, oldDigest, newDigest)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Container Updated: "+notifications.SanitizeForEmail(containerName))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) Vulnerability(environmentName string, payload notification.DispatchVulnerabilityFound) notifications.Content {
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
			htmlBody, _, err := s.renderVulnerabilitySummaryEmailTemplateInternal(environmentName, payload)
			if err != nil {
				return "", "", fmt.Errorf("failed to render summary email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary: "+notifications.SanitizeForEmail(payload.CVEID))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) BatchImageUpdate(environmentName string, updates map[string]*imageupdate.Response) notifications.Content {
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
			htmlBody, _, err := s.renderBatchEmailTemplateInternal(environmentName, updates)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			updateCount := len(updates)
			plural := kit.Ternary(updateCount > 1, "s", "")
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
			htmlBody, _, err := s.renderBatchContainerUpdateEmailTemplateInternal(environmentName, entries)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			updateCount := len(entries)
			plural := kit.Ternary(updateCount > 1, "s", "")
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("%d Container%s Updated", updateCount, plural))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *Service) PruneReport(environmentName string, result *system.PruneAllResult) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "System Prune Report")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildPruneReportNotificationMessage(format, environmentName, result)
		}),
		Title:        defaultTitle,
		NtfyTitle:    "🧹 System Prune Complete",
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderPruneReportEmailTemplateInternal(environmentName, result)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("System Prune Report: %s Reclaimed", notifications.FormatBytes(result.SpaceReclaimed)))
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
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Auto Heal: Container '%s' Restarted", containerName))
			body := fmt.Sprintf(
				"<p><strong>Environment:</strong> %s</p><p><strong>Container:</strong> %s</p><p>Automatically restarted because it was unhealthy.</p>",
				html.EscapeString(environmentName),
				html.EscapeString(containerName),
			)
			return subject, body, nil
		},
	}
}

// TestEmail builds the provider test email content.
func (s *Service) TestEmail(environmentName string) notifications.Content {
	return notifications.Content{
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderTestEmailTemplateInternal(environmentName)
			if err != nil {
				return "", "", fmt.Errorf("failed to render test email template: %w", err)
			}
			return notifications.BuildEmailSubject(environmentName, "Test Email from Arcane"), htmlBody, nil
		},
	}
}

func (s *Service) renderBatchContainerUpdateEmailTemplateInternal(environmentName string, entries []notifications.ContainerUpdateBatchEntry) (string, string, error) {
	type batchEntry struct {
		ContainerName string
		ImageRef      string
		OldDigest     string
		NewDigest     string
	}

	sorted := make([]batchEntry, 0, len(entries))
	for _, entry := range entries {
		sorted = append(sorted, batchEntry{
			ContainerName: entry.ContainerName,
			ImageRef:      entry.ImageRef,
			OldDigest:     entry.OldDigest,
			NewDigest:     entry.NewDigest,
		})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ContainerName < sorted[j].ContainerName })

	appURL := s.appURL()
	data := map[string]any{
		"LogoURL":     appURL + logoURLPath,
		"AppURL":      appURL,
		"Environment": environmentName,
		"UpdateCount": len(sorted),
		"CompletedAt": time.Now().Format(time.RFC1123),
		"Entries":     sorted,
	}

	return s.renderTemplatesInternal("batch-container-updates", data, true)
}

func (s *Service) renderEmailTemplateInternal(environmentName, imageRef string, updateInfo *imageupdate.Response) (string, string, error) {
	appURL := s.appURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":       logoURL,
		"AppURL":        appURL,
		"Environment":   environmentName,
		"ImageRef":      imageRef,
		"HasUpdate":     updateInfo.HasUpdate,
		"UpdateType":    updateInfo.UpdateType,
		"CurrentDigest": updateInfo.CurrentDigest,
		"LatestDigest":  updateInfo.LatestDigest,
		"CheckTime":     updateInfo.CheckTime.Format(time.RFC1123),
	}

	return s.renderTemplatesInternal("image-update", data, true)
}

func (s *Service) renderContainerUpdateEmailTemplateInternal(environmentName, containerName, imageRef, oldDigest, newDigest string) (string, string, error) {
	appURL := s.appURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":       logoURL,
		"AppURL":        appURL,
		"Environment":   environmentName,
		"ContainerName": containerName,
		"ImageRef":      imageRef,
		"OldDigest":     oldDigest,
		"NewDigest":     newDigest,
		"UpdateTime":    time.Now().Format(time.RFC1123),
	}

	return s.renderTemplatesInternal("container-update", data, true)
}

func (s *Service) renderTestEmailTemplateInternal(environmentName string) (string, string, error) {
	appURL := s.appURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":     logoURL,
		"AppURL":      appURL,
		"Environment": environmentName,
	}

	return s.renderTemplatesInternal("test", data, true)
}

func (s *Service) renderBatchEmailTemplateInternal(environmentName string, updates map[string]*imageupdate.Response) (string, string, error) {
	// Build list of image names
	imageList := slices.Collect(maps.Keys(updates))

	appURL := s.appURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":     logoURL,
		"AppURL":      appURL,
		"Environment": environmentName,
		"UpdateCount": len(updates),
		"CheckTime":   time.Now().Format(time.RFC1123),
		"ImageList":   imageList,
	}

	return s.renderTemplatesInternal("batch-image-updates", data, true)
}

func (s *Service) renderVulnerabilitySummaryEmailTemplateInternal(environmentName string, payload notification.DispatchVulnerabilityFound) (string, string, error) {
	appURL := s.appURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":           logoURL,
		"AppURL":            appURL,
		"Environment":       environmentName,
		"SummaryLabel":      payload.CVEID,
		"Overview":          payload.ImageName,
		"FixableCount":      payload.FixedVersion,
		"SeverityBreakdown": payload.Severity,
		"SampleCVEs":        payload.PkgName,
	}

	return s.renderTemplatesInternal("vulnerability-summary", data, true)
}

func (s *Service) renderPruneReportEmailTemplateInternal(environmentName string, result *system.PruneAllResult) (string, string, error) {
	appURL := s.appURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":                  logoURL,
		"AppURL":                   appURL,
		"Environment":              environmentName,
		"TotalSpaceReclaimed":      notifications.FormatBytes(result.SpaceReclaimed),
		"ContainerSpaceReclaimed":  notifications.FormatBytes(result.ContainerSpaceReclaimed),
		"ImageSpaceReclaimed":      notifications.FormatBytes(result.ImageSpaceReclaimed),
		"VolumeSpaceReclaimed":     notifications.FormatBytes(result.VolumeSpaceReclaimed),
		"BuildCacheSpaceReclaimed": notifications.FormatBytes(result.BuildCacheSpaceReclaimed),
		"Time":                     time.Now().Format(time.RFC1123),
	}

	return s.renderTemplatesInternal("prune-report", data, false)
}

func (s *Service) renderTemplatesInternal(name string, data any, textRequired bool) (string, string, error) {
	htmlContent, err := resources.FS.ReadFile(fmt.Sprintf("email-templates/%s_html.tmpl", name))
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var htmlBuf bytes.Buffer
	if executeTemplateErr := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); executeTemplateErr != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", executeTemplateErr)
	}

	textContent, err := resources.FS.ReadFile(fmt.Sprintf("email-templates/%s_text.tmpl", name))
	if err != nil {
		if textRequired {
			return "", "", fmt.Errorf("failed to read text template: %w", err)
		}
		return htmlBuf.String(), "", nil
	}
	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		if textRequired {
			return "", "", fmt.Errorf("failed to parse text template: %w", err)
		}
		return htmlBuf.String(), "", nil
	}
	var textBuf bytes.Buffer
	if executeTextTemplateErr := textTmpl.ExecuteTemplate(&textBuf, "root", data); executeTextTemplateErr != nil {
		if textRequired {
			return "", "", fmt.Errorf("failed to execute text template: %w", executeTextTemplateErr)
		}
		return htmlBuf.String(), "", nil
	}
	return htmlBuf.String(), textBuf.String(), nil
}
