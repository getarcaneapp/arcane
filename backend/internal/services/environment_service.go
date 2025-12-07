package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/google/uuid"
	"go.getarcane.app/types/containerregistry"
	"go.getarcane.app/types/environment"
	"gorm.io/gorm"
)

type EnvironmentService struct {
	db            *database.DB
	httpClient    *http.Client
	dockerService *DockerClientService
}

func NewEnvironmentService(db *database.DB, httpClient *http.Client, dockerService *DockerClientService) *EnvironmentService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &EnvironmentService{db: db, httpClient: httpClient, dockerService: dockerService}
}

func (s *EnvironmentService) EnsureLocalEnvironment(ctx context.Context, appUrl string) error {
	const localEnvID = "0"

	var existingEnv models.Environment
	err := s.db.WithContext(ctx).Where("id = ?", localEnvID).First(&existingEnv).Error

	if err == nil {
		// Local environment already exists, ensure ApiUrl matches current appUrl
		if existingEnv.ApiUrl != appUrl {
			if err := s.db.WithContext(ctx).Model(&existingEnv).Update("api_url", appUrl).Error; err != nil {
				return fmt.Errorf("failed to update local environment api url: %w", err)
			}
			slog.InfoContext(ctx, "updated local environment api url", "id", localEnvID, "url", appUrl)
		}
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check for local environment: %w", err)
	}

	// Create the local environment
	now := time.Now()
	localEnv := &models.Environment{
		BaseModel: models.BaseModel{
			ID:        localEnvID,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:    "Local Docker",
		ApiUrl:  appUrl,
		Status:  string(models.EnvironmentStatusOnline),
		Enabled: true,
	}

	if err := s.db.WithContext(ctx).Create(localEnv).Error; err != nil {
		return fmt.Errorf("failed to create local environment: %w", err)
	}

	slog.InfoContext(ctx, "created local environment record", "id", localEnvID)
	return nil
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, environment *models.Environment) (*models.Environment, error) {
	environment.ID = uuid.New().String()
	environment.Status = string(models.EnvironmentStatusOffline)
	now := time.Now()
	environment.CreatedAt = now
	environment.UpdatedAt = &now

	if err := s.db.WithContext(ctx).Create(environment).Error; err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	return environment, nil
}

func (s *EnvironmentService) GetEnvironmentByID(ctx context.Context, id string) (*models.Environment, error) {
	var environment models.Environment
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&environment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("environment not found")
		}
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}
	return &environment, nil
}

func (s *EnvironmentService) ListEnvironmentsPaginated(ctx context.Context, params pagination.QueryParams) ([]environment.Environment, pagination.Response, error) {
	var envs []models.Environment
	q := s.db.WithContext(ctx).Model(&models.Environment{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR api_url LIKE ?",
			searchPattern, searchPattern,
		)
	}

	if status := params.Filters["status"]; status != "" {
		q = q.Where("status = ?", status)
	}
	if enabled := params.Filters["enabled"]; enabled != "" {
		switch enabled {
		case "true", "1":
			q = q.Where("enabled = ?", true)
		case "false", "0":
			q = q.Where("enabled = ?", false)
		}
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &envs)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate environments: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, id string, updates map[string]interface{}) (*models.Environment, error) {
	now := time.Now()
	updates["updated_at"] = &now

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update environment: %w", err)
	}

	return s.GetEnvironmentByID(ctx, id)
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Delete(&models.Environment{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete environment: %w", err)
	}
	return nil
}

func (s *EnvironmentService) TestConnection(ctx context.Context, id string, customApiUrl *string) (string, error) {
	environment, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return "error", err
	}

	// Special handling for local Docker environment (ID "0")
	if id == "0" && customApiUrl == nil {
		return s.testLocalDockerConnection(ctx, id)
	}

	apiUrl := environment.ApiUrl
	if customApiUrl != nil && *customApiUrl != "" {
		apiUrl = *customApiUrl
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := strings.TrimRight(apiUrl, "/") + "/api/health"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return "offline", fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return "offline", fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
		}
		return "online", nil
	}

	if customApiUrl == nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusError))
	}
	return "error", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (s *EnvironmentService) testLocalDockerConnection(ctx context.Context, id string) (string, error) {
	// Test local Docker socket by pinging Docker
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.Ping(reqCtx)
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", fmt.Errorf("docker ping failed: %w", err)
	}

	_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
	return "online", nil
}

func (s *EnvironmentService) updateEnvironmentStatusInternal(ctx context.Context, id, status string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":     status,
		"last_seen":  &now,
		"updated_at": &now,
	}
	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment status: %w", err)
	}
	return nil
}

func (s *EnvironmentService) UpdateEnvironmentHeartbeat(ctx context.Context, id string) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_seen":  &now,
		"status":     string(models.EnvironmentStatusOnline),
		"updated_at": &now,
	}).Error; err != nil {
		return fmt.Errorf("failed to update environment heartbeat: %w", err)
	}
	return nil
}

