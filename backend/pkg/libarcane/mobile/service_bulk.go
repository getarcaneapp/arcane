package mobile

import (
	"context"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
)

// This file groups the (mostly JSON-passthrough) RPC handlers added in
// Tier 2/3. Each handler is a thin wrapper around a Callbacks field —
// auth+env validation lives in the small helpers near the bottom.

// ---------- Container extras ----------

func (s *MobileServer) PauseContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.PauseContainer)
}
func (s *MobileServer) UnpauseContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.UnpauseContainer)
}
func (s *MobileServer) KillContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.KillContainer)
}
func (s *MobileServer) RenameContainer(ctx context.Context, req *mobilepb.RenameContainerRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.RenameContainer == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := s.callbacks.RenameContainer(ctx, req.GetEnvironmentId(), req.GetId(), req.GetNewName()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

// ---------- Images ----------

func (s *MobileServer) ListImages(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.ListImages)
}
func (s *MobileServer) InspectImage(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.JSONResponse, error) {
	return s.envIDJSONOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.InspectImage)
}
func (s *MobileServer) DeleteImage(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.DeleteImage)
}
func (s *MobileServer) PruneImages(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.PruneResult, error) {
	data, err := s.envJSONOutRaw(ctx, req.GetEnvironmentId(), s.callbacks.PruneImages)
	if err != nil {
		return nil, err
	}
	return &mobilepb.PruneResult{ReportJson: data}, nil
}

// ---------- Image updates ----------

func (s *MobileServer) GetImageUpdateSummary(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.GetImageUpdateSummary)
}
func (s *MobileServer) GetImageUpdatesByRefs(ctx context.Context, req *mobilepb.GetImageUpdatesByRefsRequest) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.GetImageUpdatesByRefs == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.GetImageUpdatesByRefs(ctx, req.GetEnvironmentId(), req.GetRefs())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}
func (s *MobileServer) CheckImageUpdates(ctx context.Context, req *mobilepb.EnvIDAndQueryRequest) (*mobilepb.JSONResponse, error) {
	return s.envQueryJSONOut(ctx, req.GetEnvironmentId(), req.GetQuery(), s.callbacks.CheckImageUpdates)
}
func (s *MobileServer) CheckAllImageUpdates(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.CheckAllImageUpdates == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := s.callbacks.CheckAllImageUpdates(ctx, req.GetEnvironmentId(), ""); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}
func (s *MobileServer) CheckImageUpdate(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.JSONResponse, error) {
	return s.envIDJSONOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.CheckImageUpdate)
}

// ---------- Vulnerabilities ----------

func (s *MobileServer) GetVulnerabilityScannerStatus(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.GetVulnerabilityScannerStatus)
}
func (s *MobileServer) GetImageVulnerabilitySummary(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.JSONResponse, error) {
	return s.envIDJSONOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.GetImageVulnerabilitySummary)
}
func (s *MobileServer) ListImageVulnerabilities(ctx context.Context, req *mobilepb.EnvIDAndIDAndQueryRequest) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.ListImageVulnerabilities == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.ListImageVulnerabilities(ctx, req.GetEnvironmentId(), req.GetId(), req.GetQuery())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}
func (s *MobileServer) ScanImageVulnerabilities(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.JSONResponse, error) {
	return s.envIDJSONOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.ScanImageVulnerabilities)
}
func (s *MobileServer) GetAllVulnerabilitiesSummary(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.GetAllVulnerabilitiesSummary)
}
func (s *MobileServer) GetVulnerabilityImageOptions(ctx context.Context, req *mobilepb.EnvIDAndQueryRequest) (*mobilepb.JSONResponse, error) {
	return s.envQueryJSONOut(ctx, req.GetEnvironmentId(), req.GetQuery(), s.callbacks.GetVulnerabilityImageOptions)
}
func (s *MobileServer) ListAllVulnerabilities(ctx context.Context, req *mobilepb.EnvIDAndQueryRequest) (*mobilepb.JSONResponse, error) {
	return s.envQueryJSONOut(ctx, req.GetEnvironmentId(), req.GetQuery(), s.callbacks.ListAllVulnerabilities)
}
func (s *MobileServer) IgnoreVulnerability(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.IgnoreVulnerability)
}
func (s *MobileServer) DeleteVulnerabilityIgnore(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.DeleteVulnerabilityIgnore)
}

// ---------- Projects (mutations) ----------

