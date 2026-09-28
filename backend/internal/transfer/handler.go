package transfer

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	transferlib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/transfer"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	"go.getarcane.app/kit/pkg/mapping"
)

type envInputInternal struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type bodyInputInternal[T any] struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          T
}

type transferInputInternal[T any] struct {
	EnvironmentID string `path:"id" doc:"Source environment ID"`
	TransferID    string `path:"transferId" doc:"Transfer ID"`
	Body          T
}

type transferRefInternal struct {
	EnvironmentID string `path:"id" doc:"Source environment ID"`
	TransferID    string `path:"transferId" doc:"Transfer ID"`
}

type volumeRefInternal struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type projectRefInternal struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type exportInputInternal struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ExportID      string `path:"exportId" doc:"Export ID"`
	Offset        int64  `query:"offset" minimum:"0" doc:"Byte offset"`
	Length        int64  `query:"length" minimum:"1" doc:"Bytes to read"`
}

type volumeInputInternal[T any] struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Body          T
}

type projectInputInternal[T any] struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          T
}

type releaseHoldInputInternal struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Kind          string `path:"kind" doc:"Resource kind"`
	Resource      string `path:"resource" doc:"Resource name or ID"`
	TransferID    string `query:"transferId" doc:"Owning transfer ID"`
}

type createOutputInternal struct {
	Status int
	Body   base.ApiResponse[transfertypes.Transfer]
}

var done = base.MessageResponse{Message: "ok"}

func httpErrorInternal(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, common.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrValidation), errors.Is(err, common.ErrBadRequest):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, common.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}

// routeInternal registers one operation: the call returns the payload and the
// wrapper applies the envelope, permission lookup, and error mapping.
func routeInternal[I, O any](api huma.API, id, method, path, summary, tag, perm string, call func(context.Context, *I, *authz.PermissionSet) (O, error)) {
	handlerutil.RegisterSecured(api, handlerutil.Operation(id, method, path, summary, "", tag), perm, func(ctx context.Context, input *I) (*handlerutil.Out[O], error) {
		permissions, ok := middleware.PermissionsFromContext(ctx)
		if !ok || permissions == nil {
			return nil, huma.Error401Unauthorized("Not authenticated")
		}
		out, err := call(ctx, input, permissions)
		if err != nil {
			return nil, httpErrorInternal(err)
		}
		return &handlerutil.Out[O]{Body: base.ApiResponse[O]{Success: true, Data: out}}, nil
	})
}

// actionInternal adapts an error-only call to the envelope shape.
func actionInternal[I any](call func(context.Context, *I) error) func(context.Context, *I, *authz.PermissionSet) (base.MessageResponse, error) {
	return func(ctx context.Context, input *I, _ *authz.PermissionSet) (base.MessageResponse, error) {
		return done, call(ctx, input)
	}
}

// dtoInternal renders the API view of a record.
func dtoInternal(record *ResourceTransfer, err error) (transfertypes.Transfer, error) {
	if err != nil {
		return transfertypes.Transfer{}, err
	}
	return mapping.MapOne[ResourceTransfer, transfertypes.Transfer](*record)
}

