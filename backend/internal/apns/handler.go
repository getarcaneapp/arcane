package apns

import (
	"context"
	"net/http"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	apnstypes "github.com/getarcaneapp/arcane/types/v2/apns"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

type ApnsHandler struct {
	service *ApnsService
}

type RegisterDeviceInput struct {
	Body apnstypes.RegisterDeviceRequest
}

type UpdateDeviceInput struct {
	ID   string `path:"id" doc:"Device ID"`
	Body apnstypes.UpdateDeviceRequest
}

type DeviceIDInput struct {
	ID string `path:"id" doc:"Device ID"`
}

func RegisterApns(api huma.API, service *ApnsService) {
	h := &ApnsHandler{service: service}

	huma.Register(api, securedApnsOperationInternal("get-apns-status", http.MethodGet, "/apns/status", "Get mobile push status", "Whether mobile push is enabled and the caller's registered devices"), h.Status)
	huma.Register(api, securedApnsOperationInternal("create-apns-pairing-token", http.MethodPost, "/apns/pairing-token", "Issue a pairing token", "Issue a short-lived signed token the mobile app presents to the push relay"), h.PairingToken)
	huma.Register(api, securedApnsOperationInternal("register-apns-device", http.MethodPost, "/apns/devices", "Register a mobile device", ""), h.RegisterDevice)
	huma.Register(api, securedApnsOperationInternal("update-apns-device", http.MethodPatch, "/apns/devices/{id}", "Update a mobile device", ""), h.UpdateDevice)
	huma.Register(api, securedApnsOperationInternal("delete-apns-device", http.MethodDelete, "/apns/devices/{id}", "Remove a mobile device", ""), h.DeleteDevice)
	huma.Register(api, securedApnsOperationInternal("test-apns-device", http.MethodPost, "/apns/devices/{id}/test", "Send a test push", ""), h.TestDevice)
}

func securedApnsOperationInternal(operationID, method, path, summary, description string) huma.Operation {
	op := handlerutil.Operation(operationID, method, path, summary, description, "Mobile Push")
	op.Security = handlerutil.DefaultOperationSecurity()
	return op
}

func apnsHTTPErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrApnsDisabled):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, common.ErrApnsDeviceNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrApnsDeviceConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrApnsRelay):
		return huma.Error502BadGateway(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}

func (h *ApnsHandler) Status(ctx context.Context, _ *struct{}) (*handlerutil.Out[apnstypes.Status], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	status, err := h.service.Status(ctx, user.ID)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &handlerutil.Out[apnstypes.Status]{Body: base.ApiResponse[apnstypes.Status]{Success: true, Data: status}}, nil
}

func (h *ApnsHandler) PairingToken(ctx context.Context, _ *struct{}) (*handlerutil.Out[apnstypes.PairingToken], error) {
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}
	token, err := h.service.IssuePairingToken(ctx)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &handlerutil.Out[apnstypes.PairingToken]{Body: base.ApiResponse[apnstypes.PairingToken]{Success: true, Data: token}}, nil
}

func (h *ApnsHandler) RegisterDevice(ctx context.Context, input *RegisterDeviceInput) (*handlerutil.Out[apnstypes.Device], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.RegisterDevice(ctx, user.ID, input.Body)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &handlerutil.Out[apnstypes.Device]{Body: base.ApiResponse[apnstypes.Device]{Success: true, Data: device}}, nil
}

func (h *ApnsHandler) UpdateDevice(ctx context.Context, input *UpdateDeviceInput) (*handlerutil.Out[apnstypes.Device], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.UpdateDevice(ctx, user.ID, input.ID, input.Body)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &handlerutil.Out[apnstypes.Device]{Body: base.ApiResponse[apnstypes.Device]{Success: true, Data: device}}, nil
}

func (h *ApnsHandler) DeleteDevice(ctx context.Context, input *DeviceIDInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.DeleteDevice(ctx, user.ID, input.ID); err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Device removed"}}}, nil
}

func (h *ApnsHandler) TestDevice(ctx context.Context, input *DeviceIDInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.TestDevice(ctx, user.ID, input.ID); err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Test notification queued"}}}, nil
}
