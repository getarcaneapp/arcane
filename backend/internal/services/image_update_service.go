package services

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	registry "github.com/getarcaneapp/arcane/backend/internal/utils/registry"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	ref "go.podman.io/image/v5/docker/reference"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type ImageUpdateService struct {
	db                  *database.DB
	settingsService     *SettingsService
	registryService     *ContainerRegistryService
	dockerService       *DockerClientService
	eventService        *EventService
	notificationService *NotificationService
}

type ImageParts struct {
	Registry   string
	Repository string
	Tag        string
}

type localImageSnapshot struct {
	ImageID       string
	Repository    string
	Tag           string
	PrimaryDigest string
	AllDigests    []string
}

func NewImageUpdateService(db *database.DB, settingsService *SettingsService, registryService *ContainerRegistryService, dockerService *DockerClientService, eventService *EventService, notificationService *NotificationService) *ImageUpdateService {
	return &ImageUpdateService{
		db:                  db,
		settingsService:     settingsService,
		registryService:     registryService,
		dockerService:       dockerService,
		eventService:        eventService,
		notificationService: notificationService,
	}
}

func (s *ImageUpdateService) CheckImageUpdate(ctx context.Context, imageRef string) (*imageupdate.Response, error) {
	startTime := time.Now()

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		return &imageupdate.Response{
			Error:          "Invalid image reference format",
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
		}, nil
	}

	digestResult, snapshot, err := s.checkDigestUpdateWithSnapshotInternal(ctx, parts)
	if err != nil {
		result := &imageupdate.Response{
			Error:          err.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
		}
		metadata := models.JSON{
			"action":    "check_update",
			"imageRef":  imageRef,
			"error":     err.Error(),
			"checkType": "digest",
		}
		if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageScan, "", imageRef, systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check error event", "imageRef", imageRef, "error", logErr.Error())
		}
		if saveErr := s.saveUpdateResultWithSnapshotInternal(ctx, imageRef, result, snapshot); saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
		}
		return result, err
	}

	digestResult.ResponseTimeMs = int(time.Since(startTime).Milliseconds())
	metadata := models.JSON{
		"action":         "check_update",
		"imageRef":       imageRef,
		"hasUpdate":      digestResult.HasUpdate,
		"updateType":     "digest",
		"currentDigest":  digestResult.CurrentDigest,
		"latestDigest":   digestResult.LatestDigest,
		"responseTimeMs": digestResult.ResponseTimeMs,
	}
	if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageScan, "", imageRef, systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "Failed to log image update check event", "imageRef", imageRef, "error", logErr.Error())
	}
	if saveErr := s.saveUpdateResultWithSnapshotInternal(ctx, imageRef, digestResult, snapshot); saveErr != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
	}

	// Send notification if update is available
	if digestResult.HasUpdate && s.notificationService != nil {
		if notifErr := s.notificationService.SendImageUpdateNotification(ctx, imageRef, digestResult, models.NotificationEventImageUpdate); notifErr != nil {
			slog.WarnContext(ctx, "Failed to send update notification", "imageRef", imageRef, "error", notifErr.Error())
		}
	}

	return digestResult, nil
}

func (s *ImageUpdateService) checkDigestUpdateWithSnapshotInternal(ctx context.Context, parts *ImageParts) (*imageupdate.Response, *localImageSnapshot, error) {
	if s.registryService == nil {
		return nil, nil, fmt.Errorf("registry service unavailable")
	}

	imageRef := fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag)
	start := time.Now()
	digestResult, err := s.registryService.inspectImageDigestInternal(ctx, imageRef, nil)
	elapsed := time.Since(start)
	if err != nil {
		partial := digestResult // may contain auth metadata even on error
		if partial == nil {
			return nil, nil, fmt.Errorf("failed to get remote digest: %w", err)
		}
		return &imageupdate.Response{
			Error:          err.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(elapsed.Milliseconds()),
			AuthMethod:     partial.AuthMethod,
			AuthUsername:   partial.AuthUsername,
			AuthRegistry:   partial.AuthRegistry,
			UsedCredential: partial.UsedCredential,
		}, nil, fmt.Errorf("failed to get remote digest: %w", err)
	}

	snapshot, err := s.inspectLocalImageSnapshotInternal(ctx, imageRef)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get local digest: %w", err)
	}

	localDigest := snapshot.PrimaryDigest
	hasUpdate := true
	for _, localDig := range snapshot.AllDigests {
		if localDig == digestResult.Digest {
			localDigest = localDig
			hasUpdate = false
			break
		}
	}

	slog.DebugContext(ctx, "digest comparison",
		"imageRef", imageRef,
		"primaryLocalDigest", localDigest,
		"allLocalDigests", snapshot.AllDigests,
		"remoteDigest", digestResult.Digest,
		"hasUpdate", hasUpdate)

	return &imageupdate.Response{
		HasUpdate:      hasUpdate,
		UpdateType:     "digest",
		CurrentDigest:  localDigest,
		LatestDigest:   digestResult.Digest,
		CheckTime:      time.Now(),
		ResponseTimeMs: int(elapsed.Milliseconds()),
		AuthMethod:     digestResult.AuthMethod,
		AuthUsername:   digestResult.AuthUsername,
		AuthRegistry:   digestResult.AuthRegistry,
		UsedCredential: digestResult.UsedCredential,
	}, snapshot, nil
}

