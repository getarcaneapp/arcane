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

type StatusOutput struct {
	Body base.ApiResponse[apnstypes.Status]
}

type PairingTokenOutput struct {
	Body base.ApiResponse[apnstypes.PairingToken]
}

type RegisterDeviceInput struct {
	Body apnstypes.RegisterDeviceRequest
}

type DeviceOutput struct {
	Body base.ApiResponse[apnstypes.Device]
}

type UpdateDeviceInput struct {
	ID   string `path:"id" doc:"Device ID"`
	Body apnstypes.UpdateDeviceRequest
}

type DeviceIDInput struct {
	ID string `path:"id" doc:"Device ID"`
}

type MessageOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

func RegisterApns(api huma.API, service *ApnsService) {
	h := &ApnsHandler{service: service}
	tags := []string{"Mobile Push"}

	huma.Register(api, huma.Operation{
		OperationID: "get-apns-status",
		Method:      http.MethodGet,
		Path:        "/apns/status",
		Summary:     "Get mobile push status",
		Description: "Whether mobile push is enabled and the caller's registered devices",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.Status)

	huma.Register(api, huma.Operation{
		OperationID: "create-apns-pairing-token",
		Method:      http.MethodPost,
		Path:        "/apns/pairing-token",
		Summary:     "Issue a pairing token",
		Description: "Issue a short-lived signed token the mobile app presents to the push relay",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.PairingToken)

	huma.Register(api, huma.Operation{
		OperationID: "register-apns-device",
		Method:      http.MethodPost,
		Path:        "/apns/devices",
		Summary:     "Register a mobile device",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.RegisterDevice)

	huma.Register(api, huma.Operation{
		OperationID: "update-apns-device",
		Method:      http.MethodPatch,
		Path:        "/apns/devices/{id}",
		Summary:     "Update a mobile device",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.UpdateDevice)

	huma.Register(api, huma.Operation{
		OperationID: "delete-apns-device",
		Method:      http.MethodDelete,
		Path:        "/apns/devices/{id}",
		Summary:     "Remove a mobile device",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.DeleteDevice)

	huma.Register(api, huma.Operation{
		OperationID: "test-apns-device",
		Method:      http.MethodPost,
		Path:        "/apns/devices/{id}/test",
		Summary:     "Send a test push",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.TestDevice)
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

func (h *ApnsHandler) Status(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	status, err := h.service.Status(ctx, user.ID)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &StatusOutput{Body: base.ApiResponse[apnstypes.Status]{Success: true, Data: status}}, nil
}

func (h *ApnsHandler) PairingToken(ctx context.Context, _ *struct{}) (*PairingTokenOutput, error) {
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}
	token, err := h.service.IssuePairingToken(ctx)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &PairingTokenOutput{Body: base.ApiResponse[apnstypes.PairingToken]{Success: true, Data: token}}, nil
}

func (h *ApnsHandler) RegisterDevice(ctx context.Context, input *RegisterDeviceInput) (*DeviceOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.RegisterDevice(ctx, user.ID, input.Body)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &DeviceOutput{Body: base.ApiResponse[apnstypes.Device]{Success: true, Data: device}}, nil
}

func (h *ApnsHandler) UpdateDevice(ctx context.Context, input *UpdateDeviceInput) (*DeviceOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.UpdateDevice(ctx, user.ID, input.ID, input.Body)
	if err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &DeviceOutput{Body: base.ApiResponse[apnstypes.Device]{Success: true, Data: device}}, nil
}

func (h *ApnsHandler) DeleteDevice(ctx context.Context, input *DeviceIDInput) (*MessageOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.DeleteDevice(ctx, user.ID, input.ID); err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &MessageOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Device removed"}}}, nil
}

func (h *ApnsHandler) TestDevice(ctx context.Context, input *DeviceIDInput) (*MessageOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.TestDevice(ctx, user.ID, input.ID); err != nil {
		return nil, apnsHTTPErrorInternal(err)
	}
	return &MessageOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Test notification queued"}}}, nil
}
