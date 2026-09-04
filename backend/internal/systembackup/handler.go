package systembackup

import (
	"context"
	"net/http"
	"strings"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/samber/mo"
)

type SystemBackupHandler struct {
	service  *SystemBackupService
	activity *activity.ActivityService
	appCtx   context.Context
}

type ListSystemBackupsInput struct {
	Search string `query:"search"`
	Sort   string `query:"sort" default:"createdAt"`
	Order  string `query:"order" default:"desc"`
	Start  int    `query:"start" default:"0"`
	Limit  int    `query:"limit" default:"20"`
}

type ListBackupHistoryInput struct {
	Search string `query:"search"`
	Sort   string `query:"sort" default:"createdAt"`
	Order  string `query:"order" default:"desc"`
	Start  int    `query:"start" default:"0"`
	Limit  int    `query:"limit" default:"20"`
	Type   string `query:"type"`
}

type SystemVolumeBackupConfigOutput struct {
	Body backuptypes.SystemVolumeBackupPolicyCollection
}

type UpdateSystemVolumeBackupConfigInput struct {
	Body backuptypes.UpdateSystemVolumeBackupPolicies
}

type SystemVolumeBackupOptionsOutput struct {
	Body []backuptypes.SystemVolumeBackupOption
}

type SystemVolumeBackupRunOutput struct {
	Body backuptypes.BackupRunAccepted
}

type RunSystemVolumeBackupsInput struct {
	Body *backuptypes.RunSystemVolumeBackupsRequest `json:"body,omitempty"`
}

type SystemBackupPoliciesOutput struct {
	Body backuptypes.SystemBackupPolicyCollection
}
type UpdateSystemBackupPoliciesInput struct {
	Body backuptypes.UpdateSystemBackupPolicies
}
type SetSystemBackupRecoveryKeyInput struct {
	Body backuptypes.SystemBackupRecoveryKey
}
type SystemBackupRecoveryKeyOutput struct {
	Body backuptypes.SystemBackupRecoveryKeyStatus
}
type GenerateSystemBackupRecoveryKeyOutput struct {
	Body backuptypes.SystemBackupRecoveryKey
}
type CreateSystemBackupInput struct {
	Body backuptypes.CreateSystemBackupRequest
}
type SystemBackupOutput struct{ Body backuptypes.SystemBackupRun }
type RestoreSystemBackupInput struct {
	ID   string `path:"id"`
	Body backuptypes.RestoreSystemBackupRequest
}

// BrowseSystemBackupFilesInput selects one page of a system backup tree.
type BrowseSystemBackupFilesInput struct {
	ID     string `path:"id"`
	Path   string `query:"path" doc:"Folder path relative to the backup root"`
	Search string `query:"search" doc:"Case-insensitive full-path search"`
	Start  int    `query:"start" default:"0" doc:"Start index for the page"`
	Limit  int    `query:"limit" default:"20" doc:"Requested page size"`
	Body   backuptypes.SystemBackupRecoveryKey
}

// RestoreSystemBackupFilesInput selects project files to restore from a system backup.
type RestoreSystemBackupFilesInput struct {
	ID   string `path:"id"`
	Body backuptypes.RestoreSystemBackupFilesRequest
}
type UploadSystemBackupInput struct {
	ID   string `path:"id"`
	Body backuptypes.UploadSystemBackupRequest
}
type DeleteSystemBackupInput struct {
	ID   string `path:"id"`
	Body backuptypes.DeleteSystemBackupRequest
}
type DiscoverSystemBackupsInput struct {
	Body backuptypes.DiscoverSystemBackupsRequest
}

