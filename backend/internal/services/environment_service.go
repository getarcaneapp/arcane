package services

import (
	"bytes"
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
	"github.com/getarcaneapp/arcane/types/containerregistry"
	"github.com/getarcaneapp/arcane/types/environment"
	"github.com/getarcaneapp/arcane/types/gitops"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EnvironmentService struct {
	db            *database.DB
	httpClient    *http.Client
	dockerService *DockerClientService
	eventService  *EventService
}

func NewEnvironmentService(db *database.DB, httpClient *http.Client, dockerService *DockerClientService, eventService *EventService) *EnvironmentService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &EnvironmentService{db: db, httpClient: httpClient, dockerService: dockerService, eventService: eventService}
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

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, environment *models.Environment, userID, username *string) (*models.Environment, error) {
	environment.ID = uuid.New().String()

	// Only set status to offline if not already set (e.g., API key flow sets it to pending)
	if environment.Status == "" {
		environment.Status = string(models.EnvironmentStatusOffline)
	}

	now := time.Now()
	environment.CreatedAt = now
	environment.UpdatedAt = &now

	if err := s.db.WithContext(ctx).Create(environment).Error; err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	// Create event in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), environment.ID, environment.Name, models.EventTypeEnvironmentCreate, "Environment Created", fmt.Sprintf("Environment '%s' was created", environment.Name), models.EventSeveritySuccess, userID, username)

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

func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, id string, updates map[string]interface{}, userID, username *string) (*models.Environment, error) {
	now := time.Now()
	updates["updated_at"] = &now

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update environment: %w", err)
	}

	updated, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Create event in background (skip for local environment)
	if id != "0" {
		go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, updated.Name, models.EventTypeEnvironmentUpdate, "Environment Updated", fmt.Sprintf("Environment '%s' was updated", updated.Name), models.EventSeverityInfo, userID, username)
	}

	return updated, nil
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id string, userID, username *string) error {
	// Get environment details before deletion
	env, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Delete(&models.Environment{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete environment: %w", err)
	}

	// Create event in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, env.Name, models.EventTypeEnvironmentDelete, "Environment Deleted", fmt.Sprintf("Environment '%s' was deleted", env.Name), models.EventSeverityWarning, userID, username)

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
	// Don't update status for pending environments - they're waiting for agent pairing
	var currentEnv models.Environment
	if err := s.db.WithContext(ctx).Select("status").Where("id = ?", id).First(&currentEnv).Error; err != nil {
		return fmt.Errorf("failed to check environment status: %w", err)
	}

	if currentEnv.Status == string(models.EnvironmentStatusPending) {
		slog.DebugContext(ctx, "skipping status update for pending environment", "environment_id", id)
		return nil
	}

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

	// Use Exec with raw SQL for better performance
	// Only update if last_seen is NULL or older than 30 seconds to reduce write frequency
	result := s.db.WithContext(ctx).Exec(`
		UPDATE environments 
		SET last_seen = ?, status = ?, updated_at = ?
		WHERE id = ? 
		AND (last_seen IS NULL OR last_seen < ?)
	`, &now, string(models.EnvironmentStatusOnline), &now, id, now.Add(-30*time.Second))

	if result.Error != nil {
		return fmt.Errorf("failed to update environment heartbeat: %w", result.Error)
	}

	return nil
}
func (s *EnvironmentService) createEnvironmentEvent(ctx context.Context, envID, envName string, eventType models.EventType, title, description string, severity models.EventSeverity, userID, username *string) {
	resourceType := "environment"
	resourceID := envID
	resourceName := envName
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &resourceID,
		ResourceName:  &resourceName,
		UserID:        userID,
		Username:      username,
		EnvironmentID: &envID,
	})
}

