package proxy

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/gitrepo"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/settings"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		db              *database.DB
		tunnels         *agent.TunnelRegistry
		settingsService *settings.SettingsService
		syncGate        *concurrency.SyncGate
		syncLocks       concurrency.KeyedMutex
	}

	Config struct {
		DB              *database.DB
		Tunnels         *agent.TunnelRegistry
		SettingsService *settings.SettingsService
		SyncGate        *concurrency.SyncGate
	}

	Target struct {
		accessToken *string
		id          string
		name        string
	}
)

func New(cfg Config) *Service {
	return &Service{
		db:              cfg.DB,
		tunnels:         cfg.Tunnels,
		settingsService: cfg.SettingsService,
		syncGate:        cfg.SyncGate,
	}
}

func NewTarget(id, name string, accessToken *string) (*Target, error) {
	if id == "0" {
		return nil, errors.New("cannot proxy request to local environment")
	}

	return &Target{
		id:          id,
		name:        name,
		accessToken: accessToken,
	}, nil
}

func (s *Service) TargetByID(ctx context.Context, envID string) (*Target, error) {
	var row struct {
		AccessToken *string
		ID          string
		Name        string
	}
	if err := s.db.WithContext(ctx).
		Table("environments").
		Select("id", "name", "access_token").
		Where("id = ?", envID).
		Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to get environment: %w", common.Classify(common.ErrNotFound, errors.New("environment not found")))
		}
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return NewTarget(row.ID, row.Name, row.AccessToken)
}

func (s *Service) ProxyJSONRequest(ctx context.Context, envID, method, path string, body []byte, out any) error {
	proxyCtx, cancel := s.Context(ctx)
	defer cancel()

	target, err := s.TargetByID(proxyCtx, envID)
	if err != nil {
		return err
	}

	return s.JSON(proxyCtx, target, method, path, body, out)
}

func (s *Service) ExecuteRemoteRequest(ctx context.Context, envID, method, path string, body []byte) (*agent.RemoteResponse, error) {
	target, err := s.TargetByID(ctx, envID)
	if err != nil {
		return nil, err
	}

	return s.execute(ctx, target, method, path, body)
}

func (s *Service) ProxyRequest(ctx context.Context, envID, method, path string, body []byte) ([]byte, int, error) {
	proxyCtx, cancel := s.Context(ctx)
	defer cancel()

	resp, err := s.ExecuteRemoteRequest(proxyCtx, envID, method, path, body)
	if err != nil {
		return nil, 0, err
	}

	return resp.Body, resp.StatusCode, nil
}

func (s *Service) ForgetSyncState(envID string) {
	s.syncGate.Forget(envID)
}

func (s *Service) ListEnabledRemoteEnvironmentIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).
		Table("environments").
		Where("enabled = ?", true).
		Where("id <> ?", "0").
		Where("access_token IS NOT NULL AND TRIM(access_token) <> ''").
		Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list enabled remote environments: %w", err)
	}
	return ids, nil
}

func (s *Service) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().ProxyRequestTimeout.AsInt(), timeouts.DefaultProxyRequest))
}