func (s *ImageUpdateService) parseImageReference(imageRef string) *ImageParts {
	// Use the official Docker reference parser to handle all edge cases
	named, err := ref.ParseNormalizedNamed(imageRef)
	if err != nil {
		// Fallback to manual parsing if the official parser fails
		return s.parseImageReferenceFallback(imageRef)
	}

	// Extract registry
	registryHost := ref.Domain(named)

	// Extract repository (path without registry)
	repository := ref.Path(named)

	// Extract tag or default to latest
	tag := "latest"
	if tagged, ok := named.(ref.NamedTagged); ok {
		tag = tagged.Tag()
	} else if _, ok := named.(ref.Digested); ok {
		// If it's a digest reference, still use "latest" as the tag for registry queries
		tag = "latest"
	}

	return &ImageParts{
		Registry:   registry.NormalizeRegistryForComparison(registryHost),
		Repository: repository,
		Tag:        tag,
	}
}

// Fallback parser for cases where the official parser fails
func (s *ImageUpdateService) parseImageReferenceFallback(imageRef string) *ImageParts {
	var registryHost, repository, tag string
	if strings.Contains(imageRef, "@sha256:") {
		digestParts := strings.Split(imageRef, "@")
		if len(digestParts) != 2 {
			return nil
		}
		repoWithRegistry := digestParts[0]
		parts := strings.Split(repoWithRegistry, "/")
		if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
			registryHost = parts[0]
			repository = strings.Join(parts[1:], "/")
		} else {
			registryHost = "docker.io"
			if len(parts) == 1 {
				repository = "library/" + parts[0]
			} else {
				repository = repoWithRegistry
			}
		}
		tag = "latest"
	} else {
		parts := strings.Split(imageRef, "/")
		switch {
		case len(parts) == 1:
			registryHost = "docker.io"
			if strings.Contains(parts[0], ":") {
				repoParts := strings.Split(parts[0], ":")
				repository = "library/" + repoParts[0]
				tag = repoParts[1]
			} else {
				repository = "library/" + parts[0]
				tag = "latest"
			}
		case strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":"):
			registryHost = parts[0]
			repository = strings.Join(parts[1:], "/")
			if strings.Contains(repository, ":") {
				repoParts := strings.Split(repository, ":")
				repository = repoParts[0]
				tag = repoParts[1]
			} else {
				tag = "latest"
			}
		default:
			registryHost = "docker.io"
			repository = imageRef
			if strings.Contains(repository, ":") {
				repoParts := strings.Split(repository, ":")
				repository = repoParts[0]
				tag = repoParts[1]
			} else {
				tag = "latest"
			}
		}
	}
	return &ImageParts{Registry: registry.NormalizeRegistryForComparison(registryHost), Repository: repository, Tag: tag}
}

func (s *ImageUpdateService) getImageRefByID(ctx context.Context, imageID string) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	imageID = strings.TrimPrefix(imageID, "sha256:")
	inspectResponse, err := dockerClient.ImageInspect(ctx, imageID)
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	if len(inspectResponse.RepoTags) > 0 {
		for _, tag := range inspectResponse.RepoTags {
			if tag != "<none>:<none>" {
				return tag, nil
			}
		}
	}
	if len(inspectResponse.RepoDigests) > 0 {
		for _, digest := range inspectResponse.RepoDigests {
			if digest != "<none>@<none>" {
				digestParts := strings.Split(digest, "@")
				if len(digestParts) == 2 {
					return digestParts[0] + ":latest", nil
				}
			}
		}
	}
	return "", fmt.Errorf("no valid repository tags or digests found for image")
}

