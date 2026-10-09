package backup

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Handler struct {
	service  *Service
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

type SystemBackupPoliciesOutput struct {
	Body backup.SystemBackupPolicyCollection
}

type UpdateSystemBackupPoliciesInput struct {
	Body backup.UpdateSystemBackupPolicies
}

type SetSystemBackupRecoveryKeyInput struct {
	Body backup.SystemBackupRecoveryKey
}

type SystemBackupRecoveryKeyOutput struct {
	Body backup.SystemBackupRecoveryKeyStatus
}

type GenerateSystemBackupRecoveryKeyOutput struct {
	Body backup.SystemBackupRecoveryKey
}

type CreateSystemBackupInput struct {
	Body backup.CreateSystemBackupRequest
}

type (
	SystemBackupOutput       struct{ Body backup.SystemBackupRun }
	RestoreSystemBackupInput struct {
		ID   string `path:"id"`
		Body backup.RestoreSystemBackupRequest
	}
)

// BrowseSystemBackupFilesInput selects one page of a system backup tree.
type BrowseSystemBackupFilesInput struct {
	ID     string `path:"id"`
	Path   string `query:"path" doc:"Folder path relative to the backup root"`
	Search string `query:"search" doc:"Case-insensitive full-path search"`
	Start  int    `query:"start" default:"0" doc:"Start index for the page"`
	Limit  int    `query:"limit" default:"20" doc:"Requested page size"`
	Body   backup.SystemBackupRecoveryKey
}

// RestoreSystemBackupFilesInput selects project files to restore from a system backup.
type RestoreSystemBackupFilesInput struct {
	ID   string `path:"id"`
	Body backup.RestoreSystemBackupFilesRequest
}

type UploadSystemBackupInput struct {
	ID   string `path:"id"`
	Body backup.UploadSystemBackupRequest
}

type DeleteSystemBackupInput struct {
	ID   string `path:"id"`
	Body backup.DeleteSystemBackupRequest
}

type DiscoverSystemBackupsInput struct {
	Body backup.DiscoverSystemBackupsRequest
}

func NewHandler(service *Service, activityService *activity.ActivityService, appCtx context.Context) *Handler {
	return &Handler{service: service, activity: activityService, appCtx: appCtx}
}

func (h *Handler) ListHistory(ctx context.Context, input *ListBackupHistoryInput) (*handlerutil.Page[backup.HistoryEntry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.Filters = map[string]string{"type": input.Type}
	history, page, err := h.service.ListBackupHistory(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[backup.HistoryEntry]{
		Body: base.Paginated[backup.HistoryEntry]{
			Success:    true,
			Data:       history,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *Handler) List(ctx context.Context, input *ListSystemBackupsInput) (*handlerutil.Page[backup.SystemBackupRun], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	runs, page, err := h.service.ListBackups(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[backup.SystemBackupRun]{
		Body: base.Paginated[backup.SystemBackupRun]{
			Success:    true,
			Data:       runs,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *Handler) GetPolicies(ctx context.Context, _ *struct{}) (*SystemBackupPoliciesOutput, error) {
	policies, err := h.service.GetPolicies(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemBackupPoliciesOutput{Body: *policies}, nil
}

func (h *Handler) UpdatePolicies(ctx context.Context, input *UpdateSystemBackupPoliciesInput) (*SystemBackupPoliciesOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var policies *backup.SystemBackupPolicyCollection
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

func (h *Handler) GenerateRecoveryKey(_ context.Context, _ *struct{}) (*GenerateSystemBackupRecoveryKeyOutput, error) {
	recoveryKey, err := h.service.GenerateRecoveryKey()
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GenerateSystemBackupRecoveryKeyOutput{Body: *recoveryKey}, nil
}

func (h *Handler) SetRecoveryKey(ctx context.Context, input *SetSystemBackupRecoveryKeyInput) (*SystemBackupRecoveryKeyOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var status *backup.SystemBackupRecoveryKeyStatus
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

func (h *Handler) Discover(ctx context.Context, input *DiscoverSystemBackupsInput) (*handlerutil.Out[int], error) {
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

func (h *Handler) Create(ctx context.Context, input *CreateSystemBackupInput) (*SystemBackupOutput, error) {
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

func (h *Handler) Restore(ctx context.Context, input *RestoreSystemBackupInput) (*handlerutil.Out[base.MessageResponse], error) {
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
	return handlerutil.MessageOutput("Arcane system restore started", activityID), nil
}

// BrowseFiles returns one lazy-loaded project tree page.
func (h *Handler) BrowseFiles(ctx context.Context, input *BrowseSystemBackupFilesInput) (*handlerutil.Page[backup.BackupFileEntry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, "", "", input.Search)
	items, page, err := h.service.BrowseBackupFiles(ctx, input.ID, input.Body.RecoveryKey, input.Path, params)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &handlerutil.Page[backup.BackupFileEntry]{Body: base.Paginated[backup.BackupFileEntry]{
		Success: true, Data: items, Pagination: handlerutil.PaginationResponse(page),
	}}, nil
}

// RestoreFiles restores selected project files from a system backup.
func (h *Handler) RestoreFiles(ctx context.Context, input *RestoreSystemBackupFilesInput) (*handlerutil.Out[base.MessageResponse], error) {
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
	return handlerutil.MessageOutput("Arcane project files restored successfully", activityID), nil
}

func (h *Handler) Upload(ctx context.Context, input *UploadSystemBackupInput) (*SystemBackupOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var run *backup.SystemBackupRun
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
	return &SystemBackupOutput{Body: *run}, nil
}

func (h *Handler) Delete(ctx context.Context, input *DeleteSystemBackupInput) (*handlerutil.Out[base.MessageResponse], error) {
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
	return handlerutil.MessageOutput("Arcane system backup deleted successfully", activityID), nil
}
