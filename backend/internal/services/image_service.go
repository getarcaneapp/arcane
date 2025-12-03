package services

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"log/slog"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"go.getarcane.app/types/containerregistry"
	imagetypes "go.getarcane.app/types/image"
	ref "go.podman.io/image/v5/docker/reference"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type ImageService struct {
	db                 *database.DB
	dockerService      *DockerClientService
	imageUpdateService *ImageUpdateService
	registryService    *ContainerRegistryService
	eventService       *EventService
}

func NewImageService(db *database.DB, dockerService *DockerClientService, registryService *ContainerRegistryService, imageUpdateService *ImageUpdateService, eventService *EventService) *ImageService {
	return &ImageService{
		db:                 db,
		dockerService:      dockerService,
		registryService:    registryService,
		imageUpdateService: imageUpdateService,
		eventService:       eventService,
	}
}

func (s *ImageService) GetImageByID(ctx context.Context, id string) (*image.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	inspect, err := dockerClient.ImageInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspect not found: %w", err)
	}

	return &inspect, nil
}

func (s *ImageService) RemoveImage(ctx context.Context, id string, force bool, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", id, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	imageDetails, inspectErr := dockerClient.ImageInspect(ctx, id)
	var imageName string
	if inspectErr == nil && len(imageDetails.RepoTags) > 0 {
		imageName = imageDetails.RepoTags[0]
	} else {
		imageName = id
	}

	options := image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	}

	_, err = dockerClient.ImageRemove(ctx, id, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", id, imageName, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to remove image: %w", err)
	}

	if s.db != nil {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return tx.Delete(&models.ImageUpdateRecord{}, "id = ?", id).Error
		}); err != nil {
			slog.WarnContext(ctx, "failed to delete image update record", "id", id, "error", err)
		}
	}

	metadata := models.JSON{
		"action":  "delete",
		"imageId": id,
		"force":   force,
	}
	if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageDelete, id, imageName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.Warn("could not log image deletion action", "err", logErr, "image", imageName, "image_id", id)
	}

	return nil
}

func (s *ImageService) PullImage(ctx context.Context, imageName string, progressWriter io.Writer, user models.User, externalCreds []containerregistry.Credential) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", err, models.JSON{"action": "pull"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	slog.DebugContext(ctx, "Attempting to pull image", "image", imageName, "externalCredCount", len(externalCreds))

	pullOptions, err := s.getPullOptionsWithAuth(ctx, imageName, externalCreds)
	if err != nil {
		slog.WarnContext(ctx, "Failed to get registry authentication for image; proceeding without auth", "image", imageName, "error", err.Error())
		pullOptions = image.PullOptions{}
	}

	reader, err := dockerClient.ImagePull(ctx, imageName, pullOptions)
	if err != nil {
		slog.ErrorContext(ctx, "Docker ImagePull failed", "image", imageName, "hasAuth", pullOptions.RegistryAuth != "", "error", err.Error())
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", err, models.JSON{"action": "pull"})
		return fmt.Errorf("failed to initiate image pull for %s: %w", imageName, err)
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	flusher, implementsFlusher := progressWriter.(http.Flusher)

	for scanner.Scan() {
		line := scanner.Bytes()
		if _, writeErr := progressWriter.Write(line); writeErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", writeErr, models.JSON{"action": "pull", "step": "write_progress"})
			return fmt.Errorf("error writing pull progress for %s: %w", imageName, writeErr)
		}
		if _, writeErr := progressWriter.Write([]byte("\n")); writeErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", writeErr, models.JSON{"action": "pull", "step": "write_newline"})
			return fmt.Errorf("error writing newline for %s: %w", imageName, writeErr)
		}

		if implementsFlusher {
			flusher.Flush()
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		if errors.Is(scanErr, context.Canceled) || strings.Contains(scanErr.Error(), "context canceled") {
			slog.Debug("image pull stream canceled", "image", imageName, "err", scanErr)
			s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", scanErr, models.JSON{"action": "pull", "step": "canceled"})
			return fmt.Errorf("image pull stream canceled for %s: %w", imageName, scanErr)
		}
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", scanErr, models.JSON{"action": "pull", "step": "read_stream"})
		return fmt.Errorf("error reading image pull stream for %s: %w", imageName, scanErr)
	}

	slog.Debug("image pull stream completed", "image", imageName)

	metadata := models.JSON{
		"action":    "pull",
		"imageName": imageName,
	}
	if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImagePull, "", imageName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.Warn("could not log image pull action", "err", logErr, "image", imageName)
	}

	return nil
}