func (s *ImageUpdateService) getAllImageRefsInternal(ctx context.Context, limit int) ([]string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	imageList, err := dockerClient.ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker images: %w", err)
	}

	return dedupeImageRefsFromSummariesInternal(imageList.Items, limit), nil
}

func dedupeImageRefsFromSummariesInternal(images []image.Summary, limit int) []string {
	seen := make(map[string]struct{})
	var imageRefs []string
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag != "<none>:<none>" {
				if _, exists := seen[tag]; exists {
					continue
				}
				seen[tag] = struct{}{}
				imageRefs = append(imageRefs, tag)
			}
			if limit > 0 && len(imageRefs) >= limit {
				return imageRefs[:limit]
			}
		}
	}
	return imageRefs
}

func (s *ImageUpdateService) inspectLocalImageSnapshotInternal(ctx context.Context, imageRef string) (*localImageSnapshot, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	inspectResponse, err := dockerClient.ImageInspect(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}

	var allDigests []string
	var primaryDigest string

	// Extract all digests from RepoDigests
	if len(inspectResponse.RepoDigests) > 0 {
		for _, repoDigest := range inspectResponse.RepoDigests {
			// Format: repository@sha256:...
			digestParts := strings.Split(repoDigest, "@")
			if len(digestParts) == 2 {
				digest := digestParts[1]
				allDigests = append(allDigests, digest)

				// Use first digest as primary if not yet set
				if primaryDigest == "" {
					primaryDigest = digest
				}
			}
		}
	}

	// Fallback to image ID if no repo digests available
	if primaryDigest == "" {
		primaryDigest = inspectResponse.ID
		allDigests = []string{primaryDigest}
	}

	repo, tag := extractRepoAndTagFromImage(inspectResponse.InspectResponse)

	return &localImageSnapshot{
		ImageID:       inspectResponse.ID,
		Repository:    repo,
		Tag:           tag,
		PrimaryDigest: primaryDigest,
		AllDigests:    allDigests,
	}, nil
}

func (s *ImageUpdateService) normalizeRepository(regHost, repo string) string {
	if regHost == "docker.io" && !strings.Contains(repo, "/") {
		return "library/" + repo
	}
	return repo
}

func (s *ImageUpdateService) CheckImageUpdateByID(ctx context.Context, imageID string) (*imageupdate.Response, error) {
	imageRef, err := s.getImageRefByID(ctx, imageID)
	if err != nil {
		metadata := models.JSON{
			"action":  "check_update_by_id",
			"imageID": imageID,
			"error":   err.Error(),
		}
		if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageScan, imageID, "", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check by ID error event", "imageID", imageID, "error", logErr.Error())
		}
		return nil, fmt.Errorf("failed to get image reference: %w", err)
	}
	result, err := s.CheckImageUpdate(ctx, imageRef)
	if err != nil {
		return nil, err
	}
	if saveErr := s.saveUpdateResultByIDInternal(ctx, imageID, result); saveErr != nil {
		slog.WarnContext(ctx, "Failed to save update result by ID", "imageID", imageID, "error", saveErr.Error())
	}
	return result, nil
}

func (s *ImageUpdateService) saveUpdateResultWithSnapshotInternal(ctx context.Context, imageRef string, result *imageupdate.Response, snapshot *localImageSnapshot) error {
	if snapshot != nil && snapshot.ImageID != "" {
		return s.savePreparedUpdateResultInternal(ctx, snapshot.ImageID, snapshot.Repository, snapshot.Tag, result)
	}

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		return fmt.Errorf("invalid image reference")
	}
	imageID, err := s.getImageIDByRef(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("failed to get image ID: %w", err)
	}
	return s.saveUpdateResultByIDInternal(ctx, imageID, result)
}