func (s *MobileServer) CreateProject(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.CreateProject)
}
func (s *MobileServer) UpdateProject(ctx context.Context, req *mobilepb.EnvIDAndIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.UpdateProject == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.UpdateProject(ctx, req.GetEnvironmentId(), req.GetId(), req.GetBody())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}
func (s *MobileServer) DeleteProject(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.DeleteProject)
}
func (s *MobileServer) StartProject(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.StartProject)
}
func (s *MobileServer) StopProject(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.StopProject)
}
func (s *MobileServer) DestroyProject(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.DestroyProject)
}

// ---------- Environments ----------

func (s *MobileServer) ListEnvironments(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.ListEnvironments)
}
func (s *MobileServer) CreateEnvironment(ctx context.Context, req *mobilepb.JSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.bodyJSONOut(ctx, req.GetBody(), s.callbacks.CreateEnvironment)
}
func (s *MobileServer) TestEnvironment(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.TestEnvironment)
}

// ---------- Settings ----------

func (s *MobileServer) GetSettings(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.GetSettings)
}
func (s *MobileServer) UpdateSettings(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.UpdateSettings)
}
func (s *MobileServer) GetOidcStatus(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.GetOidcStatus)
}

// ---------- Notifications ----------

func (s *MobileServer) GetNotificationSettings(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.GetNotificationSettings)
}
func (s *MobileServer) SaveNotificationProvider(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.SaveNotificationProvider)
}
func (s *MobileServer) DeleteNotificationProvider(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.DeleteNotificationProvider)
}
func (s *MobileServer) TestNotificationProvider(ctx context.Context, req *mobilepb.EnvIDAndIDAndJSONBodyRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.TestNotificationProvider == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if _, err := s.callbacks.TestNotificationProvider(ctx, req.GetEnvironmentId(), req.GetId(), req.GetBody()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}
func (s *MobileServer) GetApprise(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.GetApprise)
}
func (s *MobileServer) UpdateApprise(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.UpdateApprise)
}
func (s *MobileServer) TestApprise(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.TestApprise == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if _, err := s.callbacks.TestApprise(ctx, req.GetEnvironmentId(), req.GetBody()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

// ---------- Webhooks ----------

func (s *MobileServer) ListWebhooks(ctx context.Context, req *mobilepb.EnvIDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.envJSONOut(ctx, req.GetEnvironmentId(), s.callbacks.ListWebhooks)
}
func (s *MobileServer) CreateWebhook(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.CreateWebhook)
}
func (s *MobileServer) UpdateWebhook(ctx context.Context, req *mobilepb.EnvIDAndIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.UpdateWebhook == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.UpdateWebhook(ctx, req.GetEnvironmentId(), req.GetId(), req.GetBody())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}
func (s *MobileServer) DeleteWebhook(ctx context.Context, req *mobilepb.EnvIDAndIDRequest) (*mobilepb.ActionResult, error) {
	return s.envIDActionOut(ctx, req.GetEnvironmentId(), req.GetId(), s.callbacks.DeleteWebhook)
}

// ---------- Users (global) ----------

func (s *MobileServer) ListUsers(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.ListUsers)
}
func (s *MobileServer) CreateUser(ctx context.Context, req *mobilepb.JSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.bodyJSONOut(ctx, req.GetBody(), s.callbacks.CreateUser)
}
func (s *MobileServer) UpdateUser(ctx context.Context, req *mobilepb.IDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.idBodyJSONOut(ctx, req.GetId(), req.GetBody(), s.callbacks.UpdateUser)
}
func (s *MobileServer) DeleteUser(ctx context.Context, req *mobilepb.IDOnlyRequest) (*mobilepb.ActionResult, error) {
	return s.idActionOut(ctx, req.GetId(), s.callbacks.DeleteUser)
}

// ---------- API keys (global) ----------

func (s *MobileServer) ListApiKeys(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.ListApiKeys)
}
func (s *MobileServer) CreateApiKey(ctx context.Context, req *mobilepb.JSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.bodyJSONOut(ctx, req.GetBody(), s.callbacks.CreateApiKey)
}
func (s *MobileServer) DeleteApiKey(ctx context.Context, req *mobilepb.IDOnlyRequest) (*mobilepb.ActionResult, error) {
	return s.idActionOut(ctx, req.GetId(), s.callbacks.DeleteApiKey)
}

// ---------- Container registries (global) ----------

