package notification

import (
	"bytes"
	"fmt"
	"html/template"
	"sort"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/system"
)

func (s *NotificationService) renderBatchContainerUpdateEmailTemplateInternal(environmentName string, entries []notifications.ContainerUpdateBatchEntry) (string, string, error) {
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

	appURL := s.config.GetAppURL()
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

func (s *NotificationService) renderEmailTemplateInternal(environmentName, imageRef string, updateInfo *imageupdate.Response) (string, string, error) {
	appURL := s.config.GetAppURL()
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

func (s *NotificationService) renderContainerUpdateEmailTemplateInternal(environmentName, containerName, imageRef, oldDigest, newDigest string) (string, string, error) {
	appURL := s.config.GetAppURL()
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

func (s *NotificationService) renderTestEmailTemplateInternal(environmentName string) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":     logoURL,
		"AppURL":      appURL,
		"Environment": environmentName,
	}

	return s.renderTemplatesInternal("test", data, true)
}

func (s *NotificationService) renderBatchEmailTemplateInternal(environmentName string, updates map[string]*imageupdate.Response) (string, string, error) {
	// Build list of image names
	imageList := make([]string, 0, len(updates))
	for imageRef := range updates {
		imageList = append(imageList, imageRef)
	}

	appURL := s.config.GetAppURL()
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

func (s *NotificationService) renderVulnerabilitySummaryEmailTemplateInternal(environmentName string, payload VulnerabilityNotificationPayload) (string, string, error) {
	appURL := s.config.GetAppURL()
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

func (s *NotificationService) renderPruneReportEmailTemplateInternal(environmentName string, result *system.PruneAllResult) (string, string, error) {
	appURL := s.config.GetAppURL()
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

func (s *NotificationService) renderTemplatesInternal(name string, data any, textRequired bool) (string, string, error) {
	htmlContent, err := resources.FS.ReadFile(fmt.Sprintf("email-templates/%s_html.tmpl", name))
	if err != nil {
		return "", "", errors.WrapIf(err, "failed to read HTML template")
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", errors.WrapIf(err, "failed to parse HTML template")
	}

	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", errors.WrapIf(err, "failed to execute HTML template")
	}

	textContent, err := resources.FS.ReadFile(fmt.Sprintf("email-templates/%s_text.tmpl", name))
	if err != nil {
		if textRequired {
			return "", "", errors.WrapIf(err, "failed to read text template")
		}
		return htmlBuf.String(), "", nil
	}
	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		if textRequired {
			return "", "", errors.WrapIf(err, "failed to parse text template")
		}
		return htmlBuf.String(), "", nil
	}
	var textBuf bytes.Buffer
	if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err != nil {
		if textRequired {
			return "", "", errors.WrapIf(err, "failed to execute text template")
		}
		return htmlBuf.String(), "", nil
	}
	return htmlBuf.String(), textBuf.String(), nil
}