func extractRepoAndTagFromImage(dockerImage image.InspectResponse) (repo, tag string) {
	if len(dockerImage.RepoTags) > 0 && dockerImage.RepoTags[0] != "<none>:<none>" {
		if named, err := ref.ParseNormalizedNamed(dockerImage.RepoTags[0]); err == nil {
			repo = ref.FamiliarName(named)
			if tagged, ok := named.(ref.NamedTagged); ok {
				tag = tagged.Tag()
			} else {
				tag = "latest"
			}
			return repo, tag
		}

		parts := strings.SplitN(dockerImage.RepoTags[0], ":", 2)
		repo = parts[0]
		if len(parts) > 1 {
			tag = parts[1]
		} else {
			tag = "latest"
		}
		return repo, tag
	}

	if len(dockerImage.RepoDigests) > 0 {
		for _, rd := range dockerImage.RepoDigests {
			if rd == "<none>@<none>" {
				continue
			}
			if at := strings.LastIndex(rd, "@"); at != -1 {
				repoCandidate := rd[:at]
				if repoCandidate != "" {
					return repoCandidate, "<none>"
				}
			}
		}
	}

	return "<none>", "<none>"
}

func stringToPtr(s string) *string {
	if s == "" {
		return nil
	}
	return new(s)
}

func stringPtrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func buildImageUpdateRecord(imageID, repo, tag string, result *imageupdate.Response) *models.ImageUpdateRecord {
	currentVersion := result.CurrentVersion
	if currentVersion == "" {
		currentVersion = tag
	}

	return &models.ImageUpdateRecord{
		ID:             imageID,
		Repository:     repo,
		Tag:            tag,
		HasUpdate:      result.HasUpdate,
		UpdateType:     result.UpdateType,
		CurrentVersion: currentVersion,
		LatestVersion:  stringToPtr(result.LatestVersion),
		CurrentDigest:  stringToPtr(result.CurrentDigest),
		LatestDigest:   stringToPtr(result.LatestDigest),
		CheckTime:      result.CheckTime,
		ResponseTimeMs: result.ResponseTimeMs,
		LastError:      stringToPtr(result.Error),
		AuthMethod:     stringToPtr(result.AuthMethod),
		AuthUsername:   stringToPtr(result.AuthUsername),
		AuthRegistry:   stringToPtr(result.AuthRegistry),
		UsedCredential: result.UsedCredential,
	}
}

func (s *ImageUpdateService) saveUpdateResultByIDInternal(ctx context.Context, imageID string, result *imageupdate.Response) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	dockerImage, err := dockerClient.ImageInspect(ctx, imageID)
	if err != nil {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	repo, tag := extractRepoAndTagFromImage(dockerImage.InspectResponse)
	return s.savePreparedUpdateResultInternal(ctx, imageID, repo, tag, result)
}

func (s *ImageUpdateService) savePreparedUpdateResultInternal(ctx context.Context, imageID, repo, tag string, result *imageupdate.Response) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updateRecord := buildImageUpdateRecord(imageID, repo, tag, result)

		// Check if there's an existing record to compare state changes
		var existingRecord models.ImageUpdateRecord
		if err := tx.Where("id = ?", imageID).First(&existingRecord).Error; err == nil {
			// Existing record found - check if we need to reset notification_sent
			stateChanged := existingRecord.HasUpdate != updateRecord.HasUpdate
			digestChanged := stringPtrToString(existingRecord.LatestDigest) != stringPtrToString(updateRecord.LatestDigest)
			versionChanged := stringPtrToString(existingRecord.LatestVersion) != stringPtrToString(updateRecord.LatestVersion)

			// Reset notification_sent if the update state changed in any way
			if stateChanged || (updateRecord.HasUpdate && (digestChanged || versionChanged)) {
				updateRecord.NotificationSent = false
			} else {
				// Keep the existing notification_sent value if nothing changed
				updateRecord.NotificationSent = existingRecord.NotificationSent
			}
		} else {
			// New record - start with notification_sent = false
			updateRecord.NotificationSent = false
		}

		return tx.Save(updateRecord).Error
	})
}

func (s *ImageUpdateService) getImageIDByRef(ctx context.Context, imageRef string) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	inspectResponse, err := dockerClient.ImageInspect(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	return inspectResponse.ID, nil
}

// GetUnnotifiedUpdates returns a map of image IDs that have updates but haven't been notified yet
func (s *ImageUpdateService) GetUnnotifiedUpdates(ctx context.Context) (map[string]*models.ImageUpdateRecord, error) {
	var records []models.ImageUpdateRecord
	if err := s.db.WithContext(ctx).
		Where("has_update = ? AND notification_sent = ?", true, false).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to get unnotified updates: %w", err)
	}

	result := make(map[string]*models.ImageUpdateRecord)
	for i := range records {
		result[records[i].ID] = &records[i]
	}
	return result, nil
}