func (s *ImageService) LoadImageFromReader(ctx context.Context, reader io.Reader, fileName string, user models.User, maxSizeBytes int64) (*imagetypes.LoadResult, error) {
	// Wrap reader with size limit enforcement
	limitedReader := io.LimitReader(reader, maxSizeBytes+1)

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", fileName, user.ID, user.Username, "0", err, models.JSON{"action": "load"})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// ImageLoad accepts a tar archive reader and optional load options
	loadResp, err := dockerClient.ImageLoad(ctx, limitedReader)
	if err != nil {
		// Check if error is due to size limit being exceeded
		if err.Error() == "unexpected EOF" || strings.Contains(err.Error(), "unexpected EOF") {
			return nil, fmt.Errorf("file size exceeds maximum allowed size of %d MB", maxSizeBytes/(1024*1024))
		}
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", fileName, user.ID, user.Username, "0", err, models.JSON{"action": "load", "file": fileName})
		return nil, fmt.Errorf("failed to load image from tar: %w", err)
	}
	defer loadResp.Body.Close()

	var result imagetypes.LoadResult
	responseBytes, err := io.ReadAll(loadResp.Body)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", fileName, user.ID, user.Username, "0", err, models.JSON{"action": "load", "file": fileName, "step": "read_response"})
		return nil, fmt.Errorf("failed to read load response: %w", err)
	}

	responseStr := string(responseBytes)
	result.Stream = responseStr

	metadata := models.JSON{
		"action":   "load",
		"fileName": fileName,
	}
	if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageLoad, "", fileName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.Warn("could not log image load action", "err", logErr, "file", fileName)
	}

	return &result, nil
}

func (s *ImageService) ImageExistsLocally(ctx context.Context, imageName string) (bool, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return false, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.ImageInspect(ctx, imageName)
	if err == nil {
		return true, nil
	}

	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "no such image") || strings.Contains(errLower, "not found") {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}

func (s *ImageService) getPullOptionsWithAuth(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) (image.PullOptions, error) {
	pullOptions := image.PullOptions{}

	registryHost := s.extractRegistryHost(imageRef)

	// Check external credentials first
	for _, cred := range externalCreds {
		if !cred.Enabled || cred.Username == "" || cred.Token == "" {
			continue
		}

		if s.isRegistryMatch(cred.URL, registryHost) {
			authStr, err := s.createAuthHeader(cred.Username, cred.Token, s.normalizeRegistryURL(cred.URL))
			if err != nil {
				return pullOptions, fmt.Errorf("failed to create auth header: %w", err)
			}
			pullOptions.RegistryAuth = authStr

			slog.DebugContext(ctx, "Using external credentials for image pull", "registry", registryHost, "username", cred.Username)
			return pullOptions, nil
		}
	}

	if s.registryService == nil {
		return pullOptions, nil
	}

	// Check database registries
	registries, err := s.registryService.GetEnabledRegistries(ctx)
	if err != nil {
		return pullOptions, fmt.Errorf("failed to get registry credentials: %w", err)
	}

	for _, reg := range registries {
		if s.isRegistryMatch(reg.URL, registryHost) {
			decryptedToken, err := s.registryService.GetDecryptedToken(ctx, reg.ID)
			if err != nil {
				return pullOptions, fmt.Errorf("failed to decrypt token for registry %s: %w", reg.URL, err)
			}

			authStr, err := s.createAuthHeader(reg.Username, decryptedToken, s.normalizeRegistryURL(reg.URL))
			if err != nil {
				return pullOptions, fmt.Errorf("failed to create auth header: %w", err)
			}
			pullOptions.RegistryAuth = authStr

			slog.DebugContext(ctx, "Using database credentials for image pull", "registry", registryHost, "username", reg.Username)
			break
		}
	}

	return pullOptions, nil
}

