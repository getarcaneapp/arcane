package apns

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type ApnsHandler struct {
	service *ApnsService
}

type RegisterDeviceInput struct {
	Body types.ApnsRegisterDeviceRequest
}

type UpdateDeviceInput struct {
	ID string `path:"id" doc:"Device ID"`
	types.Update[types.ApnsDevice]
}

type DeviceIDInput struct {
	ID string `path:"id" doc:"Device ID"`
}

type DeviceActionInput struct {
	ID     string `path:"id" doc:"Device ID"`
	Action string `query:"action" required:"true" enum:"test" doc:"Device action"`
}

func securedApnsOperation(operationID, method, path, summary, description string) huma.Operation {
	op := handlerutil.Operation(operationID, method, path, summary, description, "Mobile Push")
	op.Security = handlerutil.DefaultOperationSecurity()
	return op
}

func apnsHTTPError(err error) error {
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

func (h *ApnsHandler) Status(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.ApnsStatus], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	status, err := h.service.Status(ctx, user.ID)
	if err != nil {
		return nil, apnsHTTPError(err)
	}
	return &handlerutil.Out[types.ApnsStatus]{Body: types.ApiResponse[types.ApnsStatus]{Success: true, Data: status}}, nil
}

func (h *ApnsHandler) PairingToken(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.ApnsPairingToken], error) {
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}
	token, err := h.service.IssuePairingToken(ctx)
	if err != nil {
		return nil, apnsHTTPError(err)
	}
	return &handlerutil.Out[types.ApnsPairingToken]{Body: types.ApiResponse[types.ApnsPairingToken]{Success: true, Data: token}}, nil
}

func (h *ApnsHandler) RegisterDevice(ctx context.Context, input *RegisterDeviceInput) (*handlerutil.Out[types.ApnsDevice], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.RegisterDevice(ctx, user.ID, input.Body)
	if err != nil {
		return nil, apnsHTTPError(err)
	}
	return &handlerutil.Out[types.ApnsDevice]{Body: types.ApiResponse[types.ApnsDevice]{Success: true, Data: device}}, nil
}

func (h *ApnsHandler) UpdateDevice(ctx context.Context, input *UpdateDeviceInput) (*handlerutil.Out[types.ApnsDevice], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}
	device, err := h.service.UpdateDevice(ctx, user.ID, input.ID, input.Body, submitted)
	if err != nil {
		return nil, apnsHTTPError(err)
	}
	return &handlerutil.Out[types.ApnsDevice]{Body: types.ApiResponse[types.ApnsDevice]{Success: true, Data: device}}, nil
}

func (h *ApnsHandler) DeleteDevice(ctx context.Context, input *DeviceIDInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if deleteDeviceErr := h.service.DeleteDevice(ctx, user.ID, input.ID); deleteDeviceErr != nil {
		return nil, apnsHTTPError(deleteDeviceErr)
	}
	return handlerutil.MessageOutput("Device removed", ""), nil
}

func (h *ApnsHandler) TestDevice(ctx context.Context, input *DeviceActionInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if testDeviceErr := h.service.TestDevice(ctx, user.ID, input.ID); testDeviceErr != nil {
		return nil, apnsHTTPError(testDeviceErr)
	}
	return handlerutil.MessageOutput("Test notification queued", ""), nil
}