// MarkUpdatesAsNotified marks the given image IDs as having been notified
func (s *ImageUpdateService) MarkUpdatesAsNotified(ctx context.Context, imageIDs []string) error {
	if len(imageIDs) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).
		Model(&models.ImageUpdateRecord{}).
		Where("id IN ?", imageIDs).
		Update("notification_sent", true).Error
}

type batchImage struct {
	refs         []string
	canonicalRef string
	parts        *ImageParts
}

func (s *ImageUpdateService) parseAndGroupImagesInternal(imageRefs []string) (map[string]map[string]struct{}, map[string]*imageupdate.Response, []batchImage) {
	regRepos := make(map[string]map[string]struct{})
	results := make(map[string]*imageupdate.Response)
	var images []batchImage
	indexByNormalizedRef := make(map[string]int)

	for _, ref := range imageRefs {
		parts := s.parseImageReference(ref)
		if parts == nil {
			results[ref] = &imageupdate.Response{
				Error:          "Invalid image reference format",
				CheckTime:      time.Now(),
				ResponseTimeMs: 0,
			}
			continue
		}
		if _, ok := regRepos[parts.Registry]; !ok {
			regRepos[parts.Registry] = make(map[string]struct{})
		}
		regRepos[parts.Registry][s.normalizeRepository(parts.Registry, parts.Repository)] = struct{}{}
		normalizedRef := strings.ToLower(fmt.Sprintf("%s/%s:%s", parts.Registry, s.normalizeRepository(parts.Registry, parts.Repository), parts.Tag))
		if idx, exists := indexByNormalizedRef[normalizedRef]; exists {
			images[idx].refs = append(images[idx].refs, ref)
			continue
		}

		indexByNormalizedRef[normalizedRef] = len(images)
		images = append(images, batchImage{
			refs:         []string{ref},
			canonicalRef: ref,
			parts:        parts,
		})
	}
	return regRepos, results, images
}

func (s *ImageUpdateService) checkSingleImageInBatchInternal(ctx context.Context, externalCreds []containerregistry.Credential, parts *ImageParts) (*imageupdate.Response, *localImageSnapshot) {
	if s.registryService == nil {
		return &imageupdate.Response{
			Error:          "registry service unavailable",
			CheckTime:      time.Now(),
			ResponseTimeMs: 0,
		}, nil
	}

	start := time.Now()
	imageRef := fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag)
	digestResult, digestErr := s.registryService.inspectImageDigestInternal(ctx, imageRef, externalCreds)
	if digestErr != nil {
		resp := &imageupdate.Response{
			Error:          digestErr.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
		}
		if digestResult != nil {
			resp.AuthMethod = digestResult.AuthMethod
			resp.AuthUsername = digestResult.AuthUsername
			resp.AuthRegistry = digestResult.AuthRegistry
			resp.UsedCredential = digestResult.UsedCredential
		}
		return resp, nil
	}

	snapshot, ldErr := s.inspectLocalImageSnapshotInternal(ctx, imageRef)
	if ldErr != nil {
		return &imageupdate.Response{
			Error:          ldErr.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			AuthMethod:     digestResult.AuthMethod,
			AuthUsername:   digestResult.AuthUsername,
			AuthRegistry:   digestResult.AuthRegistry,
			UsedCredential: digestResult.UsedCredential,
		}, nil
	}

	localDigest := snapshot.PrimaryDigest
	hasDigestUpdate := true
	for _, localDig := range snapshot.AllDigests {
		if localDig == digestResult.Digest {
			localDigest = localDig
			hasDigestUpdate = false
			break
		}
	}

	return &imageupdate.Response{
		HasUpdate:      hasDigestUpdate,
		UpdateType:     "digest",
		CurrentDigest:  localDigest,
		LatestDigest:   digestResult.Digest,
		CheckTime:      time.Now(),
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
		AuthMethod:     digestResult.AuthMethod,
		AuthUsername:   digestResult.AuthUsername,
		AuthRegistry:   digestResult.AuthRegistry,
		UsedCredential: digestResult.UsedCredential,
	}, snapshot
}

