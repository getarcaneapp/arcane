package environment

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"

	"context"
	"encoding/json/v2"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"go.getarcane.app/sys/crypto"
)

// SyncRegistriesToRemoteEnvironments syncs container registries to all eligible remote environments.
// Eligibility requires a non-local, enabled environment with a configured access token.
func (s *EnvironmentService) SyncRegistriesToRemoteEnvironments(ctx context.Context) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to list remote environments for registry sync")
	}

	if len(envs) == 0 {
		return nil
	}

	var failedCount int
	for _, env := range envs {
		if env.AccessToken == nil || *env.AccessToken == "" {
			slog.DebugContext(ctx, "Skipping registry sync for environment without access token",
				"environmentID", env.ID,
				"environmentName", env.Name)
			continue
		}

		if err := s.SyncRegistriesToEnvironment(ctx, env.ID); err != nil {
			failedCount++
			slog.WarnContext(ctx, "Failed to sync registries to remote environment",
				"environmentID", env.ID,
				"environmentName", env.Name,
				"error", err.Error())
		}
	}

	if failedCount > 0 {
		return errors.Errorf("failed to sync registries to %d remote environment(s)", failedCount)
	}

	return nil
}

// SyncS3DestinationsToRemoteEnvironments refreshes the destination cache on every enabled remote environment.
func (s *EnvironmentService) SyncS3DestinationsToRemoteEnvironments(ctx context.Context) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to list remote environments for S3 destination sync")
	}

	var failedCount int
	for _, env := range envs {
		if env.AccessToken == nil || strings.TrimSpace(*env.AccessToken) == "" {
			slog.DebugContext(ctx, "Skipping S3 destination sync for environment without access token", "environmentID", env.ID, "environmentName", env.Name)
			continue
		}
		if err := s.SyncS3DestinationsToEnvironment(ctx, env.ID); err != nil {
			failedCount++
			slog.WarnContext(ctx, "Failed to sync S3 destinations to remote environment", "environmentID", env.ID, "environmentName", env.Name, "error", err)
		}
	}

	if failedCount > 0 {
		return errors.Errorf("failed to sync S3 destinations to %d remote environment(s)", failedCount)
	}
	return nil
}

// CheckS3DestinationReferences returns an error while any managed environment
// still references the destination, or when a synced environment cannot be
// checked conclusively. Deleting credentials that a remote environment still
// needs would strand its policies and retained backups.
func (s *EnvironmentService) CheckS3DestinationReferences(ctx context.Context, destinationID string) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to list remote environments for S3 destination reference check")
	}
	for _, env := range envs {
		if env.AccessToken == nil || strings.TrimSpace(*env.AccessToken) == "" {
			// Environments without an access token never receive destination
			// syncs, so they cannot hold a reference.
			continue
		}
		var result struct {
			InUse bool `json:"inUse"`
		}
		if err := s.ProxyJSONRequestForEnvironment(ctx, env, http.MethodGet, "/api/backups/s3/"+url.PathEscape(destinationID)+"/in-use", nil, &result); err != nil {
			return errors.WrapIff(err, "cannot verify S3 destination references on environment %s; restore connectivity before deleting", env.Name)
		}
		if result.InUse {
			return errors.Errorf("still referenced by environment %s", env.Name)
		}
	}
	return nil
}

type remoteEnvironmentTargetInternal struct {
	ID          string
	Name        string
	IsEdge      bool
	AccessToken *string
	TargetURL   string
}

func (s *EnvironmentService) resolveRemoteEnvironmentTargetInternal(ctx context.Context, envID string) (*remoteEnvironmentTargetInternal, error) {
	envRecord, err := s.GetEnvironmentByID(ctx, envID)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get environment")
	}

	return s.remoteEnvironmentTargetFromModelInternal(*envRecord)
}

func (s *EnvironmentService) remoteEnvironmentTargetFromModelInternal(environment Environment) (*remoteEnvironmentTargetInternal, error) {
	if environment.ID == "0" {
		return nil, errors.New("cannot proxy request to local environment")
	}

	targetURL := strings.TrimRight(environment.ApiUrl, "/")
	if !environment.IsEdge {
		validatedTargetURL, err := httputils.NormalizeBaseURL(environment.ApiUrl)
		if err != nil {
			return nil, errors.WrapIf(err, "invalid environment API URL")
		}
		targetURL = validatedTargetURL
	}

	return &remoteEnvironmentTargetInternal{
		ID:          environment.ID,
		Name:        environment.Name,
		IsEdge:      environment.IsEdge,
		AccessToken: environment.AccessToken,
		TargetURL:   targetURL,
	}, nil
}