func RegisterSystemBackups(api huma.API, service *SystemBackupService, activityService *activity.ActivityService, appCtx handlerutil.ActivityAppContext) {
	h := &SystemBackupHandler{service: service, activity: activityService, appCtx: appCtx.Context()}
	adminOnly := middleware.RequireGlobalAdmin(api)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-system-backups", Method: http.MethodGet, Path: "/backups", Summary: "List Arcane system backups", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.List)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-backup-history", Method: http.MethodGet, Path: "/backups/history", Summary: "List unified backup history", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.ListHistory)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-system-volume-backup-config", Method: http.MethodGet, Path: "/backups/volumes/config", Summary: "Get system-managed volume backup policies", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.GetSystemVolumeConfig)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-system-volume-backup-config", Method: http.MethodPut, Path: "/backups/volumes/config", Summary: "Update system-managed volume backup policies", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.UpdateSystemVolumeConfig)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-system-volume-backup-options", Method: http.MethodGet, Path: "/backups/volumes/options", Summary: "List volumes available to system-managed backups", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.ListSystemVolumeOptions)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "run-system-volume-backups", DefaultStatus: http.StatusAccepted, Method: http.MethodPost, Path: "/backups/volumes/run", Summary: "Run system-managed volume backups", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.RunSystemVolumeBackups)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-system-backup-policies", Method: http.MethodGet, Path: "/backups/policies", Summary: "Get Arcane system backup policies", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.GetPolicies)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-system-backup-policies", Method: http.MethodPut, Path: "/backups/policies", Summary: "Update Arcane system backup policies", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.UpdatePolicies)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "generate-system-backup-recovery-key", Method: http.MethodPost, Path: "/backups/recovery-key/generate", Summary: "Generate an Arcane system backup recovery key", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRecoveryKey, h.GenerateRecoveryKey)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "set-system-backup-recovery-key", Method: http.MethodPut, Path: "/backups/recovery-key", Summary: "Configure Arcane system backup recovery key", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRecoveryKey, h.SetRecoveryKey)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "create-system-backup", DefaultStatus: http.StatusAccepted, Method: http.MethodPost, Path: "/backups", Summary: "Create Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Create)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "discover-system-backups", Method: http.MethodPost, Path: "/backups/discover", Summary: "Discover Arcane system backups in S3", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Discover)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "restore-system-backup", Method: http.MethodPost, Path: "/backups/{id}/restore", Summary: "Restore Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRestore, h.Restore)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "browse-system-backup-files", Method: http.MethodPost, Path: "/backups/{id}/files/browse", Summary: "Browse project files in an Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.BrowseFiles)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "restore-system-backup-files", Method: http.MethodPost, Path: "/backups/{id}/restore-files", Summary: "Restore project files from an Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRestore, h.RestoreFiles)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "upload-system-backup", Method: http.MethodPost, Path: "/backups/{id}/upload", Summary: "Upload Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Upload)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-system-backup", Method: http.MethodDelete, Path: "/backups/{id}", Summary: "Delete Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Delete)
}