func (s *MobileServer) ListContainerRegistries(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.ListContainerRegistries)
}
func (s *MobileServer) CreateContainerRegistry(ctx context.Context, req *mobilepb.JSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.bodyJSONOut(ctx, req.GetBody(), s.callbacks.CreateContainerRegistry)
}
func (s *MobileServer) UpdateContainerRegistry(ctx context.Context, req *mobilepb.IDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.idBodyJSONOut(ctx, req.GetId(), req.GetBody(), s.callbacks.UpdateContainerRegistry)
}
func (s *MobileServer) DeleteContainerRegistry(ctx context.Context, req *mobilepb.IDOnlyRequest) (*mobilepb.ActionResult, error) {
	return s.idActionOut(ctx, req.GetId(), s.callbacks.DeleteContainerRegistry)
}

// ---------- Templates (global) ----------

func (s *MobileServer) ListTemplates(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.ListTemplates)
}
func (s *MobileServer) GetTemplateContent(ctx context.Context, req *mobilepb.IDOnlyRequest) (*mobilepb.JSONResponse, error) {
	return s.idJSONOut(ctx, req.GetId(), s.callbacks.GetTemplateContent)
}
func (s *MobileServer) ListTemplateRegistries(ctx context.Context, _ *mobilepb.EmptyRequest) (*mobilepb.JSONResponse, error) {
	return s.emptyJSONOut(ctx, s.callbacks.ListTemplateRegistries)
}
func (s *MobileServer) CreateTemplateRegistry(ctx context.Context, req *mobilepb.JSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.bodyJSONOut(ctx, req.GetBody(), s.callbacks.CreateTemplateRegistry)
}
func (s *MobileServer) UpdateTemplateRegistry(ctx context.Context, req *mobilepb.IDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.idBodyJSONOut(ctx, req.GetId(), req.GetBody(), s.callbacks.UpdateTemplateRegistry)
}
func (s *MobileServer) DeleteTemplateRegistry(ctx context.Context, req *mobilepb.IDOnlyRequest) (*mobilepb.ActionResult, error) {
	return s.idActionOut(ctx, req.GetId(), s.callbacks.DeleteTemplateRegistry)
}

// ---------- System ----------

func (s *MobileServer) PruneSystem(ctx context.Context, req *mobilepb.EnvIDAndJSONBodyRequest) (*mobilepb.JSONResponse, error) {
	return s.envBodyJSONOut(ctx, req.GetEnvironmentId(), req.GetBody(), s.callbacks.PruneSystem)
}

// ---------- Helpers ----------

func (s *MobileServer) envJSONOut(ctx context.Context, envID string, fn EnvIDFetcher) (*mobilepb.JSONResponse, error) {
	data, err := s.envJSONOutRaw(ctx, envID, fn)
	if err != nil {
		return nil, err
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) envJSONOutRaw(ctx context.Context, envID string, fn EnvIDFetcher) ([]byte, error) {
	if err := s.requireAuthAndLocalEnv(ctx, envID); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, envID)
	if err != nil {
		return nil, statusFromError(err)
	}
	return data, nil
}

func (s *MobileServer) envIDJSONOut(ctx context.Context, envID, id string, fn EnvIDIDFetcher) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, envID); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, envID, id)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) envQueryJSONOut(ctx context.Context, envID, query string, fn EnvIDQueryFetcher) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, envID); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, envID, query)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) envBodyJSONOut(ctx context.Context, envID string, body []byte, fn EnvIDBodyFetcher) (*mobilepb.JSONResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, envID); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, envID, body)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) envIDActionOut(ctx context.Context, envID, id string, fn EnvIDIDAction) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, envID); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := fn(ctx, envID, id); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

func (s *MobileServer) emptyJSONOut(ctx context.Context, fn EmptyFetcher) (*mobilepb.JSONResponse, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) bodyJSONOut(ctx context.Context, body []byte, fn BodyFetcher) (*mobilepb.JSONResponse, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, body)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) idJSONOut(ctx context.Context, id string, fn IDFetcher) (*mobilepb.JSONResponse, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, id)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) idBodyJSONOut(ctx context.Context, id string, body []byte, fn IDBodyFetcher) (*mobilepb.JSONResponse, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, id, body)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.JSONResponse{Payload: data}, nil
}

func (s *MobileServer) idActionOut(ctx context.Context, id string, fn IDAction) (*mobilepb.ActionResult, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := fn(ctx, id); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}