func (s *EnvironmentService) PairAgentWithBootstrap(ctx context.Context, apiUrl, bootstrapToken string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, strings.TrimRight(apiUrl, "/")+"/api/environments/0/agent/pair", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Arcane-Agent-Bootstrap", bootstrapToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if !parsed.Success || parsed.Data.Token == "" {
		return "", fmt.Errorf("pairing unsuccessful")
	}

	return parsed.Data.Token, nil
}

func (s *EnvironmentService) PairAndPersistAgentToken(ctx context.Context, environmentID, apiUrl, bootstrapToken string) (string, error) {
	token, err := s.PairAgentWithBootstrap(ctx, apiUrl, bootstrapToken)
	if err != nil {
		return "", err
	}
	if err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("id = ?", environmentID).
		Update("access_token", token).Error; err != nil {
		return "", fmt.Errorf("failed to persist agent token: %w", err)
	}
	return token, nil
}

func (s *EnvironmentService) GetDB() *database.DB {
	return s.db
}

func (s *EnvironmentService) GetEnabledRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get enabled container registries: %w", err)
	}

	var creds []containerregistry.Credential
	for _, reg := range registries {
		if !reg.Enabled || reg.Username == "" || reg.Token == "" {
			continue
		}

		decryptedToken, err := utils.Decrypt(reg.Token)
		if err != nil {
			slog.WarnContext(ctx, "Failed to decrypt registry token",
				slog.String("registryURL", reg.URL),
				slog.String("error", err.Error()))
			continue
		}

		creds = append(creds, containerregistry.Credential{
			URL:      reg.URL,
			Username: reg.Username,
			Token:    decryptedToken,
			Enabled:  reg.Enabled,
		})
	}

	return creds, nil
}

// SyncRegistriesToEnvironment syncs all registries from this manager to a remote environment
func (s *EnvironmentService) SyncRegistriesToEnvironment(ctx context.Context, environmentID string) error {
	// Get the environment
	environment, err := s.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	// Don't sync to local environment (ID "0")
	if environmentID == "0" {
		return fmt.Errorf("cannot sync registries to local environment")
	}

	slog.InfoContext(ctx, "Starting registry sync to environment",
		slog.String("environmentID", environmentID),
		slog.String("environmentName", environment.Name),
		slog.String("apiUrl", environment.ApiUrl))

	// Get all registries from this manager
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&registries).Error; err != nil {
		return fmt.Errorf("failed to get registries: %w", err)
	}

	slog.InfoContext(ctx, "Found registries to sync",
		slog.Int("count", len(registries)))

	// Prepare sync items with decrypted tokens
	syncItems := make([]containerregistry.Sync, 0, len(registries))
	for _, reg := range registries {
		decryptedToken, err := utils.Decrypt(reg.Token)
		if err != nil {
			slog.WarnContext(ctx, "Failed to decrypt registry token for sync",
				slog.String("registryID", reg.ID),
				slog.String("registryURL", reg.URL),
				slog.String("error", err.Error()))
			continue
		}

		syncItems = append(syncItems, containerregistry.Sync{
			ID:          reg.ID,
			URL:         reg.URL,
			Username:    reg.Username,
			Token:       decryptedToken,
			Description: reg.Description,
			Insecure:    reg.Insecure,
			Enabled:     reg.Enabled,
			CreatedAt:   reg.CreatedAt,
			UpdatedAt:   reg.UpdatedAt,
		})
	}

	// Prepare the sync request
	syncReq := containerregistry.SyncRequest{
		Registries: syncItems,
	}

	// Marshal the request
	reqBody, err := json.Marshal(syncReq)
	if err != nil {
		return fmt.Errorf("failed to marshal sync request: %w", err)
	}

	// Send the sync request to the remote environment
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	targetURL := strings.TrimRight(environment.ApiUrl, "/") + "/api/container-registries/sync"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return fmt.Errorf("failed to create sync request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if environment.AccessToken != nil && *environment.AccessToken != "" {
		req.Header.Set("X-Arcane-Agent-Token", *environment.AccessToken)
		slog.DebugContext(ctx, "Set agent token header for sync request")
	} else {
		slog.WarnContext(ctx, "No access token available for environment sync",
			slog.String("environmentID", environmentID))
	}

	slog.InfoContext(ctx, "Sending sync request to agent",
		slog.String("url", targetURL),
		slog.Int("registryCount", len(syncItems)))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(ctx, "Sync request failed",
			slog.Int("statusCode", resp.StatusCode),
			slog.String("response", string(body)))
		return fmt.Errorf("sync request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode sync response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("sync failed: %s", result.Data.Message)
	}

	slog.InfoContext(ctx, "Successfully synced registries to environment",
		slog.String("environmentID", environmentID),
		slog.String("environmentName", environment.Name))

	return nil
}