func buildEnvironmentEndpointURLInternal(apiURL, endpointPath string) (string, error) {
	baseURL, err := httputils.NormalizeBaseURL(apiURL)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(baseURL, "/") + endpointPath, nil
}

func (s *EnvironmentService) getProxyRequestContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	if s != nil && s.settingsService != nil {
		settings := s.settingsService.GetSettingsConfig()
		return context.WithTimeout(ctx, timeouts.GetDuration(settings.ProxyRequestTimeout.AsInt(), timeouts.DefaultProxyRequest))
	}

	return context.WithTimeout(ctx, timeouts.DefaultProxyRequest)
}

func (s *EnvironmentService) buildRemoteRequestInternal(
	target *remoteEnvironmentTargetInternal,
	method string,
	path string,
	body []byte,
	headers map[string]string,
) (remenv.Request, error) {
	if target == nil {
		return remenv.Request{}, errors.New("remote environment target is required")
	}

	requestHeaders := make(map[string]string, len(headers)+2)
	maps.Copy(requestHeaders, headers)
	if len(body) > 0 && method != http.MethodGet && requestHeaders["Content-Type"] == "" {
		requestHeaders["Content-Type"] = "application/json"
	}
	remenv.ApplyAgentTokenHeaderMap(requestHeaders, target.AccessToken)

	return remenv.Request{
		EnvironmentID: target.ID,
		IsEdge:        target.IsEdge,
		Method:        method,
		URL:           target.TargetURL + path,
		Path:          path,
		Headers:       requestHeaders,
		Body:          body,
	}, nil
}

func (s *EnvironmentService) ExecuteRemoteRequest(ctx context.Context, envID string, method string, path string, body []byte) (*remenv.Response, error) {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, envID)
	if err != nil {
		return nil, err
	}

	return s.executeRemoteRequestForTargetInternal(ctx, target, method, path, body)
}

func (s *EnvironmentService) executeRemoteRequestForTargetInternal(
	ctx context.Context,
	target *remoteEnvironmentTargetInternal,
	method string,
	path string,
	body []byte,
) (*remenv.Response, error) {
	// Forward the activity batch ID so bulk actions proxied to a remote
	// environment group the same way they do locally.
	var headers map[string]string
	if batchID := utils.ActivityBatchIDFromContext(ctx); batchID != "" {
		headers = map[string]string{utils.HeaderActivityBatchID: batchID}
	}
	request, err := s.buildRemoteRequestInternal(target, method, path, body, headers)
	if err != nil {
		return nil, err
	}

	resp, err := s.remoteClient.Do(ctx, request)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to send request to environment %s", target.Name)
	}

	return resp, nil
}

func (s *EnvironmentService) ProxyJSONRequest(ctx context.Context, envID string, method string, path string, body []byte, out any) error {
	proxyCtx, cancel := s.getProxyRequestContextInternal(ctx)
	defer cancel()

	target, err := s.resolveRemoteEnvironmentTargetInternal(proxyCtx, envID)
	if err != nil {
		return err
	}

	return s.proxyJSONRequestForTargetInternal(proxyCtx, target, method, path, body, out)
}

// ProxyJSONRequestForEnvironment sends a JSON request using an already-loaded
// environment row, avoiding an extra environment lookup on hot stream paths.
func (s *EnvironmentService) ProxyJSONRequestForEnvironment(ctx context.Context, environment Environment, method string, path string, body []byte, out any) error {
	proxyCtx, cancel := s.getProxyRequestContextInternal(ctx)
	defer cancel()

	target, err := s.remoteEnvironmentTargetFromModelInternal(environment)
	if err != nil {
		return err
	}

	return s.proxyJSONRequestForTargetInternal(proxyCtx, target, method, path, body, out)
}