func (s *Service) execute(
	ctx context.Context,
	target *Target,
	method string,
	path string,
	body []byte,
) (*agent.RemoteResponse, error) {
	// Forward the activity batch ID so bulk actions proxied to a remote
	// environment group the same way they do locally.
	requestHeaders := make(http.Header, 3)
	if batchID := activitylib.ActivityBatchIDFromContext(ctx); batchID != "" {
		requestHeaders.Set(middleware.HeaderActivityBatchID, batchID)
	}
	if len(body) > 0 && method != http.MethodGet {
		requestHeaders.Set("Content-Type", "application/json")
	}
	agent.SetAgentToken(requestHeaders, kit.FromPtr(target.accessToken))

	resp, err := s.tunnels.Do(ctx, agent.RemoteRequest{
		Headers:       requestHeaders,
		EnvironmentID: target.id,
		Method:        method,
		Path:          path,
		Body:          body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send request to environment %s: %w", target.name, err)
	}

	return resp, nil
}

func (s *Service) OpenRemoteStream(ctx context.Context, envID, path string) (*http.Response, error) {
	target, err := s.TargetByID(ctx, envID)
	if err != nil {
		return nil, err
	}
	var lost *agent.AgentTunnel
	var resp *http.Response
	for {
		tunnel, ok := s.tunnels.WaitForActive(ctx, target.id, agent.WaitForActiveOptions{
			Except:  lost,
			Timeout: kit.Ternary(lost != nil, agent.DefaultTunnelReconnectTimeout, agent.DefaultTunnelAcquireTimeout),
		}).Get()
		if !ok {
			return nil, &agent.TransportError{Err: agent.ErrEnvironmentUnavailable}
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, agent.MuxAgentBaseURL+path, http.NoBody)
		if reqErr != nil {
			return nil, fmt.Errorf("create stream request: %w", reqErr)
		}
		req.Header.Set("Accept", "text/event-stream")
		agent.SetAgentToken(req.Header, kit.FromPtr(target.accessToken))
		resp, err = tunnel.HTTPClient.Do(req)
		if err == nil {
			break
		}
		if lost == nil && tunnel.RetryAfterLoss(req, err) {
			lost = tunnel
			continue
		}
		return nil, &agent.TransportError{Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &agent.StatusError{StatusCode: resp.StatusCode, Body: body}
	}
	return resp, nil
}

func (s *Service) JSON(
	ctx context.Context,
	target *Target,
	method string,
	path string,
	body []byte,
	out any,
) error {
	resp, err := s.execute(ctx, target, method, path, body)
	if err != nil {
		return err
	}
	if requireSuccessErr := resp.RequireSuccess(); requireSuccessErr != nil {
		return requireSuccessErr
	}
	// This is the erased decode behind the RemoteJSONProxy func-value seam;
	// typed callers go through RemoteJSONProxy.JSON.
	if out == nil {
		return nil
	}
	if unmarshalErr := json.Unmarshal(resp.Body, out); unmarshalErr != nil {
		return &agent.DecodeError{Err: unmarshalErr}
	}

	return nil
}

func (s *Service) SyncRegistriesToEnvironment(ctx context.Context, envID string) error {
	target, targetErr := s.TargetByID(ctx, envID)
	if targetErr != nil {
		return targetErr
	}
	return s.fanOutSyncToEnvironment(ctx, target, "registries", "/api/registries/sync",
		func(reg registry.ContainerRegistry) (types.ContainerRegistrySync, bool, error) {
			registryType, typeErr := registry.NormalizeRegistryType(reg.RegistryType)
			if typeErr != nil {
				return types.ContainerRegistrySync{}, false, fmt.Errorf("normalize registry type for sync %s: %w", reg.ID, typeErr)
			}

			decrypted, err := crypto.Decrypt(kit.Ternary(registryType == registry.RegistryTypeECR, reg.AWSSecretAccessKey, reg.Token))
			if err != nil {
				return types.ContainerRegistrySync{}, false, fmt.Errorf("failed to decrypt %s for registry %s for sync: %w",
					kit.Ternary(registryType == registry.RegistryTypeECR, "ECR secret", "token"), reg.ID, err)
			}

			return types.ContainerRegistrySync{
				ID:                 reg.ID,
				URL:                reg.URL,
				Username:           kit.Ternary(registryType == registry.RegistryTypeECR, "", reg.Username),
				Token:              kit.Ternary(registryType == registry.RegistryTypeECR, "", decrypted),
				Description:        reg.Description,
				Insecure:           reg.Insecure,
				Enabled:            reg.Enabled,
				RegistryType:       registryType,
				RepositoryNames:    reg.RepositoryNames,
				AWSAccessKeyID:     kit.Ternary(registryType == registry.RegistryTypeECR, reg.AWSAccessKeyID, ""),
				AWSSecretAccessKey: kit.Ternary(registryType == registry.RegistryTypeECR, decrypted, ""),
				AWSRegion:          kit.Ternary(registryType == registry.RegistryTypeECR, reg.AWSRegion, ""),
				CreatedAt:          reg.CreatedAt,
				UpdatedAt:          reg.UpdatedAt,
			}, true, nil
		},
		func(items []types.ContainerRegistrySync) types.ContainerRegistrySyncRequest {
			return types.ContainerRegistrySyncRequest{Registries: items}
		},
	)
}

func (s *Service) SyncS3DestinationsToEnvironment(ctx context.Context, envID string) error {
	target, targetErr := s.TargetByID(ctx, envID)
	if targetErr != nil {
		return targetErr
	}
	return s.fanOutSyncToEnvironment(ctx, target, "S3 destinations", "/api/backups/s3/sync",
		func(destination s3.S3Destination) (types.S3Destination, bool, error) {
			secret, err := crypto.Decrypt(destination.SecretAccessKey)
			if err != nil {
				return types.S3Destination{}, false, fmt.Errorf("failed to decrypt S3 destination %s for sync: %w", destination.ID, err)
			}
			return destination.ToSync(secret), true, nil
		},
		func(items []types.S3Destination) types.S3DestinationSyncRequest {
			return types.S3DestinationSyncRequest{Destinations: items}
		},
	)
}

func (s *Service) SyncRepositoriesToEnvironment(ctx context.Context, envID string) error {
	target, targetErr := s.TargetByID(ctx, envID)
	if targetErr != nil {
		return targetErr
	}
	return s.fanOutSyncToEnvironment(ctx, target, "git repositories", "/api/git/repositories/sync",
		func(repo gitrepo.GitRepository) (types.GitRepositorySync, bool, error) {
			decrypt := func(value, what string) (string, error) {
				if value == "" {
					return "", nil
				}
				plain, err := crypto.Decrypt(value)
				if err != nil {
					return "", fmt.Errorf("failed to decrypt %s for repository %s for sync: %w", what, repo.ID, err)
				}
				return plain, nil
			}
			token, err := decrypt(repo.Token, "token")
			if err != nil {
				return types.GitRepositorySync{}, false, err
			}
			sshKey, err := decrypt(repo.SSHKey, "SSH key")
			if err != nil {
				return types.GitRepositorySync{}, false, err
			}
			signingKey, err := decrypt(repo.SigningKey, "signing key")
			if err != nil {
				return types.GitRepositorySync{}, false, err
			}
			passphrase, err := decrypt(repo.SigningKeyPassphrase, "signing key passphrase")
			if err != nil {
				return types.GitRepositorySync{}, false, err
			}

			return types.GitRepositorySync{
				ID:                     repo.ID,
				Name:                   repo.Name,
				URL:                    repo.URL,
				Provider:               repo.Provider,
				AuthType:               repo.AuthType,
				Username:               repo.Username,
				Token:                  token,
				SSHKey:                 sshKey,
				SSHHostKeyVerification: repo.SSHHostKeyVerification,
				CommitAuthorName:       repo.CommitAuthorName,
				CommitAuthorEmail:      repo.CommitAuthorEmail,
				SigningKey:             signingKey,
				SigningKeyPassphrase:   passphrase,
				Description:            repo.Description,
				Enabled:                repo.Enabled,
				CreatedAt:              repo.CreatedAt,
				UpdatedAt:              kit.FromPtr(repo.UpdatedAt),
			}, true, nil
		},
		func(items []types.GitRepositorySync) types.GitRepositorySyncRequest {
			return types.GitRepositorySyncRequest{Repositories: items}
		},
	)
}

func (s *Service) fanOutSyncToEnvironment[Model, Item, Request any](
	ctx context.Context,
	target *Target,
	kind string,
	path string,
	toSyncItem func(Model) (Item, bool, error),
	wrap func([]Item) Request,
) error {
	unlock, err := s.syncLocks.LockContext(ctx, target.id+"\x00"+path)
	if err != nil {
		return fmt.Errorf("sync cancelled while waiting for an in-flight sync: %w", err)
	}
	defer unlock()

	var records []Model
	if loadRecordsErr := s.db.WithContext(ctx).Find(&records).Error; loadRecordsErr != nil {
		return fmt.Errorf("failed to get %s: %w", kind, loadRecordsErr)
	}

	syncItems := make([]Item, 0, len(records))
	for _, record := range records {
		item, keep, toSyncItemErr := toSyncItem(record)
		if toSyncItemErr != nil {
			return toSyncItemErr
		}
		if keep {
			syncItems = append(syncItems, item)
		}
	}

	reqBody, err := json.Marshal(wrap(syncItems))
	if err != nil {
		return fmt.Errorf("failed to marshal sync request: %w", err)
	}
	unchanged, finishDelivery, err := s.syncGate.Begin(ctx, target.id, path, reqBody)
	if err != nil {
		return fmt.Errorf("sync cancelled while waiting for an in-flight delivery: %w", err)
	}
	if unchanged {
		slog.DebugContext(ctx, "Skipping sync; payload unchanged since last delivery", "kind", kind, "environmentId", target.id, "environmentName", target.name)
		return nil
	}
	delivered := false
	defer func() { finishDelivery(delivered) }()

	slog.InfoContext(ctx, "Starting sync to environment", "kind", kind, "environmentId", target.id, "environmentName", target.name)

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	slog.InfoContext(ctx, "Sending sync request to agent", "kind", kind, "path", path, "count", len(syncItems))

	var result struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
		Success bool `json:"success"`
	}
	if proxyJSONRequestForTargetErr := s.JSON(reqCtx, target, http.MethodPost, path, reqBody, &result); proxyJSONRequestForTargetErr != nil {
		return fmt.Errorf("failed to send sync request: %w", proxyJSONRequestForTargetErr)
	}

	if !result.Success {
		return fmt.Errorf("sync failed: %s", result.Data.Message)
	}
	delivered = true

	slog.InfoContext(ctx, "Successfully synced to environment", "kind", kind, "environmentId", target.id, "environmentName", target.name)

	return nil
}
