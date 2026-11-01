// Package proxy owns the transport to remote environment agents: request
// building, agent tunnel routing, JSON decoding and configuration pushes.
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

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/sys/crypto"

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
		client          *agent.RemoteClient
		settingsService *settings.SettingsService
		syncGate        *concurrency.SyncGate
		syncLocks       concurrency.KeyedMutex
	}

	// Target is a validated remote environment endpoint; build it with NewTarget.
	Target struct {
		accessToken *string
		id          string
		name        string
	}
)

// New builds the remote transport. syncGate is owned by the parent so it can
// forget delivery state when an environment reconnects.
func New(db *database.DB, settingsService *settings.SettingsService, syncGate *concurrency.SyncGate) *Service {
	return &Service{
		db:              db,
		client:          agent.NewRemoteClient(agent.RemoteClientConfig{}),
		settingsService: settingsService,
		syncGate:        syncGate,
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

// Context bounds a proxied request by the configured proxy timeout.
func (s *Service) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().ProxyRequestTimeout.AsInt(), timeouts.DefaultProxyRequest))
}

// Execute sends one request to the target agent.
func (s *Service) Execute(
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

	resp, err := s.client.Do(ctx, agent.RemoteRequest{
		EnvironmentID: target.id,
		Method:        method,
		Path:          path,
		Headers:       requestHeaders,
		Body:          body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send request to environment %s: %w", target.name, err)
	}

	return resp, nil
}

func (s *Service) Stream(ctx context.Context, target *Target, path string) (*http.Response, error) {
	tunnel, ok := agent.GetRegistry().WaitForActive(ctx, target.id, agent.DefaultTunnelAcquireTimeout).Get()
	if !ok {
		return nil, &agent.TransportError{Err: agent.ErrEnvironmentUnavailable}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agent.MuxAgentBaseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	agent.SetAgentToken(req.Header, kit.FromPtr(target.accessToken))
	resp, err := tunnel.HTTPClient.Do(req)
	if err != nil {
		return nil, &agent.TransportError{Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &agent.StatusError{StatusCode: resp.StatusCode, Body: body}
	}
	return resp, nil
}

// JSON sends one request and decodes a successful JSON response into out.
func (s *Service) JSON(
	ctx context.Context,
	target *Target,
	method string,
	path string,
	body []byte,
	out any,
) error {
	resp, err := s.Execute(ctx, target, method, path, body)
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

// SyncRegistries syncs all registries from this manager to a remote environment
func (s *Service) SyncRegistries(ctx context.Context, target *Target) error {
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

// SyncS3Destinations sends manager-owned destinations to one remote environment.
func (s *Service) SyncS3Destinations(ctx context.Context, target *Target) error {
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

// SyncRepositories syncs all git repositories from this manager to a remote environment
func (s *Service) SyncRepositories(ctx context.Context, target *Target) error {
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

// fanOutSyncToEnvironment pushes every row of Model held by this
// manager to one remote environment. toSyncItem maps a row to its wire form and
// reports whether to keep it. Mapping errors abort before sending the snapshot.
// wrap builds the request envelope the target endpoint expects.
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