func (s *ImageUpdateService) resolveBatchCredentialsInternal(ctx context.Context, externalCreds []containerregistry.Credential) []containerregistry.Credential {
	if len(externalCreds) > 0 {
		filtered := make([]containerregistry.Credential, 0, len(externalCreds))
		for _, cred := range externalCreds {
			if !cred.Enabled || strings.TrimSpace(cred.URL) == "" || strings.TrimSpace(cred.Username) == "" || strings.TrimSpace(cred.Token) == "" {
				continue
			}
			filtered = append(filtered, cred)
		}
		return filtered
	}

	if s.registryService == nil {
		return nil
	}

	registries, err := s.registryService.GetEnabledRegistries(ctx)
	if err != nil {
		slog.DebugContext(ctx, "failed to load enabled registries for batch check", "error", err.Error())
		return nil
	}

	credentials := make([]containerregistry.Credential, 0, len(registries))
	for _, reg := range registries {
		if strings.TrimSpace(reg.URL) == "" || strings.TrimSpace(reg.Username) == "" || reg.Token == "" {
			continue
		}

		token, decryptErr := crypto.Decrypt(reg.Token)
		if decryptErr != nil {
			slog.DebugContext(ctx, "failed to decrypt registry token for batch check", "registryURL", reg.URL, "error", decryptErr.Error())
			continue
		}
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		credentials = append(credentials, containerregistry.Credential{
			URL:      reg.URL,
			Username: reg.Username,
			Token:    token,
			Enabled:  reg.Enabled,
		})
	}

	return credentials
}

func (s *ImageUpdateService) CheckMultipleImages(ctx context.Context, imageRefs []string, externalCreds []containerregistry.Credential) (map[string]*imageupdate.Response, error) {
	startBatch := time.Now()
	results := make(map[string]*imageupdate.Response, len(imageRefs))
	if len(imageRefs) == 0 {
		return results, nil
	}

	slog.DebugContext(ctx, "Starting batch image update check", "imageCount", len(imageRefs), "externalCredCount", len(externalCreds))

	regRepos, initialResults, images := s.parseAndGroupImagesInternal(imageRefs)
	maps.Copy(results, initialResults)

	resolvedCreds := s.resolveBatchCredentialsInternal(ctx, externalCreds)

	slog.DebugContext(ctx, "Resolved batch registry credentials", "credentialCount", len(resolvedCreds), "registryCount", len(regRepos))

	var mu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(10) // Limit concurrency

	for _, img := range images {
		g.Go(func() error {
			res, snapshot := s.checkSingleImageInBatchInternal(groupCtx, resolvedCreds, img.parts)

			mu.Lock()
			for _, ref := range img.refs {
				results[ref] = res
			}
			mu.Unlock()

			if err := s.saveUpdateResultWithSnapshotInternal(groupCtx, img.canonicalRef, res, snapshot); err != nil {
				slog.WarnContext(groupCtx, "Failed to save update result", "imageRef", img.canonicalRef, "error", err.Error())
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "Batch check error", "error", err)
	}

	slog.InfoContext(ctx, "Batch image update check completed",
		"totalImages", len(imageRefs),
		"successCount", len(results),
		"duration", time.Since(startBatch))

	if s.notificationService != nil {
		// Use a context with timeout for notifications
		notifCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Get only the updates that haven't been notified yet
		unnotifiedUpdates, err := s.GetUnnotifiedUpdates(notifCtx)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "Failed to get unnotified updates", "error", err.Error())
		case len(unnotifiedUpdates) > 0:
			// Convert unnotified records to the format expected by notification service
			updatesToNotify := make(map[string]*imageupdate.Response)
			imageIDsToMark := make([]string, 0, len(unnotifiedUpdates))

			for imageID, record := range unnotifiedUpdates {
				// Construct image ref from repository and tag
				imageRef := fmt.Sprintf("%s:%s", record.Repository, record.Tag)
				updatesToNotify[imageRef] = &imageupdate.Response{
					HasUpdate:      record.HasUpdate,
					UpdateType:     record.UpdateType,
					CurrentVersion: record.CurrentVersion,
					LatestVersion:  stringPtrToString(record.LatestVersion),
					CurrentDigest:  stringPtrToString(record.CurrentDigest),
					LatestDigest:   stringPtrToString(record.LatestDigest),
					CheckTime:      record.CheckTime,
					ResponseTimeMs: record.ResponseTimeMs,
					Error:          stringPtrToString(record.LastError),
					AuthMethod:     stringPtrToString(record.AuthMethod),
					AuthUsername:   stringPtrToString(record.AuthUsername),
					AuthRegistry:   stringPtrToString(record.AuthRegistry),
					UsedCredential: record.UsedCredential,
				}
				imageIDsToMark = append(imageIDsToMark, imageID)
			}

			slog.InfoContext(ctx, "Sending notifications for unnotified updates", "count", len(updatesToNotify))

			if notifErr := s.notificationService.SendBatchImageUpdateNotification(notifCtx, updatesToNotify); notifErr != nil {
				slog.WarnContext(ctx, "Failed to send batch update notification", "error", notifErr.Error())
			} else {
				// Mark the images as notified only if notification was successful
				if markErr := s.MarkUpdatesAsNotified(notifCtx, imageIDsToMark); markErr != nil {
					slog.WarnContext(ctx, "Failed to mark updates as notified", "error", markErr.Error())
				}
			}
		default:
			slog.DebugContext(ctx, "No new updates to notify")
		}
	}

	return results, nil
}