func (s *ImageService) createAuthHeader(username, password, serverAddress string) (string, error) {
	authConfig := &registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: serverAddress,
	}

	authBytes, err := json.Marshal(authConfig)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(authBytes), nil
}

func (s *ImageService) extractRegistryHost(imageRef string) string {
	if i := strings.IndexByte(imageRef, '@'); i != -1 {
		imageRef = imageRef[:i]
	}

	hostCandidate, _, found := strings.Cut(imageRef, "/")
	if !found {
		return "docker.io"
	}

	if !strings.Contains(hostCandidate, ".") && !strings.Contains(hostCandidate, ":") {
		return "docker.io"
	}
	return hostCandidate
}

func (s *ImageService) isRegistryMatch(credURL, registryHost string) bool {
	normalizedCred := s.normalizeRegistryForComparison(credURL)
	normalizedHost := s.normalizeRegistryForComparison(registryHost)

	return normalizedCred == normalizedHost
}

func (s *ImageService) normalizeRegistryForComparison(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, "/")

	if slash := strings.Index(url, "/"); slash != -1 {
		url = url[:slash]
	}

	if url == "docker.io" || url == "registry-1.docker.io" || url == "index.docker.io" {
		return "docker.io"
	}
	return url
}

func (s *ImageService) normalizeRegistryURL(url string) string {
	normalized := s.normalizeRegistryForComparison(url)
	if normalized == "docker.io" {
		return "https://index.docker.io/v1/"
	}

	result := strings.TrimPrefix(url, "https://")
	result = strings.TrimPrefix(result, "http://")
	result = strings.TrimSuffix(result, "/")

	return result
}

func (s *ImageService) PruneImages(ctx context.Context, dangling bool) (*image.PruneReport, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := filters.NewArgs()
	if dangling {
		filterArgs.Add("dangling", "true")
	} else {
		filterArgs.Add("dangling", "false")
	}

	report, err := dockerClient.ImagesPrune(ctx, filterArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to prune images: %w", err)
	}

	// Clean up database records for deleted images
	if s.db != nil && len(report.ImagesDeleted) > 0 {
		var idsToDelete []string
		for _, img := range report.ImagesDeleted {
			if img.Deleted != "" {
				idsToDelete = append(idsToDelete, img.Deleted)
			} else if img.Untagged != "" {
				idsToDelete = append(idsToDelete, img.Untagged)
			}
		}

		if len(idsToDelete) > 0 {
			if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return tx.Where("id IN ?", idsToDelete).Delete(&models.ImageUpdateRecord{}).Error
			}); err != nil {
				slog.WarnContext(ctx, "failed to clean up image update records after prune", "error", err)
			}
		}
	}

	metadata := models.JSON{
		"action":         "prune",
		"dangling":       dangling,
		"imagesDeleted":  len(report.ImagesDeleted),
		"spaceReclaimed": report.SpaceReclaimed,
	}
	if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageDelete, "", "bulk_prune", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		slog.Warn("could not log image prune action", "err", logErr)
	}

	return &report, nil
}

