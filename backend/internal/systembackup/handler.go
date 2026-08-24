package systembackup

import (
	"context"
	"net/http"
	"strings"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
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

type ListSystemBackupsOutput struct {
	Body struct {
		Data       []backuptypes.SystemBackupRun `json:"data"`
		Pagination base.PaginationResponse       `json:"pagination"`
	}
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

// ListSystemBackupFilesInput identifies a system backup whose project files should be listed.
type ListSystemBackupFilesInput struct {
	ID   string `path:"id"`
	Body backuptypes.ListSystemBackupFilesRequest
}

// ListSystemBackupFilesOutput contains project files eligible for selective restore.
type ListSystemBackupFilesOutput struct{ Body base.ApiResponse[[]string] }

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
type DiscoverSystemBackupsOutput struct{ Body base.ApiResponse[int] }
type SystemBackupMessageOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

func RegisterSystemBackups(api huma.API, service *SystemBackupService, activityService *activity.ActivityService, appCtx handlerutil.ActivityAppContext) {
	h := &SystemBackupHandler{service: service, activity: activityService, appCtx: appCtx.Context()}
	adminOnly := middleware.RequireGlobalAdmin(api)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-system-backups", Method: http.MethodGet, Path: "/backups", Summary: "List Arcane system backups", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.List)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-system-backup-policies", Method: http.MethodGet, Path: "/backups/policies", Summary: "Get Arcane system backup policies", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.GetPolicies)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-system-backup-policies", Method: http.MethodPut, Path: "/backups/policies", Summary: "Update Arcane system backup policies", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.UpdatePolicies)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "generate-system-backup-recovery-key", Method: http.MethodPost, Path: "/backups/recovery-key/generate", Summary: "Generate an Arcane system backup recovery key", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRecoveryKey, h.GenerateRecoveryKey)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "set-system-backup-recovery-key", Method: http.MethodPut, Path: "/backups/recovery-key", Summary: "Configure Arcane system backup recovery key", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRecoveryKey, h.SetRecoveryKey)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "create-system-backup", Method: http.MethodPost, Path: "/backups", Summary: "Create Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Create)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "discover-system-backups", Method: http.MethodPost, Path: "/backups/discover", Summary: "Discover Arcane system backups in S3", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Discover)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "restore-system-backup", Method: http.MethodPost, Path: "/backups/{id}/restore", Summary: "Restore Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRestore, h.Restore)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-system-backup-files", Method: http.MethodPost, Path: "/backups/{id}/files", Summary: "List project files in an Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRead, h.ListFiles)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "restore-system-backup-files", Method: http.MethodPost, Path: "/backups/{id}/restore-files", Summary: "Restore project files from an Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsRestore, h.RestoreFiles)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "upload-system-backup", Method: http.MethodPost, Path: "/backups/{id}/upload", Summary: "Upload Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Upload)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-system-backup", Method: http.MethodDelete, Path: "/backups/{id}", Summary: "Delete Arcane system backup", Tags: []string{"System Backups"}, Middlewares: adminOnly}, authz.PermSystemBackupsManage, h.Delete)
}

func (h *SystemBackupHandler) List(ctx context.Context, input *ListSystemBackupsInput) (*ListSystemBackupsOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	runs, page, err := h.service.ListBackups(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	out := &ListSystemBackupsOutput{}
	out.Body.Data, out.Body.Pagination = runs, handlerutil.PaginationResponse(page)
	return out, nil
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

func (h *SystemBackupHandler) Discover(ctx context.Context, input *DiscoverSystemBackupsInput) (*DiscoverSystemBackupsOutput, error) {
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
	return &DiscoverSystemBackupsOutput{Body: base.ApiResponse[int]{Success: true, Data: count}}, nil
}

func (h *SystemBackupHandler) Create(ctx context.Context, input *CreateSystemBackupInput) (*SystemBackupOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var run *SystemBackupRun
	_, err = activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: "arcane", ResourceName: "Arcane", User: user,
		Step: "Creating system backup", Message: "Creating Arcane system backup", SuccessMessage: "Arcane system backup created successfully",
		Metadata: database.JSON{"action": "create_system_backup", "destination": input.Body.Destination, "s3DestinationId": input.Body.S3DestinationID, "policyId": input.Body.PolicyID},
	}, func(activityCtx context.Context) error {
		var e error
		run, e = h.service.CreateBackup(activityCtx, *user, SystemBackupTriggerManual, input.Body)
		return e
	})
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	dto := run.ToDTO()
	return &SystemBackupOutput{Body: dto}, nil
}

func (h *SystemBackupHandler) Restore(ctx context.Context, input *RestoreSystemBackupInput) (*SystemBackupMessageOutput, error) {
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

// ListFiles lists project files eligible for selective restore from a system backup.
func (h *SystemBackupHandler) ListFiles(ctx context.Context, input *ListSystemBackupFilesInput) (*ListSystemBackupFilesOutput, error) {
	files, err := h.service.ListBackupFiles(ctx, input.ID, input.Body.RecoveryKey)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &ListSystemBackupFilesOutput{Body: base.ApiResponse[[]string]{Success: true, Data: files}}, nil
}

// RestoreFiles restores selected project files from a system backup.
func (h *SystemBackupHandler) RestoreFiles(ctx context.Context, input *RestoreSystemBackupFilesInput) (*SystemBackupMessageOutput, error) {
	if len(input.Body.Paths) == 0 {
		return nil, huma.Error400BadRequest("paths are required")
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	activityID, err := activitylib.RunHandlerActivity(utils.ActivityRuntimeContext(ctx, h.appCtx), h.activity, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup", ResourceID: input.ID, ResourceName: "Arcane", User: user,
		Step: "Restoring project files", Message: "Restoring project files from Arcane system backup", SuccessMessage: "Arcane project files restored successfully",
		Metadata: database.JSON{"action": "restore_system_backup_files", "backupId": input.ID, "pathCount": len(input.Body.Paths)},
	}, func(activityCtx context.Context) error {
		return h.service.RestoreBackupFiles(activityCtx, input.ID, input.Body, *user)
	})
	if errors.Is(err, ErrSystemBackupAlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if errors.Is(err, errInvalidSystemBackupFilePath) {
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

func (h *SystemBackupHandler) Delete(ctx context.Context, input *DeleteSystemBackupInput) (*SystemBackupMessageOutput, error) {
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

func messageOutputInternal(message, activityID string) *SystemBackupMessageOutput {
	return &SystemBackupMessageOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: message, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}}}
}