// registerManagerRoutesInternal mounts the coordinator API once per kind so
// each subtree carries its own transfer permission.
func registerManagerRoutesInternal(api huma.API, s *Service) {
	for _, entry := range []struct {
		segment string
		kind    transfertypes.Kind
		perm    string
	}{{"projects", transfertypes.KindProject, authz.PermProjectsTransfer}, {"volumes", transfertypes.KindVolume, authz.PermVolumesTransfer}} {
		kind, perm := entry.kind, entry.perm
		prefix := "/environments/{id}/transfers/" + entry.segment
		id := func(name string) string { return name + "-" + string(kind) + "-transfer" }
		routeInternal(api, id("preflight"), http.MethodPost, prefix+"/preflight", "Preflight a transfer", "Transfers", perm, func(ctx context.Context, in *bodyInputInternal[transfertypes.Request], ps *authz.PermissionSet) (transfertypes.Plan, error) {
			in.Body.Kind = kind
			return s.Preflight(ctx, in.EnvironmentID, in.Body, ps)
		})
		handlerutil.RegisterSecured(api, handlerutil.Operation(id("create"), http.MethodPost, prefix, "Start a transfer", "", "Transfers"), perm, func(ctx context.Context, in *bodyInputInternal[transfertypes.CreateRequest]) (*createOutputInternal, error) {
			user, err := handlerutil.RequireUser(ctx)
			if err != nil {
				return nil, err
			}
			permissions, _ := middleware.PermissionsFromContext(ctx)
			in.Body.Request.Kind = kind
			record, created, err := s.Create(ctx, in.EnvironmentID, in.Body, user, permissions)
			dto, err := dtoInternal(record, err)
			if err != nil {
				return nil, httpErrorInternal(err)
			}
			status := http.StatusOK
			if created {
				status = http.StatusCreated
			}
			return &createOutputInternal{Status: status, Body: base.ApiResponse[transfertypes.Transfer]{Success: true, Data: dto}}, nil
		})
		routeInternal(api, id("list"), http.MethodGet, prefix, "List transfers", "Transfers", perm, func(ctx context.Context, in *envInputInternal, _ *authz.PermissionSet) ([]transfertypes.Transfer, error) {
			records, err := s.List(ctx, in.EnvironmentID, kind)
			if err != nil {
				return nil, err
			}
			return mapping.MapSlice[ResourceTransfer, transfertypes.Transfer](records)
		})
		routeInternal(api, id("get"), http.MethodGet, prefix+"/{transferId}", "Get a transfer", "Transfers", perm, func(ctx context.Context, in *transferRefInternal, _ *authz.PermissionSet) (transfertypes.Transfer, error) {
			return dtoInternal(s.Get(ctx, in.EnvironmentID, kind, in.TransferID))
		})
		routeInternal(api, id("cancel"), http.MethodPost, prefix+"/{transferId}/cancel", "Cancel a transfer", "Transfers", perm, func(ctx context.Context, in *transferRefInternal, ps *authz.PermissionSet) (transfertypes.Transfer, error) {
			return dtoInternal(s.Cancel(ctx, in.EnvironmentID, kind, in.TransferID, ps))
		})
		routeInternal(api, id("retry"), http.MethodPost, prefix+"/{transferId}/retry", "Retry a transfer", "Transfers", perm, func(ctx context.Context, in *transferRefInternal, ps *authz.PermissionSet) (transfertypes.Transfer, error) {
			return dtoInternal(s.Retry(ctx, in.EnvironmentID, kind, in.TransferID, ps))
		})
		routeInternal(api, id("rollback"), http.MethodPost, prefix+"/{transferId}/rollback", "Roll back a move", "Transfers", perm, func(ctx context.Context, in *transferInputInternal[transfertypes.RollbackRequest], ps *authz.PermissionSet) (transfertypes.Transfer, error) {
			return dtoInternal(s.Rollback(ctx, in.EnvironmentID, kind, in.TransferID, in.Body, ps))
		})
		routeInternal(api, id("cleanup"), http.MethodPost, prefix+"/{transferId}/cleanup", "Clean up moved source resources", "Transfers", perm, func(ctx context.Context, in *transferInputInternal[transfertypes.CleanupRequest], ps *authz.PermissionSet) (transfertypes.CleanupResponse, error) {
			return s.Cleanup(ctx, in.EnvironmentID, kind, in.TransferID, in.Body, ps)
		})
		routeInternal(api, id("release-hold"), http.MethodPost, prefix+"/{transferId}/release-hold", "Release the source hold", "Transfers", perm, func(ctx context.Context, in *transferRefInternal, ps *authz.PermissionSet) (transfertypes.Transfer, error) {
			return dtoInternal(s.ReleaseHold(ctx, in.EnvironmentID, kind, in.TransferID, ps))
		})
	}
}