func (h *SystemBackupHandler) ListHistory(ctx context.Context, input *ListBackupHistoryInput) (*handlerutil.Page[backuptypes.HistoryEntry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.Filters = map[string]string{"type": input.Type}
	history, page, err := h.service.ListBackupHistory(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[backuptypes.HistoryEntry]{
		Body: base.Paginated[backuptypes.HistoryEntry]{
			Success:    true,
			Data:       history,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *SystemBackupHandler) GetSystemVolumeConfig(ctx context.Context, _ *struct{}) (*SystemVolumeBackupConfigOutput, error) {
	config, err := h.service.GetSystemVolumeBackupConfig(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemVolumeBackupConfigOutput{Body: *config}, nil
}

func (h *SystemBackupHandler) UpdateSystemVolumeConfig(ctx context.Context, input *UpdateSystemVolumeBackupConfigInput) (*SystemVolumeBackupConfigOutput, error) {
	config, err := h.service.UpdateSystemVolumeBackupConfig(ctx, input.Body.Policies)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &SystemVolumeBackupConfigOutput{Body: *config}, nil
}

func (h *SystemBackupHandler) ListSystemVolumeOptions(ctx context.Context, _ *struct{}) (*SystemVolumeBackupOptionsOutput, error) {
	options, err := h.service.ListSystemVolumeBackupOptions(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemVolumeBackupOptionsOutput{Body: options}, nil
}

func (h *SystemBackupHandler) RunSystemVolumeBackups(ctx context.Context, input *RunSystemVolumeBackupsInput) (*SystemVolumeBackupRunOutput, error) {
	request := backuptypes.RunSystemVolumeBackupsRequest{}
	if input.Body != nil {
		request = *input.Body
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.StartSystemVolumeBackups(utils.ActivityRuntimeContext(ctx, h.appCtx), *user, request)
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemVolumeBackupRunOutput{Body: *result}, nil
}

func (h *SystemBackupHandler) List(ctx context.Context, input *ListSystemBackupsInput) (*handlerutil.Page[backuptypes.SystemBackupRun], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	runs, page, err := h.service.ListBackups(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[backuptypes.SystemBackupRun]{
		Body: base.Paginated[backuptypes.SystemBackupRun]{
			Success:    true,
			Data:       runs,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *SystemBackupHandler) GetPolicies(ctx context.Context, _ *struct{}) (*SystemBackupPoliciesOutput, error) {
	policies, err := h.service.GetPolicies(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemBackupPoliciesOutput{Body: *policies}, nil
}

func (h *SystemBackupHandler) UpdatePolicies(ctx context.Context, input *UpdateSystemBackupPoliciesInput) (*SystemBackupPoliciesOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var policies *backuptypes.SystemBackupPolicyCollection
	_, err = activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: "policies", ResourceName: "Arcane", User: user,
		Step: "Saving system backup schedules", Message: "Saving Arcane system backup schedules", SuccessMessage: "Arcane system backup schedules saved",
		Metadata: database.JSON{"action": "update_system_backup_policies", "policyCount": len(input.Body.Policies)},
	}, func(activityCtx context.Context) error {
		var updateErr error
		policies, updateErr = h.service.UpdatePolicies(activityCtx, input.Body.Policies)
		return updateErr
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &SystemBackupPoliciesOutput{Body: *policies}, nil
}

func (h *SystemBackupHandler) GenerateRecoveryKey(_ context.Context, _ *struct{}) (*GenerateSystemBackupRecoveryKeyOutput, error) {
	recoveryKey, err := h.service.GenerateRecoveryKey()
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GenerateSystemBackupRecoveryKeyOutput{Body: *recoveryKey}, nil
}

func (h *SystemBackupHandler) SetRecoveryKey(ctx context.Context, input *SetSystemBackupRecoveryKeyInput) (*SystemBackupRecoveryKeyOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var status *backuptypes.SystemBackupRecoveryKeyStatus
	_, err = activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: "recovery-key", ResourceName: "Arcane", User: user,
		Step: "Configuring recovery key", Message: "Configuring Arcane system backup recovery key", SuccessMessage: "Arcane system backup recovery key configured",
		Metadata: database.JSON{"action": "set_system_backup_recovery_key"},
	}, func(activityCtx context.Context) error {
		var setErr error
		status, setErr = h.service.SetRecoveryKey(activityCtx, input.Body.RecoveryKey)
		return setErr
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &SystemBackupRecoveryKeyOutput{Body: *status}, nil
}

func (h *SystemBackupHandler) Discover(ctx context.Context, input *DiscoverSystemBackupsInput) (*handlerutil.Out[int], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	count := 0
	_, err = activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: "s3", ResourceName: "Arcane", User: user,
		Step: "Discovering system backups", Message: "Discovering Arcane system backups in S3", SuccessMessage: "Arcane system backup discovery completed",
		Metadata: database.JSON{"action": "discover_system_backups", "s3DestinationId": input.Body.S3DestinationID},
	}, func(activityCtx context.Context) error {
		var discoverErr error
		count, discoverErr = h.service.DiscoverRemoteBackups(activityCtx, input.Body)
		return discoverErr
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &handlerutil.Out[int]{Body: base.ApiResponse[int]{Success: true, Data: count}}, nil
}

func (h *SystemBackupHandler) Create(ctx context.Context, input *CreateSystemBackupInput) (*SystemBackupOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	run, err := h.service.StartBackup(utils.ActivityRuntimeContext(ctx, h.appCtx), *user, input.Body)
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemBackupOutput{Body: *run}, nil
}

func (h *SystemBackupHandler) Restore(ctx context.Context, input *RestoreSystemBackupInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	activityID, err := activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: input.ID, ResourceName: "Arcane", User: user,
		Step: "Preparing system restore", Message: "Preparing Arcane system restore", SuccessMessage: "Arcane system restore started",
		Metadata: database.JSON{"action": "restore_system_backup", "backupId": input.ID},
	}, func(activityCtx context.Context) error {
		return h.service.RestoreBackup(activityCtx, input.ID, input.Body.RecoveryKey, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return messageOutputInternal("Arcane system restore started", activityID), nil
}

// BrowseFiles returns one lazy-loaded project tree page.
func (h *SystemBackupHandler) BrowseFiles(ctx context.Context, input *BrowseSystemBackupFilesInput) (*handlerutil.Page[backuptypes.BackupFileEntry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, "", "", input.Search)
	items, page, err := h.service.BrowseBackupFiles(ctx, input.ID, input.Body.RecoveryKey, input.Path, params)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &handlerutil.Page[backuptypes.BackupFileEntry]{Body: base.Paginated[backuptypes.BackupFileEntry]{
		Success: true, Data: items, Pagination: handlerutil.PaginationResponse(page),
	}}, nil
}

// RestoreFiles restores selected project files from a system backup.
func (h *SystemBackupHandler) RestoreFiles(ctx context.Context, input *RestoreSystemBackupFilesInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	activityID, err := activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: input.ID, ResourceName: "Arcane", User: user,
		Step: "Restoring project files", Message: "Restoring project files from Arcane system backup", SuccessMessage: "Arcane project files restored successfully",
		Metadata: database.JSON{"action": "restore_system_backup_files", "backupId": input.ID, "pathCount": len(input.Body.Paths), "selectAll": input.Body.SelectAll, "search": input.Body.Search},
	}, func(activityCtx context.Context) error {
		return h.service.RestoreBackupFiles(activityCtx, input.ID, input.Body, *user)
	})
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if errors.Is(err, common.ErrBadRequest) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return messageOutputInternal("Arcane project files restored successfully", activityID), nil
}

func (h *SystemBackupHandler) Upload(ctx context.Context, input *UploadSystemBackupInput) (*SystemBackupOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var run *SystemBackupRun
	_, err = activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: input.ID, ResourceName: "Arcane", User: user,
		Step: "Uploading system backup", Message: "Uploading Arcane system backup", SuccessMessage: "Arcane system backup uploaded successfully",
		Metadata: database.JSON{"action": "upload_system_backup", "backupId": input.ID, "s3DestinationId": input.Body.S3DestinationID},
	}, func(activityCtx context.Context) error {
		var e error
		run, e = h.service.UploadBackup(activityCtx, input.ID, input.Body)
		return e
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	dto := run.ToDTO()
	return &SystemBackupOutput{Body: dto}, nil
}

func (h *SystemBackupHandler) Delete(ctx context.Context, input *DeleteSystemBackupInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	activityID, err := activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: input.ID, ResourceName: "Arcane", User: user,
		Step: "Deleting system backup", Message: "Deleting Arcane system backup", SuccessMessage: "Arcane system backup deleted successfully",
		Metadata: database.JSON{"action": "delete_system_backup", "backupId": input.ID},
	}, func(activityCtx context.Context) error {
		return h.service.DeleteBackup(activityCtx, input.ID, input.Body.RecoveryKey)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return messageOutputInternal("Arcane system backup deleted successfully", activityID), nil
}

func messageOutputInternal(message, activityID string) *handlerutil.Out[base.MessageResponse] {
	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: message, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}}}
}