func (s *ImageUpdateService) CheckAllImages(ctx context.Context, limit int, externalCreds []containerregistry.Credential) (map[string]*imageupdate.Response, error) {
	imageRefs, err := s.getAllImageRefsInternal(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get image references: %w", err)
	}

	if len(imageRefs) == 0 {
		return make(map[string]*imageupdate.Response), nil
	}

	results, err := s.CheckMultipleImages(ctx, imageRefs, externalCreds)
	if err != nil {
		return nil, err
	}

	if err := s.CleanupOrphanedRecords(ctx); err != nil {
		slog.WarnContext(ctx, "failed to cleanup orphaned image update records after check-all", "error", err.Error())
	}

	return results, nil
}

func (s *ImageUpdateService) CleanupOrphanedRecords(ctx context.Context) error {
	if s.db == nil {
		return nil
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Get all image IDs from Docker
	dockerImagesResult, err := dockerClient.ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Docker images: %w", err)
	}
	dockerImages := dockerImagesResult.Items

	dockerImageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		dockerImageIDs = append(dockerImageIDs, img.ID)
	}

	var result *gorm.DB
	if len(dockerImageIDs) == 0 {
		result = s.db.WithContext(ctx).Where("1 = 1").Delete(&models.ImageUpdateRecord{})
	} else {
		result = s.db.WithContext(ctx).Where("id NOT IN ?", dockerImageIDs).Delete(&models.ImageUpdateRecord{})
	}
	if result.Error != nil {
		return fmt.Errorf("failed to delete orphaned records: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		slog.InfoContext(ctx, "Cleaned up orphaned image update records", "deletedCount", result.RowsAffected)
	} else {
		slog.InfoContext(ctx, "No orphaned image update records found")
	}
	return nil
}

func (s *ImageUpdateService) GetUpdateSummary(ctx context.Context) (*imageupdate.Summary, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	dockerImagesResult, err := dockerClient.ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker images: %w", err)
	}
	dockerImages := dockerImagesResult.Items

	liveImageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		liveImageIDs = append(liveImageIDs, img.ID)
	}

	return s.getUpdateSummaryForImageIDsInternal(ctx, liveImageIDs)
}

func (s *ImageUpdateService) getUpdateSummaryForImageIDsInternal(ctx context.Context, imageIDs []string) (*imageupdate.Summary, error) {
	summary := &imageupdate.Summary{
		TotalImages: len(imageIDs),
	}

	if s.db == nil || len(imageIDs) == 0 {
		return summary, nil
	}

	var aggregate struct {
		ImagesWithUpdates int64 `gorm:"column:images_with_updates"`
		DigestUpdates     int64 `gorm:"column:digest_updates"`
		ErrorsCount       int64 `gorm:"column:errors_count"`
	}
	if err := s.db.WithContext(ctx).
		Model(&models.ImageUpdateRecord{}).
		Select(`
			COALESCE(SUM(CASE WHEN has_update THEN 1 ELSE 0 END), 0) AS images_with_updates,
			COALESCE(SUM(CASE WHEN has_update AND update_type = ? THEN 1 ELSE 0 END), 0) AS digest_updates,
			COALESCE(SUM(CASE WHEN last_error IS NOT NULL AND last_error != '' THEN 1 ELSE 0 END), 0) AS errors_count
		`, "digest").
		Where("id IN ?", imageIDs).
		Scan(&aggregate).Error; err != nil {
		return nil, err
	}

	summary.ImagesWithUpdates = int(aggregate.ImagesWithUpdates)
	summary.DigestUpdates = int(aggregate.DigestUpdates)
	summary.ErrorsCount = int(aggregate.ErrorsCount)

	return summary, nil
}