func (s *EnvironmentService) RegenerateEnvironmentApiKey(ctx context.Context, envID string, newApiKeyID string, encryptedKey string, userID, username string, envName string) error {
	// Update environment with new API key and set to pending status
	updates := map[string]interface{}{
		"api_key_id":   newApiKeyID,
		"access_token": encryptedKey,
		"status":       string(models.EnvironmentStatusPending),
		"last_seen":    nil, // Clear last seen time
	}

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", envID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment with new API key: %w", err)
	}

	// Create event log in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), envID, envName, models.EventTypeEnvironmentApiKeyRegenerated, "API Key Regenerated", "Environment API key was regenerated and status set to pending", models.EventSeverityInfo, &userID, &username)

	return nil
}

// Deprecated - Use the Api Key flow
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
			slog.WarnContext(ctx, "Failed to decrypt registry token", "registryURL", reg.URL, "error", err.Error())
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

// DeploymentSnippets contains deployment configuration snippets for an environment.
type DeploymentSnippets struct {
	DockerRun     string
	DockerCompose string
}

// GenerateDeploymentSnippets generates Docker deployment snippets for an environment.
func (s *EnvironmentService) GenerateDeploymentSnippets(ctx context.Context, envID string, envAddress string, apiKey string) (*DeploymentSnippets, error) {
	managerURL := strings.TrimRight(envAddress, "/")

	dockerRun := fmt.Sprintf(`docker run -d \
  --name arcane-agent \
  --restart unless-stopped \
  -e AGENT_MODE=true \
  -e AGENT_TOKEN=%s \
  -e MANAGER_API_URL=%s \
  -p 3553:3553 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v arcane-data:/data \
  ghcr.io/getarcaneapp/arcane-headless:latest`, apiKey, managerURL)

	dockerCompose := fmt.Sprintf(`services:
  arcane-agent:
    image: ghcr.io/getarcaneapp/arcane-headless:latest
    container_name: arcane-agent
    restart: unless-stopped
    environment:
      - AGENT_MODE=true
      - AGENT_TOKEN=%s
      - MANAGER_API_URL=%s
    ports:
      - "3553:3553"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - arcane-data:/app/data

volumes:
  arcane-data:`, apiKey, managerURL)

	return &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}, nil
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

	slog.InfoContext(ctx, "Starting registry sync to environment", "environmentID", environmentID, "environmentName", environment.Name, "apiUrl", environment.ApiUrl)

	// Get all registries from this manager
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&registries).Error; err != nil {
		return fmt.Errorf("failed to get registries: %w", err)
	}

	slog.InfoContext(ctx, "Found registries to sync", "count", len(registries))

	// Prepare sync items with decrypted tokens
	syncItems := make([]containerregistry.Sync, 0, len(registries))
	for _, reg := range registries {
		decryptedToken, err := utils.Decrypt(reg.Token)
		if err != nil {
			slog.WarnContext(ctx, "Failed to decrypt registry token for sync", "registryID", reg.ID, "registryURL", reg.URL, "error", err.Error())
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
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create sync request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Use appropriate auth header based on environment type
	if environment.AccessToken != nil && *environment.AccessToken != "" {
		req.Header.Set("X-Arcane-Agent-Token", *environment.AccessToken)
		req.Header.Set("X-API-Key", *environment.AccessToken)
		slog.DebugContext(ctx, "Set auth headers for sync request")
	} else {
		slog.WarnContext(ctx, "No access token available for environment sync", "environmentID", environmentID)
	}

	slog.InfoContext(ctx, "Sending sync request to agent", "url", targetURL, "registryCount", len(syncItems))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(ctx, "Sync request failed", "statusCode", resp.StatusCode, "response", string(body))
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

	slog.InfoContext(ctx, "Successfully synced registries to environment", "environmentID", environmentID, "environmentName", environment.Name)

	return nil
}

// SyncRepositoriesToEnvironment syncs all git repositories from this manager to a remote environment
func (s *EnvironmentService) SyncRepositoriesToEnvironment(ctx context.Context, environmentID string) error {
	// Get the environment
	environment, err := s.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	// Don't sync to local environment (ID "0")
	if environmentID == "0" {
		return fmt.Errorf("cannot sync repositories to local environment")
	}

	slog.InfoContext(ctx, "Starting git repository sync to environment", "environmentID", environmentID, "environmentName", environment.Name, "apiUrl", environment.ApiUrl)

	// Get all git repositories from this manager
	var repositories []models.GitRepository
	if err := s.db.WithContext(ctx).Find(&repositories).Error; err != nil {
		return fmt.Errorf("failed to get git repositories: %w", err)
	}

	slog.InfoContext(ctx, "Found git repositories to sync", "count", len(repositories))

	// Prepare sync items with decrypted credentials
	syncItems := make([]gitops.RepositorySync, 0, len(repositories))
	for _, repo := range repositories {
		item := gitops.RepositorySync{
			ID:          repo.ID,
			Name:        repo.Name,
			URL:         repo.URL,
			AuthType:    repo.AuthType,
			Username:    repo.Username,
			Description: repo.Description,
			Enabled:     repo.Enabled,
			CreatedAt:   repo.CreatedAt,
		}
		if repo.UpdatedAt != nil {
			item.UpdatedAt = *repo.UpdatedAt
		}

		// Decrypt token if present
		if repo.Token != "" {
			decryptedToken, err := utils.Decrypt(repo.Token)
			if err != nil {
				slog.WarnContext(ctx, "Failed to decrypt repository token for sync", "repositoryID", repo.ID, "repositoryName", repo.Name, "error", err.Error())
				continue
			}
			item.Token = decryptedToken
		}

		// Decrypt SSH key if present
		if repo.SSHKey != "" {
			decryptedSSHKey, err := utils.Decrypt(repo.SSHKey)
			if err != nil {
				slog.WarnContext(ctx, "Failed to decrypt repository SSH key for sync", "repositoryID", repo.ID, "repositoryName", repo.Name, "error", err.Error())
				continue
			}
			item.SSHKey = decryptedSSHKey
		}

		syncItems = append(syncItems, item)
	}

	// Prepare the sync request
	syncReq := gitops.RepositorySyncRequest{
		Repositories: syncItems,
	}

	// Marshal the request
	reqBody, err := json.Marshal(syncReq)
	if err != nil {
		return fmt.Errorf("failed to marshal sync request: %w", err)
	}

	// Send the sync request to the remote environment
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	targetURL := strings.TrimRight(environment.ApiUrl, "/") + "/api/git-repositories/sync"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create sync request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Use appropriate auth header based on environment type
	if environment.AccessToken != nil && *environment.AccessToken != "" {
		req.Header.Set("X-Arcane-Agent-Token", *environment.AccessToken)
		req.Header.Set("X-API-Key", *environment.AccessToken)
		slog.DebugContext(ctx, "Set auth headers for git repository sync request")
	} else {
		slog.WarnContext(ctx, "No access token available for environment git repository sync", "environmentID", environmentID)
	}

	slog.InfoContext(ctx, "Sending git repository sync request to agent", "url", targetURL, "repositoryCount", len(syncItems))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(ctx, "Git repository sync request failed", "statusCode", resp.StatusCode, "response", string(body))
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

	slog.InfoContext(ctx, "Successfully synced git repositories to environment", "environmentID", environmentID, "environmentName", environment.Name)

	return nil
}

// ProxyRequest sends a request to a remote environment's API.
func (s *EnvironmentService) ProxyRequest(ctx context.Context, envID string, method string, path string, body []byte) ([]byte, int, error) {
	environment, err := s.GetEnvironmentByID(ctx, envID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get environment: %w", err)
	}

	if envID == "0" {
		return nil, 0, fmt.Errorf("cannot proxy request to local environment")
	}

	targetURL := strings.TrimRight(environment.ApiUrl, "/") + path
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	if method != http.MethodGet && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	// Use appropriate auth header
	if environment.AccessToken != nil && *environment.AccessToken != "" {
		req.Header.Set("X-Arcane-Agent-Token", *environment.AccessToken)
		req.Header.Set("X-API-Key", *environment.AccessToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}