func (s *ImageService) ListImagesPaginated(ctx context.Context, params pagination.QueryParams) ([]imagetypes.Summary, pagination.Response, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	var (
		dockerImages  []image.Summary
		containers    []container.Summary
		updateRecords []models.ImageUpdateRecord
	)

	g, ctx := errgroup.WithContext(ctx)

	// Fetch Docker images
	g.Go(func() error {
		var err error
		dockerImages, err = dockerClient.ImageList(ctx, image.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list Docker images: %w", err)
		}
		return nil
	})

	// Fetch containers to determine usage
	g.Go(func() error {
		var err error
		containers, err = dockerClient.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}
		return nil
	})

	// Fetch update records from DB
	g.Go(func() error {
		if s.db != nil {
			return s.db.WithContext(ctx).Find(&updateRecords).Error
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, pagination.Response{}, err
	}

	inUseMap := buildInUseMap(containers)
	updateMap := buildUpdateMap(updateRecords)

	items := mapDockerImagesToDTOs(dockerImages, inUseMap, updateMap)

	config := s.getImagePaginationConfig()

	result := pagination.SearchOrderAndPaginate(items, params, config)

	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (int64(result.TotalCount) + int64(params.Limit) - 1) / int64(params.Limit)
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Start / params.Limit) + 1
	}

	paginationResp := pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      int64(result.TotalCount),
		CurrentPage:     page,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: int64(result.TotalAvailable),
	}

	return result.Items, paginationResp, nil
}

func convertLabels(labels map[string]string) map[string]interface{} {
	if labels == nil {
		return nil
	}
	result := make(map[string]interface{}, len(labels))
	for k, v := range labels {
		result[k] = v
	}
	return result
}

func (s *ImageService) GetTotalImageSize(ctx context.Context) (int64, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	images, err := dockerClient.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list images: %w", err)
	}

	var total int64
	for _, img := range images {
		total += img.Size
	}

	return total, nil
}

func buildInUseMap(containers []container.Summary) map[string]bool {
	inUseMap := make(map[string]bool)
	for _, c := range containers {
		inUseMap[c.ImageID] = true
	}
	return inUseMap
}

func buildUpdateMap(records []models.ImageUpdateRecord) map[string]*models.ImageUpdateRecord {
	updateMap := make(map[string]*models.ImageUpdateRecord, len(records))
	for i := range records {
		updateMap[records[i].ID] = &records[i]
	}
	return updateMap
}

func parseRepoAndTagFromRepoTag(repoTag string) (repo, tag string) {
	if named, err := ref.ParseNormalizedNamed(repoTag); err == nil {
		repo = ref.FamiliarName(named)
		if tagged, ok := named.(ref.NamedTagged); ok {
			tag = tagged.Tag()
		} else {
			tag = "latest"
		}
		return repo, tag
	}

	if lastColonIdx := strings.LastIndex(repoTag, ":"); lastColonIdx != -1 {
		return repoTag[:lastColonIdx], repoTag[lastColonIdx+1:]
	}
	return repoTag, "latest"
}

func parseRepoFromDigests(repoDigests []string) (repo string, found bool) {
	for _, rd := range repoDigests {
		if rd == "<none>@<none>" {
			continue
		}
		if at := strings.LastIndex(rd, "@"); at != -1 {
			candidateRepo := rd[:at]
			if candidateRepo != "" {
				return candidateRepo, true
			}
		}
	}
	return "", false
}

func determineRepoAndTag(di image.Summary) (repo, tag string) {
	if len(di.RepoTags) > 0 {
		return parseRepoAndTagFromRepoTag(di.RepoTags[0])
	}

	if len(di.RepoDigests) > 0 {
		if r, found := parseRepoFromDigests(di.RepoDigests); found {
			return r, "<none>"
		}
	}

	return "<none>", "<none>"
}

func stringPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func buildUpdateInfo(updateRecord *models.ImageUpdateRecord) *imagetypes.UpdateInfo {
	return &imagetypes.UpdateInfo{
		HasUpdate:      updateRecord.HasUpdate,
		UpdateType:     updateRecord.UpdateType,
		CurrentVersion: updateRecord.CurrentVersion,
		LatestVersion:  stringPtrValue(updateRecord.LatestVersion),
		CurrentDigest:  stringPtrValue(updateRecord.CurrentDigest),
		LatestDigest:   stringPtrValue(updateRecord.LatestDigest),
		CheckTime:      updateRecord.CheckTime,
		ResponseTimeMs: updateRecord.ResponseTimeMs,
		Error:          stringPtrValue(updateRecord.LastError),
		AuthMethod:     stringPtrValue(updateRecord.AuthMethod),
		AuthUsername:   stringPtrValue(updateRecord.AuthUsername),
		AuthRegistry:   stringPtrValue(updateRecord.AuthRegistry),
		UsedCredential: updateRecord.UsedCredential,
	}
}

func mapDockerImagesToDTOs(dockerImages []image.Summary, inUseMap map[string]bool, updateMap map[string]*models.ImageUpdateRecord) []imagetypes.Summary {
	items := make([]imagetypes.Summary, 0, len(dockerImages))
	for _, di := range dockerImages {
		repo, tag := determineRepoAndTag(di)

		imageDto := imagetypes.Summary{
			ID:          di.ID,
			Repo:        repo,
			Tag:         tag,
			RepoTags:    di.RepoTags,
			RepoDigests: di.RepoDigests,
			Created:     di.Created,
			Size:        di.Size,
			VirtualSize: di.SharedSize,
			Labels:      convertLabels(di.Labels),
			InUse:       inUseMap[di.ID],
		}

		if updateRecord, exists := updateMap[di.ID]; exists {
			imageDto.UpdateInfo = buildUpdateInfo(updateRecord)
		}

		items = append(items, imageDto)
	}
	return items
}

func (s *ImageService) getImagePaginationConfig() pagination.Config[imagetypes.Summary] {
	return pagination.Config[imagetypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[imagetypes.Summary]{
			func(i imagetypes.Summary) (string, error) { return i.Repo, nil },
			func(i imagetypes.Summary) (string, error) { return i.Tag, nil },
			func(i imagetypes.Summary) (string, error) { return i.ID, nil },
			func(i imagetypes.Summary) (string, error) {
				if len(i.RepoTags) > 0 {
					return i.RepoTags[0], nil
				}
				return "", nil
			},
		},
		SortBindings: []pagination.SortBinding[imagetypes.Summary]{
			{
				Key: "repo",
				Fn: func(a, b imagetypes.Summary) int {
					return strings.Compare(a.Repo, b.Repo)
				},
			},
			{
				Key: "tag",
				Fn: func(a, b imagetypes.Summary) int {
					return strings.Compare(a.Tag, b.Tag)
				},
			},
			{
				Key: "size",
				Fn: func(a, b imagetypes.Summary) int {
					if a.Size < b.Size {
						return -1
					}
					if a.Size > b.Size {
						return 1
					}
					return 0
				},
			},
			{
				Key: "created",
				Fn: func(a, b imagetypes.Summary) int {
					if a.Created < b.Created {
						return -1
					}
					if a.Created > b.Created {
						return 1
					}
					return 0
				},
			},
			{
				Key: "inUse",
				Fn: func(a, b imagetypes.Summary) int {
					if a.InUse == b.InUse {
						return 0
					}
					if a.InUse {
						return -1
					}
					return 1
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[imagetypes.Summary]{
			{
				Key: "inUse",
				Fn: func(i imagetypes.Summary, filterValue string) bool {
					if filterValue == "true" {
						return i.InUse
					}
					if filterValue == "false" {
						return !i.InUse
					}
					return true
				},
			},
			{
				Key: "updates",
				Fn: func(i imagetypes.Summary, filterValue string) bool {
					hasUpdate := i.UpdateInfo != nil && i.UpdateInfo.HasUpdate
					if filterValue == "true" {
						return hasUpdate
					}
					if filterValue == "false" {
						return !hasUpdate
					}
					return true
				},
			},
		},
	}
}
