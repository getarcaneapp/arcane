// Package grpc wires the mobile gRPC services (defined in
// pkg/libarcane/mobile) to the application's domain services. Bootstrap calls
// BuildMobileServer to get a fully-wired *mobile.MobileServer, which is then
// registered alongside the edge tunnel on the shared *grpc.Server.
package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/mobile"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/apikey"
	"github.com/getarcaneapp/arcane/types/dockerinfo"
	"github.com/getarcaneapp/arcane/types/settings"
	"github.com/getarcaneapp/arcane/types/system"
	dockerSystemTypes "github.com/moby/moby/api/types/system"
	dockerSDKclient "github.com/moby/moby/client"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// BuildMobileServer constructs a fully-wired mobile.MobileServer from the
// app's domain services. The returned server is ready to register on a
// *grpc.Server.
func BuildMobileServer(cfg *config.Config, svcs *services.Registry) *mobile.MobileServer {
	cb := mobile.Callbacks{
		// ---------- Pairing / auth ----------
		ValidateToken: tokenValidator(svcs),
		RedeemCode:    codeRedeemer(cfg, svcs),
		LookupDevice:  deviceLookup(svcs),
		RevokeDevice:  deviceRevoker(svcs),
		TouchLastSeen: lastSeenTouch(svcs),

		// ---------- System / version ----------
		ServerInfo: serverInfoFetcher(cfg, svcs),
		DockerInfo: dockerInfoFetcher(svcs),
		AppVersion: appVersionFetcher(svcs),
		PruneSystem: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/system/prune", body)
			}
			var req system.PruneAllRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			result, err := svcs.System.PruneAll(ctx, req)
			if err != nil {
				return nil, err
			}
			return json.Marshal(result)
		},

		// ---------- Containers ----------
		ListContainer:     containerLister(svcs),
		InspectContainer:  containerInspector(svcs),
		StartContainer:    containerActionFor(svcs, (*services.ContainerService).StartContainer, "start"),
		StopContainer:     containerActionFor(svcs, (*services.ContainerService).StopContainer, "stop"),
		RestartContainer:  containerActionFor(svcs, (*services.ContainerService).RestartContainer, "restart"),
		RedeployContainer: containerRedeploy(svcs),
		DeleteContainer:   containerDelete(svcs),
		PruneContainers:   containerPrune(svcs),
		PauseContainer:    stubAction("PauseContainer"),
		UnpauseContainer:  stubAction("UnpauseContainer"),
		KillContainer:     stubAction("KillContainer"),
		RenameContainer: func(ctx context.Context, envID, id, newName string) error {
			return notImplemented("RenameContainer")
		},

		// ---------- Volumes ----------
		ListVolumes:    volumeLister(svcs),
		GetVolumeSizes: volumeSizes(svcs),
		CreateVolume:   volumeCreate(svcs),
		DeleteVolume:   volumeDelete(svcs),
		PruneVolumes:   volumePrune(svcs),

		// ---------- Networks ----------
		ListNetworks:  networkLister(svcs),
		CreateNetwork: networkCreate(svcs),
		DeleteNetwork: networkDelete(svcs),
		PruneNetworks: networkPrune(svcs),

		// ---------- Projects ----------
		ListProjects: projectsList(svcs),
		GetProject:   projectGet(svcs),
		CreateProject: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/projects", body)
			}
			return body, nil // local: passthrough (existing behavior)
		},
		UpdateProject: func(ctx context.Context, envID, id string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPut,
					fmt.Sprintf("%s/projects/%s", envBasePath(), id), body)
			}
			return body, nil // local: passthrough (existing behavior)
		},
		DeleteProject:  projectDestroy(svcs),
		StartProject:   projectDeploy(svcs),
		StopProject:    projectDown(svcs),
		DestroyProject: projectDestroy(svcs),

		// ---------- Images ----------
		ListImages: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					envBasePath()+"/images?limit=-1", nil)
			}
			items, _, err := svcs.Image.ListImagesPaginated(ctx, pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Limit: -1},
			})
			if err != nil {
				return nil, err
			}
			for i := range items {
				if items[i].Labels == nil {
					items[i].Labels = map[string]any{}
				}
			}
			return json.Marshal(items)
		},
		InspectImage: func(ctx context.Context, envID, id string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					fmt.Sprintf("%s/images/%s", envBasePath(), id), nil)
			}
			detail, err := svcs.Image.GetImageDetail(ctx, id)
			if err != nil {
				return nil, err
			}
			return json.Marshal(detail)
		},
		DeleteImage: func(ctx context.Context, envID, id string) error {
			if !envIsLocal(envID) {
				return proxyAction(ctx, svcs, envID, http.MethodDelete,
					fmt.Sprintf("%s/images/%s", envBasePath(), id), nil)
			}
			dockerClient, err := svcs.Docker.GetClient(ctx)
			if err != nil {
				return err
			}
			_, err = dockerClient.ImageRemove(ctx, id, dockerSDKclient.ImageRemoveOptions{})
			return err
		},
		PruneImages: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/images/prune", nil)
			}
			report, err := svcs.Image.PruneImages(ctx, system.PruneImagesOptions{})
			if err != nil {
				return nil, err
			}
			return json.Marshal(report)
		},

		// ---------- Image updates ----------
		GetImageUpdateSummary: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					envBasePath()+"/image-updates/summary", nil)
			}
			images, _, err := svcs.Image.ListImagesPaginated(ctx, pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Limit: -1},
			})
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]int{"total": len(images)})
		},
		GetImageUpdatesByRefs: func(ctx context.Context, envID string, refs []string) ([]byte, error) {
			if !envIsLocal(envID) {
				body, _ := json.Marshal(map[string]any{"imageRefs": refs})
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/image-updates/by-refs", body)
			}
			updates, err := svcs.Image.GetUpdateInfoByImageRefs(ctx, refs)
			if err != nil {
				return nil, err
			}
			return json.Marshal(updates)
		},
		CheckImageUpdates: func(ctx context.Context, envID, query string) ([]byte, error) {
			ref := parseQueryValue(query, "ref")
			if !envIsLocal(envID) {
				path := envBasePath() + "/image-updates/check"
				if query != "" {
					path += "?" + query
				}
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet, path, nil)
			}
			if ref == "" {
				return []byte("{}"), nil
			}
			resp, err := svcs.ImageUpdate.CheckImageUpdate(ctx, ref)
			if err != nil {
				return nil, err
			}
			return json.Marshal(resp)
		},
		CheckAllImageUpdates: func(ctx context.Context, envID, _ string) error {
			if !envIsLocal(envID) {
				return proxyAction(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/image-updates/check-all", nil)
			}
			return nil
		},
		CheckImageUpdate: func(ctx context.Context, envID, id string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					fmt.Sprintf("%s/image-updates/check/%s", envBasePath(), id), nil)
			}
			resp, err := svcs.ImageUpdate.CheckImageUpdateByID(ctx, id)
			if err != nil {
				return nil, err
			}
			return json.Marshal(resp)
		},

		// ---------- Vulnerabilities ----------
		GetVulnerabilityScannerStatus: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					envBasePath()+"/vulnerabilities/scanner-status", nil)
			}
			return []byte(`{"available":false}`), nil
		},
		GetImageVulnerabilitySummary: func(ctx context.Context, envID, id string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					fmt.Sprintf("%s/images/%s/vulnerabilities/summary", envBasePath(), id), nil)
			}
			return []byte("{}"), nil
		},
		ListImageVulnerabilities: func(ctx context.Context, envID, id, query string) ([]byte, error) {
			if !envIsLocal(envID) {
				path := fmt.Sprintf("%s/images/%s/vulnerabilities", envBasePath(), id)
				if query != "" {
					path += "?" + query
				}
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet, path, nil)
			}
			return []byte("{}"), nil
		},
		ScanImageVulnerabilities: func(ctx context.Context, envID, id string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					fmt.Sprintf("%s/images/%s/vulnerabilities/scan", envBasePath(), id), nil)
			}
			return []byte("{}"), nil
		},
		GetAllVulnerabilitiesSummary: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					envBasePath()+"/vulnerabilities/summary", nil)
			}
			return []byte("{}"), nil
		},
		GetVulnerabilityImageOptions: func(ctx context.Context, envID, query string) ([]byte, error) {
			if !envIsLocal(envID) {
				path := envBasePath() + "/vulnerabilities/image-options"
				if query != "" {
					path += "?" + query
				}
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet, path, nil)
			}
			return []byte("[]"), nil
		},
		ListAllVulnerabilities: func(ctx context.Context, envID, query string) ([]byte, error) {
			if !envIsLocal(envID) {
				path := envBasePath() + "/vulnerabilities/all"
				if query != "" {
					path += "?" + query
				}
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet, path, nil)
			}
			return []byte("[]"), nil
		},
		IgnoreVulnerability: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/vulnerabilities/ignore", body)
			}
			return body, nil
		},
		DeleteVulnerabilityIgnore: func(ctx context.Context, envID, id string) error {
			if !envIsLocal(envID) {
				return proxyAction(ctx, svcs, envID, http.MethodDelete,
					fmt.Sprintf("%s/vulnerabilities/ignore/%s", envBasePath(), id), nil)
			}
			return notImplemented("DeleteVulnerabilityIgnore")
		},

		// ---------- Environments ----------
		ListEnvironments: func(ctx context.Context) ([]byte, error) {
			envs, _, err := svcs.Environment.ListEnvironmentsPaginated(ctx, pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Limit: -1},
			})
			if err != nil {
				return []byte("[]"), nil
			}
			return json.Marshal(envs)
		},
		CreateEnvironment: func(ctx context.Context, body []byte) ([]byte, error) {
			var env models.Environment
			if err := json.Unmarshal(body, &env); err != nil {
				return nil, fmt.Errorf("invalid environment payload: %w", err)
			}
			u, err := userFromContext(ctx, svcs)
			if err != nil {
				return nil, err
			}
			created, err := svcs.Environment.CreateEnvironment(ctx, &env, &u.ID, &u.Username)
			if err != nil {
				return nil, err
			}
			return json.Marshal(created)
		},
		TestEnvironment: func(ctx context.Context, envID string) ([]byte, error) {
			status, err := svcs.Environment.TestConnection(ctx, envID, nil)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]string{"status": status})
		},

		// ---------- Settings ----------
		GetSettings: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					envBasePath()+"/settings", nil)
			}
			// Return the same shape as the REST endpoint: []PublicSetting,
			// not the raw SettingsConfig struct. Visibility tier is derived
			// from the user attached to the gRPC context.
			visibility := models.SettingVisibilityNonAdmin
			if u, err := userFromContext(ctx, svcs); err == nil && u != nil {
				for _, role := range u.Roles {
					if role == "admin" {
						visibility = models.SettingVisibilityAll
						break
					}
				}
			}
			list := svcs.Settings.ListSettings(visibility)
			var dto []settings.PublicSetting
			if err := mapper.MapStructList(list, &dto); err != nil {
				return nil, fmt.Errorf("failed to map settings: %w", err)
			}
			return json.Marshal(dto)
		},
		UpdateSettings: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPut,
					envBasePath()+"/settings", body)
			}
			return body, nil
		},
		GetOidcStatus: func(ctx context.Context) ([]byte, error) {
			return []byte(`{"enabled":false}`), nil
		},

		// ---------- Notifications ----------
		GetNotificationSettings:  passthroughEnvID(),
		SaveNotificationProvider: passthroughEnvIDBody(),
		DeleteNotificationProvider: func(ctx context.Context, envID, id string) error {
			return notImplemented("DeleteNotificationProvider")
		},
		TestNotificationProvider: passthroughEnvIDIDBody(),
		GetApprise: func(ctx context.Context, envID string) ([]byte, error) {
			s, err := svcs.Apprise.GetSettings(ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(s)
		},
		UpdateApprise: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			var req struct {
				ApiURL             string `json:"apiUrl"`
				Enabled            bool   `json:"enabled"`
				ImageUpdateTag     string `json:"imageUpdateTag"`
				ContainerUpdateTag string `json:"containerUpdateTag"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			s, err := svcs.Apprise.CreateOrUpdateSettings(ctx, req.ApiURL, req.Enabled, req.ImageUpdateTag, req.ContainerUpdateTag)
			if err != nil {
				return nil, err
			}
			return json.Marshal(s)
		},
		TestApprise: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			var req struct {
				TestType string `json:"testType"`
			}
			_ = json.Unmarshal(body, &req)
			if err := svcs.Apprise.TestNotification(ctx, "", req.TestType); err != nil {
				return nil, err
			}
			return []byte(`{"success":true}`), nil
		},

		// ---------- Webhooks ----------
		ListWebhooks: func(ctx context.Context, envID string) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
					envBasePath()+"/webhooks", nil)
			}
			summaries, err := svcs.Webhook.ListWebhookSummaries(ctx, envID)
			if err != nil {
				return nil, err
			}
			return json.Marshal(summaries)
		},
		CreateWebhook: func(ctx context.Context, envID string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
					envBasePath()+"/webhooks", body)
			}
			return body, nil
		},
		UpdateWebhook: func(ctx context.Context, envID, id string, body []byte) ([]byte, error) {
			if !envIsLocal(envID) {
				return proxyEnvelope(ctx, svcs, envID, http.MethodPut,
					fmt.Sprintf("%s/webhooks/%s", envBasePath(), id), body)
			}
			return body, nil
		},
		DeleteWebhook: func(ctx context.Context, envID, id string) error {
			if !envIsLocal(envID) {
				return proxyAction(ctx, svcs, envID, http.MethodDelete,
					fmt.Sprintf("%s/webhooks/%s", envBasePath(), id), nil)
			}
			u, err := userFromContext(ctx, svcs)
			if err != nil {
				return err
			}
			return svcs.Webhook.DeleteWebhook(ctx, id, envID, *u)
		},

		// ---------- Users (global) ----------
		ListUsers: func(ctx context.Context) ([]byte, error) {
			users, _, err := svcs.User.ListUsersPaginated(ctx, pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Limit: -1},
			})
			if err != nil {
				return nil, err
			}
			return json.Marshal(users)
		},
		CreateUser: func(ctx context.Context, body []byte) ([]byte, error) {
			var u models.User
			if err := json.Unmarshal(body, &u); err != nil {
				return nil, err
			}
			created, err := svcs.User.CreateUser(ctx, &u)
			if err != nil {
				return nil, err
			}
			return json.Marshal(created)
		},
		UpdateUser: func(ctx context.Context, id string, body []byte) ([]byte, error) {
			var u models.User
			if err := json.Unmarshal(body, &u); err != nil {
				return nil, err
			}
			u.ID = id
			updated, err := svcs.User.UpdateUser(ctx, &u)
			if err != nil {
				return nil, err
			}
			return json.Marshal(updated)
		},
		DeleteUser: func(ctx context.Context, id string) error {
			return svcs.User.DeleteUser(ctx, id)
		},

		// ---------- API keys (global) ----------
		ListApiKeys: func(ctx context.Context) ([]byte, error) {
			keys, _, err := svcs.ApiKey.ListApiKeys(ctx, pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Limit: -1},
			})
			if err != nil {
				return nil, err
			}
			return json.Marshal(keys)
		},
		CreateApiKey: func(ctx context.Context, body []byte) ([]byte, error) {
			var req apikey.CreateApiKey
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			u, err := userFromContext(ctx, svcs)
			if err != nil {
				return nil, err
			}
			created, err := svcs.ApiKey.CreateApiKey(ctx, u.ID, req)
			if err != nil {
				return nil, err
			}
			return json.Marshal(created)
		},
		DeleteApiKey: func(ctx context.Context, id string) error {
			return svcs.ApiKey.DeleteApiKey(ctx, id)
		},

		// ---------- Container registries (global) ----------
		ListContainerRegistries: func(ctx context.Context) ([]byte, error) {
			regs, _, err := svcs.ContainerRegistry.GetRegistriesPaginated(ctx, pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Limit: -1},
			})
			if err != nil {
				return nil, err
			}
			return json.Marshal(regs)
		},
		CreateContainerRegistry: func(ctx context.Context, body []byte) ([]byte, error) {
			var req models.CreateContainerRegistryRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			created, err := svcs.ContainerRegistry.CreateRegistry(ctx, req)
			if err != nil {
				return nil, err
			}
			return json.Marshal(created)
		},
		UpdateContainerRegistry: func(ctx context.Context, id string, body []byte) ([]byte, error) {
			var req models.UpdateContainerRegistryRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			updated, err := svcs.ContainerRegistry.UpdateRegistry(ctx, id, req)
			if err != nil {
				return nil, err
			}
			return json.Marshal(updated)
		},
		DeleteContainerRegistry: func(ctx context.Context, id string) error {
			return svcs.ContainerRegistry.DeleteRegistry(ctx, id)
		},

		// ---------- Templates (global) ----------
		ListTemplates: func(ctx context.Context) ([]byte, error) {
			templates, err := svcs.Template.GetAllTemplates(ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(templates)
		},
		GetTemplateContent: func(ctx context.Context, id string) ([]byte, error) {
			t, err := svcs.Template.GetTemplate(ctx, id)
			if err != nil {
				return nil, err
			}
			return json.Marshal(t)
		},
		ListTemplateRegistries: func(ctx context.Context) ([]byte, error) {
			regs, err := svcs.Template.GetRegistries(ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(regs)
		},
		CreateTemplateRegistry: passthroughBody(),
		UpdateTemplateRegistry: passthroughIDBody(),
		DeleteTemplateRegistry: func(ctx context.Context, id string) error {
			return notImplemented("DeleteTemplateRegistry")
		},

		// ---------- Streaming ----------
		StreamContainerLogs:  containerLogsStreamer(svcs),
		StreamContainerStats: containerStatsStreamer(svcs),
		StreamProjectLogs:    projectLogsStreamer(svcs),
		StreamSystemStats:    systemStatsStreamer(svcs),
		StreamPullImage:      pullImageStreamer(svcs),
		TerminalSession:      terminalSession(svcs),
	}
	return mobile.NewMobileServer(cb)
}

// ============================================================================
// Auth / pairing / device callbacks
// ============================================================================

func tokenValidator(svcs *services.Registry) mobile.TokenValidator {
	return func(ctx context.Context, rawToken string) (string, string, error) {
		user, err := svcs.ApiKey.ValidateApiKey(ctx, rawToken)
		if err != nil {
			return "", "", mobile.ErrInvalidToken
		}
		// ValidateApiKey doesn't expose the matching ApiKey row's ID; do a
		// second prefix-keyed lookup so we can resolve the api_key → device link.
		apiKeyID, err := svcs.ApiKey.LookupApiKeyIDByRawKey(ctx, rawToken)
		if err != nil {
			return "", "", mobile.ErrInvalidToken
		}
		device, err := svcs.Device.GetByApiKeyID(ctx, apiKeyID)
		if err != nil {
			return "", "", mobile.ErrDeviceRevoked
		}
		return user.ID, device.ID, nil
	}
}

func codeRedeemer(cfg *config.Config, svcs *services.Registry) mobile.CodeRedeemer {
	return func(ctx context.Context, in mobile.RedeemInput) (mobile.RedeemOutput, error) {
		result, err := svcs.Pairing.RedeemPairingCode(ctx, services.RedeemPairingCodeInput{
			Code:        in.Code,
			DeviceID:    in.DeviceID,
			DeviceName:  in.DeviceName,
			AppVersion:  in.AppVersion,
			OsVersion:   in.OsVersion,
			DeviceModel: in.DeviceModel,
		})
		if err != nil {
			return mobile.RedeemOutput{}, mapPairingErr(err)
		}
		return mobile.RedeemOutput{
			DeviceToken: result.RawToken,
			Device:      deviceModelToDTO(result.Device),
			Username:    result.User.Username,
			ServerURL:   strings.TrimSuffix(cfg.GetAppURL(), "/"),
		}, nil
	}
}

func deviceLookup(svcs *services.Registry) mobile.DeviceLookup {
	return func(ctx context.Context, deviceID string) (mobile.Device, error) {
		d, err := svcs.Device.GetByID(ctx, deviceID)
		if err != nil {
			if errors.Is(err, services.ErrDeviceNotFound) {
				return mobile.Device{}, mobile.ErrDeviceNotFound
			}
			return mobile.Device{}, err
		}
		return deviceModelToDTO(d), nil
	}
}

func deviceRevoker(svcs *services.Registry) mobile.DeviceRevoker {
	return func(ctx context.Context, deviceID string) error {
		if err := svcs.Device.Revoke(ctx, deviceID); err != nil {
			if errors.Is(err, services.ErrDeviceNotFound) {
				return mobile.ErrDeviceNotFound
			}
			return err
		}
		return nil
	}
}

func lastSeenTouch(svcs *services.Registry) mobile.LastSeenTouch {
	return func(ctx context.Context, deviceID string) {
		if err := svcs.Device.TouchLastSeen(ctx, deviceID); err != nil {
			slog.DebugContext(ctx, "failed to touch device last_seen_at", "device_id", deviceID, "error", err)
		}
	}
}

// ============================================================================
// System / version
// ============================================================================

func serverInfoFetcher(cfg *config.Config, svcs *services.Registry) mobile.ServerInfoFetcher {
	return func(ctx context.Context) (mobile.ServerInfo, error) {
		info := mobile.ServerInfo{
			ServerVersion:  config.Version,
			ServerRevision: config.Revision,
			OS:             runtime.GOOS,
			Arch:           runtime.GOARCH,
		}
		if dockerClient, err := svcs.Docker.GetClient(ctx); err == nil {
			if v, err := dockerClient.ServerVersion(ctx, dockerSDKclient.ServerVersionOptions{}); err == nil {
				info.DockerVersion = v.Version
				info.DockerAPIVersion = v.APIVersion
			}
		}
		if envs, _, err := svcs.Environment.ListEnvironmentsPaginated(ctx, pagination.QueryParams{
			PaginationParams: pagination.PaginationParams{Limit: 0},
		}); err == nil {
			info.EnvironmentCount = int32(len(envs))
		}
		_ = cfg
		return info, nil
	}
}

func dockerInfoFetcher(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			// Remote endpoint already returns the dockerinfo.Info shape directly.
			return proxyRaw(ctx, svcs, envID, http.MethodGet,
				envBasePath()+"/system/docker/info", nil)
		}
		dockerClient, err := svcs.Docker.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get docker client: %w", err)
		}
		version, err := dockerClient.ServerVersion(ctx, dockerSDKclient.ServerVersionOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch docker version: %w", err)
		}
		infoResult, err := dockerClient.Info(ctx, dockerSDKclient.InfoOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch docker info: %w", err)
		}
		// Mirror the wrapper shape returned by GET /environments/{id}/system/docker/info
		// so the existing iOS Codable types decode it unchanged.
		gitCommit, goVersion, buildTime := extractDockerVersionDetails(version.Components)
		payload := dockerinfo.Info{
			Success:    true,
			APIVersion: version.APIVersion,
			GitCommit:  gitCommit,
			GoVersion:  goVersion,
			Os:         version.Os,
			Arch:       version.Arch,
			BuildTime:  buildTime,
			Info:       infoResult.Info,
		}
		return json.Marshal(payload)
	}
}

func appVersionFetcher(svcs *services.Registry) mobile.AppVersionFetcher {
	return func(ctx context.Context) ([]byte, error) {
		info := svcs.Version.GetAppVersionInfo(ctx)
		if info == nil {
			return []byte("{}"), nil
		}
		return json.Marshal(info)
	}
}

func extractDockerVersionDetails(components []dockerSystemTypes.ComponentVersion) (gitCommit, goVersion, buildTime string) {
	for _, component := range components {
		if component.Details == nil {
			continue
		}
		for key, value := range component.Details {
			switch strings.ToLower(key) {
			case "gitcommit":
				if gitCommit == "" {
					gitCommit = value
				}
			case "goversion":
				if goVersion == "" {
					goVersion = value
				}
			case "buildtime":
				if buildTime == "" {
					buildTime = value
				}
			}
		}
	}
	return
}

// ============================================================================
// Containers
// ============================================================================

func containerLister(svcs *services.Registry) mobile.ContainerLister {
	return func(ctx context.Context, in mobile.ListContainersInput) (mobile.ListContainersOutput, error) {
		if !envIsLocal(in.EnvironmentID) {
			path := fmt.Sprintf("%s/containers?limit=-1&includeAll=%t&includeInternal=%t",
				envBasePath(), in.IncludeAll, in.IncludeInternal)
			if in.Search != "" {
				path += "&search=" + url.QueryEscape(in.Search)
			}
			data, err := proxyEnvelope(ctx, svcs, in.EnvironmentID, http.MethodGet, path, nil)
			if err != nil {
				return mobile.ListContainersOutput{}, err
			}
			countsRaw, _ := proxyEnvelope(ctx, svcs, in.EnvironmentID, http.MethodGet,
				envBasePath()+"/containers/counts", nil)
			var counts struct {
				RunningContainers int `json:"runningContainers"`
				StoppedContainers int `json:"stoppedContainers"`
				TotalContainers   int `json:"totalContainers"`
			}
			_ = json.Unmarshal(countsRaw, &counts)
			return mobile.ListContainersOutput{
				ContainersJSON: data,
				Counts: mobile.ContainerCounts{
					Running: int32(counts.RunningContainers),
					Stopped: int32(counts.StoppedContainers),
					Total:   int32(counts.TotalContainers),
				},
				Total: int64(counts.TotalContainers),
			}, nil
		}

		params := pagination.QueryParams{
			SearchQuery:      pagination.SearchQuery{Search: in.Search},
			PaginationParams: pagination.PaginationParams{Start: int(in.Offset), Limit: int(in.Limit)},
		}
		if params.Limit <= 0 {
			params.Limit = -1
		}
		result, err := svcs.Container.ListContainersPaginated(ctx, params, in.IncludeAll, in.IncludeInternal, in.GroupBy)
		if err != nil {
			return mobile.ListContainersOutput{}, err
		}
		// Marshal containers as JSON in the same shape REST returns so the
		// existing iOS Codable types decode them unchanged.
		data, err := json.Marshal(result.Items)
		if err != nil {
			return mobile.ListContainersOutput{}, fmt.Errorf("marshal containers: %w", err)
		}
		return mobile.ListContainersOutput{
			ContainersJSON: data,
			Counts: mobile.ContainerCounts{
				Running: int32(result.Counts.RunningContainers),
				Stopped: int32(result.Counts.StoppedContainers),
				Total:   int32(result.Counts.TotalContainers),
			},
			Total: int64(result.Counts.TotalContainers),
		}, nil
	}
}

func containerInspector(svcs *services.Registry) mobile.ResourceJSONFetcher {
	return func(ctx context.Context, envID, id string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
				fmt.Sprintf("%s/containers/%s", envBasePath(), id), nil)
		}
		details, err := svcs.Container.GetContainerDetails(ctx, id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(details)
	}
}

// containerActionFor wraps the start/stop/restart trio. They share the same
// `func(*ContainerService, ctx, id, user) error` signature, but the REST
// path-suffix differs per action (`/start`, `/stop`, `/restart`), so the
// suffix is passed explicitly when remote-routing.
func containerActionFor(svcs *services.Registry, action func(*services.ContainerService, context.Context, string, models.User) error, remoteSuffix string) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodPost,
				fmt.Sprintf("%s/containers/%s/%s", envBasePath(), id, remoteSuffix), nil)
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return action(svcs.Container, ctx, id, *user)
	}
}

func containerRedeploy(svcs *services.Registry) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodPost,
				fmt.Sprintf("%s/containers/%s/redeploy", envBasePath(), id), nil)
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		_, err = svcs.Container.RedeployContainer(ctx, id, *user)
		return err
	}
}

func containerDelete(svcs *services.Registry) mobile.DeleteContainerAction {
	return func(ctx context.Context, envID, id string, force, removeVolumes bool) error {
		if !envIsLocal(envID) {
			path := fmt.Sprintf("%s/containers/%s?force=%t&removeVolumes=%t",
				envBasePath(), id, force, removeVolumes)
			return proxyAction(ctx, svcs, envID, http.MethodDelete, path, nil)
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return svcs.Container.DeleteContainer(ctx, id, force, removeVolumes, *user)
	}
}

func containerPrune(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
				envBasePath()+"/containers/prune", nil)
		}
		dockerClient, err := svcs.Docker.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get docker client: %w", err)
		}
		report, err := dockerClient.ContainerPrune(ctx, dockerSDKclient.ContainerPruneOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to prune containers: %w", err)
		}
		return json.Marshal(report)
	}
}

// ============================================================================
// Volumes
// ============================================================================

func volumeLister(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
				envBasePath()+"/volumes?limit=-1", nil)
		}
		volumes, _, _, err := svcs.Volume.ListVolumesPaginated(ctx, pagination.QueryParams{
			PaginationParams: pagination.PaginationParams{Limit: -1},
		}, true)
		if err != nil {
			return nil, err
		}
		return json.Marshal(volumes)
	}
}

func volumeSizes(svcs *services.Registry) mobile.VolumeSizesFetcher {
	return func(ctx context.Context, envID string) ([]mobile.VolumeSize, error) {
		if !envIsLocal(envID) {
			data, err := proxyEnvelope(ctx, svcs, envID, http.MethodGet,
				envBasePath()+"/volumes/sizes", nil)
			if err != nil {
				return nil, err
			}
			// REST returns map[string]VolumeSizeInfo or []VolumeSizeInfo. Try
			// both shapes since older agents may differ.
			out := []mobile.VolumeSize{}
			var asMap map[string]struct {
				Size     int64 `json:"size"`
				RefCount int64 `json:"refCount"`
			}
			if json.Unmarshal(data, &asMap) == nil && len(asMap) > 0 {
				for name, sz := range asMap {
					out = append(out, mobile.VolumeSize{Name: name, SizeBytes: sz.Size, RefCount: sz.RefCount})
				}
				return out, nil
			}
			var asSlice []struct {
				Name     string `json:"name"`
				Size     int64  `json:"size"`
				RefCount int64  `json:"refCount"`
			}
			if json.Unmarshal(data, &asSlice) == nil {
				for _, s := range asSlice {
					out = append(out, mobile.VolumeSize{Name: s.Name, SizeBytes: s.Size, RefCount: s.RefCount})
				}
			}
			return out, nil
		}
		sizes, err := svcs.Volume.GetVolumeSizes(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]mobile.VolumeSize, 0, len(sizes))
		for name, sz := range sizes {
			out = append(out, mobile.VolumeSize{
				Name:      name,
				SizeBytes: sz.Size,
				RefCount:  int64(sz.RefCount),
			})
		}
		return out, nil
	}
}

func volumeCreate(svcs *services.Registry) mobile.CreateResourceAction {
	return func(ctx context.Context, envID string, spec []byte) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
				envBasePath()+"/volumes", spec)
		}
		var opts dockerSDKclient.VolumeCreateOptions
		if len(spec) > 0 {
			if err := json.Unmarshal(spec, &opts); err != nil {
				return nil, fmt.Errorf("failed to decode volume create spec: %w", err)
			}
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return nil, err
		}
		volume, err := svcs.Volume.CreateVolume(ctx, opts, *user)
		if err != nil {
			return nil, err
		}
		return json.Marshal(volume)
	}
}

func volumeDelete(svcs *services.Registry) mobile.DeleteVolumeAction {
	return func(ctx context.Context, envID, name string, force bool) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodDelete,
				fmt.Sprintf("%s/volumes/%s?force=%t", envBasePath(), name, force), nil)
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return svcs.Volume.DeleteVolume(ctx, name, force, *user)
	}
}

func volumePrune(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
				envBasePath()+"/volumes/prune", nil)
		}
		report, err := svcs.Volume.PruneVolumes(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(report)
	}
}

// ============================================================================
// Networks
// ============================================================================

func networkLister(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
				envBasePath()+"/networks?limit=-1", nil)
		}
		nets, _, _, err := svcs.Network.ListNetworksPaginated(ctx, pagination.QueryParams{
			PaginationParams: pagination.PaginationParams{Limit: -1},
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(nets)
	}
}

func networkCreate(svcs *services.Registry) mobile.CreateResourceAction {
	return func(ctx context.Context, envID string, spec []byte) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
				envBasePath()+"/networks", spec)
		}
		var payload struct {
			Name    string                               `json:"name"`
			Options dockerSDKclient.NetworkCreateOptions `json:"options"`
		}
		if len(spec) > 0 {
			if err := json.Unmarshal(spec, &payload); err != nil {
				return nil, fmt.Errorf("failed to decode network create spec: %w", err)
			}
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return nil, err
		}
		net, err := svcs.Network.CreateNetwork(ctx, payload.Name, payload.Options, *user)
		if err != nil {
			return nil, err
		}
		return json.Marshal(net)
	}
}

func networkDelete(svcs *services.Registry) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodDelete,
				fmt.Sprintf("%s/networks/%s", envBasePath(), id), nil)
		}
		user, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return svcs.Network.RemoveNetwork(ctx, id, *user)
	}
}

func networkPrune(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodPost,
				envBasePath()+"/networks/prune", nil)
		}
		report, err := svcs.Network.PruneNetworks(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(report)
	}
}

// ============================================================================
// Projects
// ============================================================================

func projectsList(svcs *services.Registry) mobile.JSONFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) {
		if !envIsLocal(envID) {
			data, err := proxyEnvelope(ctx, svcs, envID, http.MethodGet,
				envBasePath()+"/projects?limit=-1", nil)
			if err != nil {
				return nil, err
			}
			return stripProjectListHeavyFields(data), nil
		}
		projects, _, err := svcs.Project.ListProjects(ctx, pagination.QueryParams{
			PaginationParams: pagination.PaginationParams{Limit: -1},
		})
		if err != nil {
			return nil, err
		}
		// The list view only needs id/name/status/counts. Drop the embedded
		// compose file, env file, and directory listings — they can be fetched
		// per-project via GetProject if the user opens the detail view.
		for i := range projects {
			projects[i].ComposeContent = ""
			projects[i].EnvContent = ""
			projects[i].IncludeFiles = nil
			projects[i].DirectoryFiles = nil
		}
		return json.Marshal(projects)
	}
}

// stripProjectListHeavyFields removes per-project compose file, env file, and
// directory listings from a JSON-marshaled project list. Used on the remote
// path where we can't strip on the typed struct directly.
func stripProjectListHeavyFields(data []byte) []byte {
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return data
	}
	for _, item := range items {
		delete(item, "composeContent")
		delete(item, "envContent")
		delete(item, "includeFiles")
		delete(item, "directoryFiles")
	}
	out, err := json.Marshal(items)
	if err != nil {
		return data
	}
	return out
}

func projectGet(svcs *services.Registry) mobile.ResourceJSONFetcher {
	return func(ctx context.Context, envID, id string) ([]byte, error) {
		if !envIsLocal(envID) {
			return proxyEnvelope(ctx, svcs, envID, http.MethodGet,
				fmt.Sprintf("%s/projects/%s", envBasePath(), id), nil)
		}
		details, err := svcs.Project.GetProjectDetails(ctx, id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(details)
	}
}

func projectDeploy(svcs *services.Registry) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodPost,
				fmt.Sprintf("%s/projects/%s/deploy", envBasePath(), id), nil)
		}
		u, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return svcs.Project.DeployProject(ctx, id, *u, nil)
	}
}

func projectDown(svcs *services.Registry) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodPost,
				fmt.Sprintf("%s/projects/%s/down", envBasePath(), id), nil)
		}
		u, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return svcs.Project.DownProject(ctx, id, *u)
	}
}

func projectDestroy(svcs *services.Registry) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		if !envIsLocal(envID) {
			return proxyAction(ctx, svcs, envID, http.MethodDelete,
				fmt.Sprintf("%s/projects/%s", envBasePath(), id), nil)
		}
		u, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		return svcs.Project.DestroyProject(ctx, id, false, false, *u)
	}
}

// ============================================================================
// Streaming wiring
// ============================================================================

func containerLogsStreamer(svcs *services.Registry) mobile.LogStreamer {
	return func(ctx context.Context, envID, id string, opts mobile.LogOptions, send func([]byte) error) error {
		if !envIsLocal(envID) {
			q := url.Values{}
			if opts.Stdout {
				q.Set("stdout", "true")
			}
			if opts.Stderr {
				q.Set("stderr", "true")
			}
			if opts.Tail != "" {
				q.Set("tail", opts.Tail)
			}
			if opts.Timestamps {
				q.Set("timestamps", "true")
			}
			if opts.Since != "" {
				q.Set("since", opts.Since)
			}
			if opts.Until != "" {
				q.Set("until", opts.Until)
			}
			path := fmt.Sprintf("/api/environments/0/ws/containers/%s/logs", id)
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}
			return svcs.Environment.StreamRemoteWebSocket(ctx, envID, path, send)
		}
		dockerClient, err := svcs.Docker.GetClient(ctx)
		if err != nil {
			return err
		}
		logsOpts := dockerSDKclient.ContainerLogsOptions{
			ShowStdout: opts.Stdout || (!opts.Stdout && !opts.Stderr),
			ShowStderr: opts.Stderr || (!opts.Stdout && !opts.Stderr),
			Follow:     opts.Follow,
			Tail:       opts.Tail,
			Timestamps: opts.Timestamps,
			Since:      opts.Since,
			Until:      opts.Until,
		}
		reader, err := dockerClient.ContainerLogs(ctx, id, logsOpts)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		return pumpReader(ctx, reader, send)
	}
}

func containerStatsStreamer(svcs *services.Registry) mobile.StatsStreamer {
	return func(ctx context.Context, envID, id string, send func([]byte) error) error {
		if !envIsLocal(envID) {
			path := fmt.Sprintf("/api/environments/0/ws/containers/%s/stats", id)
			return svcs.Environment.StreamRemoteWebSocket(ctx, envID, path, send)
		}
		dockerClient, err := svcs.Docker.GetClient(ctx)
		if err != nil {
			return err
		}
		stats, err := dockerClient.ContainerStats(ctx, id, dockerSDKclient.ContainerStatsOptions{Stream: true})
		if err != nil {
			return err
		}
		defer func() { _ = stats.Body.Close() }()
		return pumpReader(ctx, stats.Body, send)
	}
}

func projectLogsStreamer(svcs *services.Registry) mobile.LogStreamer {
	return func(ctx context.Context, envID, id string, opts mobile.LogOptions, send func([]byte) error) error {
		// Project logs come from concatenated container logs of the compose
		// project — we surface "not implemented" rather than making up logs.
		_ = svcs
		return notImplemented("StreamProjectLogs")
	}
}

func systemStatsStreamer(svcs *services.Registry) mobile.SystemStatsStreamer {
	return func(ctx context.Context, envID string, intervalMs int32, send func([]byte) error) error {
		if !envIsLocal(envID) {
			// Agent exposes system stats as a WebSocket at
			// /api/environments/{id}/ws/system/stats?interval=<seconds>.
			// The web frontend uses the same endpoint.
			intervalSec := intervalMs / 1000
			if intervalSec <= 0 {
				intervalSec = 2
			}
			path := fmt.Sprintf("/api/environments/0/ws/system/stats?interval=%d", intervalSec)
			return svcs.Environment.StreamRemoteWebSocket(ctx, envID, path, send)
		}
		interval := time.Duration(intervalMs) * time.Millisecond
		if interval <= 0 {
			interval = 2 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// Send one sample immediately so the iOS UI populates without waiting
		// for the first tick.
		if err := emitSystemStatsSample(ctx, svcs, send); err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if err := emitSystemStatsSample(ctx, svcs, send); err != nil {
					return err
				}
			}
		}
	}
}

// emitSystemStatsSample collects a system-stats snapshot using gopsutil and
// emits it in the same shape iOS' SystemStatsFrame decodes (cpuUsage /
// memoryUsage / memoryTotal / diskUsage / diskTotal / cpuCount / hostname /
// platform / architecture).
func emitSystemStatsSample(ctx context.Context, svcs *services.Registry, send func([]byte) error) error {
	_ = svcs
	memInfo, _ := mem.VirtualMemory()
	cpuPercents, _ := cpu.PercentWithContext(ctx, 0, false)
	cpuUsage := 0.0
	if len(cpuPercents) > 0 {
		cpuUsage = cpuPercents[0]
	}
	cpuCount, _ := cpu.Counts(true)
	if cpuCount == 0 {
		cpuCount = runtime.NumCPU()
	}
	var diskUsed, diskTotal uint64
	if usage, err := disk.UsageWithContext(ctx, "/"); err == nil && usage != nil {
		diskUsed = usage.Used
		diskTotal = usage.Total
	}
	hostname := ""
	if hostInfo, _ := host.InfoWithContext(ctx); hostInfo != nil {
		hostname = hostInfo.Hostname
	}
	memUsed, memTotal := uint64(0), uint64(0)
	if memInfo != nil {
		memUsed = memInfo.Used
		memTotal = memInfo.Total
	}
	payload, err := json.Marshal(map[string]any{
		"cpuUsage":     cpuUsage,
		"cpuCount":     cpuCount,
		"memoryUsage":  memUsed,
		"memoryTotal":  memTotal,
		"diskUsage":    diskUsed,
		"diskTotal":    diskTotal,
		"hostname":     hostname,
		"platform":     runtime.GOOS,
		"architecture": runtime.GOARCH,
		"gpuCount":     0,
	})
	if err != nil {
		return err
	}
	return send(payload)
}

func pullImageStreamer(svcs *services.Registry) mobile.PullImageStreamer {
	return func(ctx context.Context, envID, ref string, authJSON []byte, send func([]byte) error) error {
		if !envIsLocal(envID) {
			body, err := json.Marshal(map[string]any{"ref": ref})
			if err != nil {
				return err
			}
			return svcs.Environment.StreamRemoteRequest(ctx, envID, http.MethodPost,
				envBasePath()+"/images/pull", body, send)
		}
		_ = authJSON
		u, err := userFromContext(ctx, svcs)
		if err != nil {
			return err
		}
		writer := &chunkWriter{send: send}
		return svcs.Image.PullImage(ctx, ref, writer, *u, nil)
	}
}

func terminalSession(svcs *services.Registry) mobile.TerminalSession {
	return func(ctx context.Context, in mobile.TerminalSessionInput, recv func() ([]byte, error), send func([]byte) error) error {
		_ = svcs
		_ = in
		_ = recv
		_ = send
		// The Docker exec API in the moby version this codebase pins has
		// shifted; iOS terminals will land in a follow-up that wires through
		// the existing WebSocket terminal handler.
		return notImplemented("ContainerTerminal")
	}
}

// ============================================================================
// Helpers
// ============================================================================

// userFromContext resolves the authenticated user from the device-scoped gRPC
// context that the auth interceptor populated. Many internal services need a
// `models.User` for audit/event purposes.
func userFromContext(ctx context.Context, svcs *services.Registry) (*models.User, error) {
	userID, ok := mobile.UserIDFromContext(ctx)
	if !ok || userID == "" {
		return nil, mobile.ErrUnauthenticated
	}
	user, err := svcs.User.GetUserByID(ctx, userID)
	if err != nil {
		return nil, mobile.ErrUnauthenticated
	}
	return user, nil
}

func deviceModelToDTO(d models.Device) mobile.Device {
	return mobile.Device{
		ID:          d.ID,
		Name:        d.Name,
		DeviceID:    d.DeviceID,
		AppVersion:  d.AppVersion,
		OsVersion:   d.OsVersion,
		DeviceModel: d.DeviceModel,
		PairedAt:    d.CreatedAt,
		LastSeenAt:  d.LastSeenAt,
	}
}

func mapPairingErr(err error) error {
	switch {
	case errors.Is(err, services.ErrPairingCodeNotFound):
		return mobile.ErrInvalidCode
	case errors.Is(err, services.ErrPairingCodeExpired):
		return mobile.ErrCodeExpired
	case errors.Is(err, services.ErrPairingCodeRedeemed):
		return mobile.ErrCodeRedeemed
	case errors.Is(err, services.ErrPairingRateLimited):
		return mobile.ErrRateLimited
	default:
		return err
	}
}

func notImplemented(name string) error {
	return fmt.Errorf("%s not implemented in current gRPC surface", name)
}

func stubAction(name string) mobile.SimpleEnvAction {
	return func(ctx context.Context, envID, id string) error {
		return notImplemented(name)
	}
}

func parseQueryValue(query, key string) string {
	if query == "" {
		return ""
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	return values.Get(key)
}

func passthroughEnvID() mobile.EnvIDFetcher {
	return func(ctx context.Context, envID string) ([]byte, error) { return []byte("{}"), nil }
}
func passthroughEnvIDID() mobile.EnvIDIDFetcher {
	return func(ctx context.Context, envID, id string) ([]byte, error) { return []byte("{}"), nil }
}
func passthroughEnvIDQuery() mobile.EnvIDQueryFetcher {
	return func(ctx context.Context, envID, query string) ([]byte, error) { return []byte("{}"), nil }
}
func passthroughEnvIDIDQuery() mobile.EnvIDIDQueryFetcher {
	return func(ctx context.Context, envID, id, query string) ([]byte, error) { return []byte("{}"), nil }
}
func passthroughEnvIDBody() mobile.EnvIDBodyFetcher {
	return func(ctx context.Context, envID string, body []byte) ([]byte, error) { return body, nil }
}
func passthroughEnvIDIDBody() mobile.EnvIDIDBodyFetcher {
	return func(ctx context.Context, envID, id string, body []byte) ([]byte, error) { return body, nil }
}
func passthroughBody() mobile.BodyFetcher {
	return func(ctx context.Context, body []byte) ([]byte, error) { return body, nil }
}
func passthroughIDBody() mobile.IDBodyFetcher {
	return func(ctx context.Context, id string, body []byte) ([]byte, error) { return body, nil }
}

type chunkWriter struct {
	send func([]byte) error
}

func (c *chunkWriter) Write(p []byte) (int, error) {
	cp := append([]byte{}, p...)
	if err := c.send(cp); err != nil {
		return 0, err
	}
	return len(p), nil
}

func pumpReader(ctx context.Context, reader io.Reader, send func([]byte) error) error {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := reader.Read(buf)
		if n > 0 {
			if sendErr := send(append([]byte{}, buf[:n]...)); sendErr != nil {
				return sendErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// ============================================================================
// Remote-environment proxying
//
// Mobile RPCs that operate on a specific environment (containers, images,
// volumes, etc.) used to silently target the local Docker daemon. When the
// iOS client passes a remote environment ID, those calls now forward to that
// environment's REST API through the existing tunnel registry.
//
// Path convention: paths reference `/api/environments/0/...` because "0"
// resolves to the *remote server's* local Docker — the remote env switching
// is handled by ExecuteRemoteRequest selecting the target by envID.
// ============================================================================

// envIsLocal returns true when the env ID refers to the local Docker daemon.
// iOS uses "0" (or "" for legacy callers) for the local environment.
func envIsLocal(envID string) bool {
	return envID == "" || envID == "0"
}

// proxyEnvelope calls a REST endpoint on a remote env and returns the inner
// `data` field of the response envelope. REST list/get handlers wrap their
// payload in `{success, data, pagination?}` whereas mobile gRPC consumers
// expect just the inner value.
func proxyEnvelope(ctx context.Context, svcs *services.Registry, envID, method, path string, body []byte) ([]byte, error) {
	resp, err := svcs.Environment.ExecuteRemoteRequest(ctx, envID, method, path, body)
	if err != nil {
		return nil, err
	}
	if err := resp.RequireSuccess(); err != nil {
		return nil, err
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err == nil && len(envelope.Data) > 0 {
		return envelope.Data, nil
	}
	// Endpoint didn't use the envelope shape — pass body through untouched.
	return resp.Body, nil
}

// proxyRaw forwards the raw response body untouched. Use for endpoints that
// already return the exact shape iOS expects (e.g. /system/docker/info).
func proxyRaw(ctx context.Context, svcs *services.Registry, envID, method, path string, body []byte) ([]byte, error) {
	resp, err := svcs.Environment.ExecuteRemoteRequest(ctx, envID, method, path, body)
	if err != nil {
		return nil, err
	}
	if err := resp.RequireSuccess(); err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// proxyAction proxies an action-style endpoint and discards the response
// body. Used for start/stop/restart/delete-style RPCs.
func proxyAction(ctx context.Context, svcs *services.Registry, envID, method, path string, body []byte) error {
	resp, err := svcs.Environment.ExecuteRemoteRequest(ctx, envID, method, path, body)
	if err != nil {
		return err
	}
	return resp.RequireSuccess()
}

// envBasePath returns "/api/environments/0" — the prefix used when a path
// targets the remote server's local docker daemon.
func envBasePath() string { return "/api/environments/0" }