func (s *EnvironmentService) proxyJSONRequestForTargetInternal(
	ctx context.Context,
	target *remoteEnvironmentTargetInternal,
	method string,
	path string,
	body []byte,
	out any,
) error {
	resp, err := s.executeRemoteRequestForTargetInternal(ctx, target, method, path, body)
	if err != nil {
		return err
	}
	if err := resp.RequireSuccess(); err != nil {
		return err
	}
	// This is the erased decode behind the RemoteJSONProxy func-value seam;
	// typed callers go through RemoteJSONProxy.JSON or remenv's generic decode.
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return &remenv.DecodeError{Err: err}
	}

	return nil
}

func ensureRemoteEnvironmentTunnelAvailableInternal(ctx context.Context, envID string) error {
	if edge.HasActiveTunnel(envID) {
		return nil
	}

	if _, ok := edge.RequestTunnelAndWait(ctx, envID, edge.DefaultTunnelDemandTTL, edge.DefaultTunnelAcquireTimeout()).Get(); ok {
		return nil
	}

	return errors.New("edge agent is not connected")
}

func doRemoteEnvironmentTunnelRequestInternal(
	ctx context.Context,
	envID string,
	method string,
	path string,
	headers map[string]string,
	body []byte,
) (*remenv.Response, error) {
	tunnel, ok := edge.GetRegistry().Get(envID).Get()
	if !ok {
		return nil, errors.Errorf("no active tunnel for environment %s", envID)
	}
	if tunnel.Conn.IsClosed() {
		return nil, errors.Errorf("tunnel for environment %s is closed", envID)
	}

	statusCode, respHeaders, respBody, err := edge.ProxyRequest(ctx, tunnel, method, path, "", headers, body)
	if err != nil {
		return nil, errors.WrapIf(err, "tunnel request failed")
	}

	return &remenv.Response{
		StatusCode: statusCode,
		Body:       respBody,
		Headers:    respHeaders,
	}, nil
}

// SyncRegistriesToEnvironment syncs all registries from this manager to a remote environment
func (s *EnvironmentService) SyncRegistriesToEnvironment(ctx context.Context, environmentID string) error {
	return s.fanOutSyncToEnvironment(ctx, environmentID, "registries", "/api/container-registries/sync",
		func(ctx context.Context, reg registry.ContainerRegistry) (containerregistry.Sync, bool, error) {
			registryType, typeErr := registry.NormalizeRegistryType(reg.RegistryType)
			if typeErr != nil {
				return containerregistry.Sync{}, false, errors.WrapIff(typeErr, "normalize registry type for sync %s", reg.ID)
			}

			syncItem := containerregistry.Sync{
				ID:              reg.ID,
				URL:             reg.URL,
				Description:     reg.Description,
				Insecure:        reg.Insecure,
				Enabled:         reg.Enabled,
				RegistryType:    registryType,
				RepositoryNames: reg.RepositoryNames,
				CreatedAt:       reg.CreatedAt,
				UpdatedAt:       reg.UpdatedAt,
			}

			if registryType == registry.RegistryTypeECR {
				decryptedSecret, err := crypto.Decrypt(reg.AWSSecretAccessKey)
				if err != nil {
					slog.WarnContext(ctx, "Failed to decrypt ECR secret for sync", "registryID", reg.ID, "registryURL", reg.URL, "error", err.Error())
					return containerregistry.Sync{}, false, nil
				}

				syncItem.AWSAccessKeyID = reg.AWSAccessKeyID
				syncItem.AWSSecretAccessKey = decryptedSecret
				syncItem.AWSRegion = reg.AWSRegion
			} else {
				decryptedToken, err := crypto.Decrypt(reg.Token)
				if err != nil {
					slog.WarnContext(ctx, "Failed to decrypt registry token for sync", "registryID", reg.ID, "registryURL", reg.URL, "error", err.Error())
					return containerregistry.Sync{}, false, nil
				}

				syncItem.Username = reg.Username
				syncItem.Token = decryptedToken
			}

			return syncItem, true, nil
		},
		func(items []containerregistry.Sync) containerregistry.SyncRequest {
			return containerregistry.SyncRequest{Registries: items}
		},
	)
}