// registerNodeRoutesInternal mounts the operations every environment serves
// for a coordinating manager.
func registerNodeRoutesInternal(api huma.API, s *Service) {
	prefix := "/environments/{id}/transfer"
	tag := "Transfer Node"
	volumes, projects := authz.PermVolumesTransfer, authz.PermProjectsTransfer
	routeInternal(api, "transfer-node-capabilities", http.MethodGet, prefix+"/capabilities", "Transfer capabilities", tag, projects, func(ctx context.Context, _ *envInputInternal, _ *authz.PermissionSet) (transfertypes.Capabilities, error) {
		return s.Capabilities(ctx)
	})
	routeInternal(api, "transfer-node-export", http.MethodPost, prefix+"/exports", "Stage a source archive", tag, volumes, func(ctx context.Context, in *bodyInputInternal[transfertypes.ExportRequest], _ *authz.PermissionSet) (transfertypes.Export, error) {
		return s.Export(ctx, in.Body)
	})
	routeInternal(api, "transfer-node-export-read", http.MethodGet, prefix+"/exports/{exportId}", "Read a byte range of a staged archive", tag, volumes, func(_ context.Context, in *exportInputInternal, _ *authz.PermissionSet) (transfertypes.ExportRange, error) {
		data, err := s.ExportRead(in.ExportID, in.Offset, in.Length)
		return transfertypes.ExportRange{Data: data}, err
	})
	routeInternal(api, "transfer-node-close-export", http.MethodDelete, prefix+"/exports/{exportId}", "Drop a staged archive", tag, volumes, actionInternal(func(_ context.Context, in *exportInputInternal) error {
		s.spool.Remove(in.ExportID)
		return nil
	}))
	routeInternal(api, "transfer-node-import", http.MethodPost, prefix+"/imports", "Extract an uploaded archive into a new target", tag, volumes, actionInternal(func(ctx context.Context, in *bodyInputInternal[transfertypes.ImportRequest]) error {
		return s.Import(ctx, in.Body)
	}))
	routeInternal(api, "transfer-node-hold", http.MethodPost, prefix+"/holds", "Reserve a resource", tag, volumes, func(ctx context.Context, in *bodyInputInternal[transfertypes.HoldRequest], _ *authz.PermissionSet) (transfertypes.Hold, error) {
		return s.Hold(ctx, in.Body)
	})
	routeInternal(api, "transfer-node-release-hold", http.MethodDelete, prefix+"/holds/{kind}/{resource}", "Release a reservation", tag, volumes, actionInternal(func(ctx context.Context, in *releaseHoldInputInternal) error {
		err := s.holds.Release(ctx, in.TransferID, transfertypes.Kind(in.Kind), in.Resource)
		if errors.Is(err, transferlib.ErrHeldByOther) {
			return common.Classify(common.ErrResourceHeldByTransfer, err)
		}
		return err
	}))
	routeInternal(api, "transfer-node-inspect-volume", http.MethodGet, prefix+"/volumes/{volumeName}", "Inspect a volume for transfer", tag, volumes, func(ctx context.Context, in *volumeRefInternal, _ *authz.PermissionSet) (transfertypes.VolumeInspection, error) {
		return s.volumes.InspectForTransfer(ctx, in.VolumeName)
	})
	routeInternal(api, "transfer-node-remove-volume", http.MethodPost, prefix+"/volumes/{volumeName}/remove", "Remove a volume after a transfer", tag, volumes, actionInternal(func(ctx context.Context, in *volumeInputInternal[transfertypes.RemoveRequest]) error {
		return s.volumes.RemoveTransferVolume(ctx, in.VolumeName, in.Body)
	}))
	routeInternal(api, "transfer-node-stop-consumers", http.MethodPost, prefix+"/consumers/stop", "Stop consumers gracefully", tag, volumes, func(ctx context.Context, in *bodyInputInternal[transfertypes.StopConsumersRequest], _ *authz.PermissionSet) (transfertypes.StopConsumersResponse, error) {
		return s.volumes.StopTransferConsumers(ctx, in.Body)
	})
	routeInternal(api, "transfer-node-restore-consumers", http.MethodPost, prefix+"/consumers/restore", "Restart recorded consumers", tag, volumes, func(ctx context.Context, in *bodyInputInternal[transfertypes.RestoreConsumersRequest], _ *authz.PermissionSet) (transfertypes.RestoreConsumersResponse, error) {
		return s.volumes.RestoreTransferConsumers(ctx, in.Body)
	})
	routeInternal(api, "transfer-node-inspect-project", http.MethodGet, prefix+"/projects/{projectId}", "Inspect a project for transfer", tag, projects, func(ctx context.Context, in *projectRefInternal, _ *authz.PermissionSet) (transfertypes.ProjectInspection, error) {
		return s.projects.InspectForTransfer(ctx, in.ProjectID)
	})
	routeInternal(api, "transfer-node-check-destination", http.MethodPost, prefix+"/projects/check", "Check a project destination", tag, projects, func(ctx context.Context, in *bodyInputInternal[transfertypes.DestinationCheckRequest], _ *authz.PermissionSet) (transfertypes.DestinationCheckResponse, error) {
		return s.projects.CheckTransferDestination(ctx, in.Body)
	})
	routeInternal(api, "transfer-node-register-project", http.MethodPost, prefix+"/projects/register", "Register an imported project", tag, projects, func(ctx context.Context, in *bodyInputInternal[transfertypes.RegisterProjectRequest], _ *authz.PermissionSet) (transfertypes.RegisterProjectResponse, error) {
		return s.projects.RegisterTransferredProject(ctx, in.Body)
	})
	routeInternal(api, "transfer-node-prepare-project", http.MethodPost, prefix+"/projects/{projectId}/prepare", "Prepare a transferred project", tag, projects, actionInternal(func(ctx context.Context, in *projectInputInternal[transfertypes.ProjectActionRequest]) error {
		return s.projects.PrepareTransferredProject(ctx, in.ProjectID, in.Body)
	}))
	routeInternal(api, "transfer-node-deploy-project", http.MethodPost, prefix+"/projects/{projectId}/deploy", "Deploy a transferred project", tag, projects, actionInternal(func(ctx context.Context, in *projectInputInternal[transfertypes.ProjectActionRequest]) error {
		return s.projects.DeployTransferredProject(ctx, in.ProjectID, in.Body)
	}))
	routeInternal(api, "transfer-node-remove-project", http.MethodPost, prefix+"/projects/{projectId}/remove", "Remove a project after a transfer", tag, projects, actionInternal(func(ctx context.Context, in *projectInputInternal[transfertypes.RemoveRequest]) error {
		return s.projects.RemoveTransferProject(ctx, in.ProjectID, in.Body)
	}))
}