// SyncS3DestinationsToEnvironment sends manager-owned destinations to one remote environment.
func (s *EnvironmentService) SyncS3DestinationsToEnvironment(ctx context.Context, environmentID string) error {
	return s.fanOutSyncToEnvironment(ctx, environmentID, "S3 destinations", "/api/backups/s3/sync",
		func(_ context.Context, destination s3domain.S3Destination) (backuptypes.S3DestinationSync, bool, error) {
			secret, err := crypto.Decrypt(destination.SecretAccessKey)
			if err != nil {
				return backuptypes.S3DestinationSync{}, false, errors.WrapIff(err, "failed to decrypt S3 destination %s for sync", destination.ID)
			}
			return destination.ToSync(secret), true, nil
		},
		func(items []backuptypes.S3DestinationSync) backuptypes.S3DestinationSyncRequest {
			return backuptypes.S3DestinationSyncRequest{Destinations: items}
		},
	)
}

// SyncRepositoriesToEnvironment syncs all git repositories from this manager to a remote environment
func (s *EnvironmentService) SyncRepositoriesToEnvironment(ctx context.Context, environmentID string) error {
	return s.fanOutSyncToEnvironment(ctx, environmentID, "git repositories", "/api/git-repositories/sync",
		func(ctx context.Context, repo gitrepo.GitRepository) (gitops.RepositorySync, bool, error) {
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

			if repo.Token != "" {
				decryptedToken, err := crypto.Decrypt(repo.Token)
				if err != nil {
					slog.WarnContext(ctx, "Failed to decrypt repository token for sync", "repositoryID", repo.ID, "repositoryName", repo.Name, "error", err.Error())
					return gitops.RepositorySync{}, false, nil
				}
				item.Token = decryptedToken
			}

			if repo.SSHKey != "" {
				decryptedSSHKey, err := crypto.Decrypt(repo.SSHKey)
				if err != nil {
					slog.WarnContext(ctx, "Failed to decrypt repository SSH key for sync", "repositoryID", repo.ID, "repositoryName", repo.Name, "error", err.Error())
					return gitops.RepositorySync{}, false, nil
				}
				item.SSHKey = decryptedSSHKey
			}

			return item, true, nil
		},
		func(items []gitops.RepositorySync) gitops.RepositorySyncRequest {
			return gitops.RepositorySyncRequest{Repositories: items}
		},
	)
}

// fanOutSyncToEnvironment pushes every row of Model held by this
// manager to one remote environment. toSyncItem maps a row to its wire form and
// reports whether to keep it — rows whose credentials fail to decrypt are
// skipped rather than failing the whole batch — and wrap builds the request
// envelope the target endpoint expects.
func (s *EnvironmentService) fanOutSyncToEnvironment[Model any, Item any, Request any](
	ctx context.Context,
	environmentID string,
	kind string,
	path string,
	toSyncItem func(context.Context, Model) (Item, bool, error),
	wrap func([]Item) Request,
) error {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Starting sync to environment", "kind", kind, "environmentID", environmentID, "environmentName", target.Name, "apiUrl", target.TargetURL)

	var records []Model
	if err := s.db.WithContext(ctx).Find(&records).Error; err != nil {
		return errors.WrapIff(err, "failed to get %s", kind)
	}

	syncItems := make([]Item, 0, len(records))
	for _, record := range records {
		item, keep, err := toSyncItem(ctx, record)
		if err != nil {
			return err
		}
		if keep {
			syncItems = append(syncItems, item)
		}
	}

	reqBody, err := json.Marshal(wrap(syncItems))
	if err != nil {
		return errors.WrapIf(err, "failed to marshal sync request")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	slog.InfoContext(ctx, "Sending sync request to agent", "kind", kind, "url", target.TargetURL+path, "count", len(syncItems), "isEdge", target.IsEdge)

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := s.proxyJSONRequestForTargetInternal(reqCtx, target, http.MethodPost, path, reqBody, &result); err != nil {
		return errors.WrapIf(err, "failed to send sync request")
	}

	if !result.Success {
		return errors.Errorf("sync failed: %s", result.Data.Message)
	}

	slog.InfoContext(ctx, "Successfully synced to environment", "kind", kind, "environmentID", environmentID, "environmentName", target.Name)

	return nil
}

// ProxyRequest sends a request to a remote environment's API.
func (s *EnvironmentService) ProxyRequest(ctx context.Context, envID string, method string, path string, body []byte) ([]byte, int, error) {
	proxyCtx, cancel := s.getProxyRequestContextInternal(ctx)
	defer cancel()

	resp, err := s.ExecuteRemoteRequest(proxyCtx, envID, method, path, body)
	if err != nil {
		return nil, 0, err
	}

	return resp.Body, resp.StatusCode, nil
}
