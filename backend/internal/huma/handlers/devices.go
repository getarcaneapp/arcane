package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
)

// DeviceDTO is the JSON shape returned to the web UI for a paired device.
// Mirrors models.Device but with a stable name for OpenAPI consumers.
type DeviceDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	DeviceID    string     `json:"deviceId"`
	AppVersion  string     `json:"appVersion"`
	OsVersion   string     `json:"osVersion"`
	DeviceModel string     `json:"deviceModel"`
	PairedAt    time.Time  `json:"pairedAt"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty"`
}

func deviceModelToDTO(d models.Device) DeviceDTO {
	return DeviceDTO{
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

// PairingCodeResponse is what the web UI receives from POST /devices/pairing-codes.
type PairingCodeResponse struct {
	ID                 string    `json:"id"`
	ShortCode          string    `json:"shortCode"`    // formatted as "ABCD-1234"
	ShortCodeRaw       string    `json:"shortCodeRaw"` // unformatted, used for clipboard copy
	QrPayload          string    `json:"qrPayload"`    // arcane://pair?u=...&c=...
	ExpiresAt          time.Time `json:"expiresAt"`
	ExpiresInSeconds   int64     `json:"expiresInSeconds"`
	ServerInsecureFlag bool      `json:"serverInsecure,omitempty"`
}

// PairingCodeStatusResponse is what GET /devices/pairing-codes/{id} returns.
type PairingCodeStatusResponse struct {
	Status     string     `json:"status"` // "pending" | "redeemed" | "expired"
	ExpiresAt  time.Time  `json:"expiresAt"`
	RedeemedAt *time.Time `json:"redeemedAt,omitempty"`
	DeviceID   *string    `json:"deviceId,omitempty"`
	DeviceName *string    `json:"deviceName,omitempty"`
}

// ---------- I/O wrappers ----------

type CreatePairingCodeInput struct{}

type CreatePairingCodeOutput struct {
	Body base.ApiResponse[PairingCodeResponse]
}

type GetPairingCodeStatusInput struct {
	ID string `path:"id" doc:"Pairing code ID"`
}

type GetPairingCodeStatusOutput struct {
	Body base.ApiResponse[PairingCodeStatusResponse]
}

type ListDevicesInput struct{}

type ListDevicesOutput struct {
	Body base.ApiResponse[[]DeviceDTO]
}

type GetDeviceInput struct {
	ID string `path:"id" doc:"Device ID"`
}

type GetDeviceOutput struct {
	Body base.ApiResponse[DeviceDTO]
}

type DeleteDeviceInput struct {
	ID string `path:"id" doc:"Device ID"`
}

type DeleteDeviceOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type RenameDeviceInput struct {
	ID   string                  `path:"id" doc:"Device ID"`
	Body RenameDeviceRequestBody `json:"body"`
}

type RenameDeviceRequestBody struct {
	Name string `json:"name" minLength:"1" maxLength:"128"`
}

type RenameDeviceOutput struct {
	Body base.ApiResponse[DeviceDTO]
}

// ---------- Handler ----------

type DeviceHandler struct {
	deviceService  *services.DeviceService
	pairingService *services.PairingService
	cfg            *config.Config
}

func RegisterDevices(api huma.API, deviceService *services.DeviceService, pairingService *services.PairingService, cfg *config.Config) {
	h := &DeviceHandler{
		deviceService:  deviceService,
		pairingService: pairingService,
		cfg:            cfg,
	}

	huma.Register(api, huma.Operation{
		OperationID: "create-pairing-code",
		Method:      http.MethodPost,
		Path:        "/devices/pairing-codes",
		Summary:     "Create a mobile device pairing code",
		Description: "Generates a short-lived pairing code that a mobile app can redeem to bind itself to your account.",
		Tags:        []string{"Devices"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreatePairingCode)

	huma.Register(api, huma.Operation{
		OperationID: "get-pairing-code-status",
		Method:      http.MethodGet,
		Path:        "/devices/pairing-codes/{id}",
		Summary:     "Poll a pairing code's status",
		Description: "Returns whether the pairing code is still pending, redeemed by a device, or expired.",
		Tags:        []string{"Devices"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetPairingCodeStatus)

	huma.Register(api, huma.Operation{
		OperationID: "list-devices",
		Method:      http.MethodGet,
		Path:        "/devices",
		Summary:     "List paired devices",
		Tags:        []string{"Devices"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListDevices)

	huma.Register(api, huma.Operation{
		OperationID: "get-device",
		Method:      http.MethodGet,
		Path:        "/devices/{id}",
		Summary:     "Get a paired device",
		Tags:        []string{"Devices"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetDevice)

	huma.Register(api, huma.Operation{
		OperationID: "rename-device",
		Method:      http.MethodPatch,
		Path:        "/devices/{id}",
		Summary:     "Rename a paired device",
		Tags:        []string{"Devices"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.RenameDevice)

	huma.Register(api, huma.Operation{
		OperationID: "revoke-device",
		Method:      http.MethodDelete,
		Path:        "/devices/{id}",
		Summary:     "Revoke a paired device",
		Description: "Deletes the device record and its API key; the device will be signed out on its next request.",
		Tags:        []string{"Devices"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.RevokeDevice)
}

// ---------- Implementations ----------

func (h *DeviceHandler) CreatePairingCode(ctx context.Context, _ *CreatePairingCodeInput) (*CreatePairingCodeOutput, error) {
	if h.pairingService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, ok := humamw.GetCurrentUserFromContext(ctx)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	res, err := h.pairingService.IssueCode(ctx, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to issue pairing code: %s", err.Error()))
	}

	serverURL := ""
	insecure := false
	if h.cfg != nil {
		serverURL = strings.TrimSuffix(h.cfg.GetAppURL(), "/")
		insecure = !strings.HasPrefix(strings.ToLower(serverURL), "https://")
	}

	qrPayload := buildQRPayload(serverURL, res.QrToken, insecure)

	return &CreatePairingCodeOutput{
		Body: base.ApiResponse[PairingCodeResponse]{
			Success: true,
			Data: PairingCodeResponse{
				ID:                 res.ID,
				ShortCode:          services.FormatShortCodeDisplay(res.ShortCode),
				ShortCodeRaw:       res.ShortCode,
				QrPayload:          qrPayload,
				ExpiresAt:          res.ExpiresAt,
				ExpiresInSeconds:   int64(time.Until(res.ExpiresAt).Seconds()),
				ServerInsecureFlag: insecure,
			},
		},
	}, nil
}

func (h *DeviceHandler) GetPairingCodeStatus(ctx context.Context, input *GetPairingCodeStatusInput) (*GetPairingCodeStatusOutput, error) {
	if h.pairingService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if _, ok := humamw.GetCurrentUserFromContext(ctx); !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	status, err := h.pairingService.GetSessionStatus(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrPairingCodeNotFound) {
			return nil, huma.Error404NotFound("pairing code not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetPairingCodeStatusOutput{
		Body: base.ApiResponse[PairingCodeStatusResponse]{
			Success: true,
			Data: PairingCodeStatusResponse{
				Status:     status.Status,
				ExpiresAt:  status.ExpiresAt,
				RedeemedAt: status.RedeemedAt,
				DeviceID:   status.DeviceID,
				DeviceName: status.DeviceName,
			},
		},
	}, nil
}

func (h *DeviceHandler) ListDevices(ctx context.Context, _ *ListDevicesInput) (*ListDevicesOutput, error) {
	if h.deviceService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, ok := humamw.GetCurrentUserFromContext(ctx)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	devices, err := h.deviceService.ListForUser(ctx, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	out := make([]DeviceDTO, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceModelToDTO(d))
	}
	return &ListDevicesOutput{
		Body: base.ApiResponse[[]DeviceDTO]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *DeviceHandler) GetDevice(ctx context.Context, input *GetDeviceInput) (*GetDeviceOutput, error) {
	if h.deviceService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, ok := humamw.GetCurrentUserFromContext(ctx)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	d, err := h.deviceService.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrDeviceNotFound) {
			return nil, huma.Error404NotFound("device not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if d.UserID != user.ID {
		return nil, huma.Error404NotFound("device not found")
	}
	return &GetDeviceOutput{
		Body: base.ApiResponse[DeviceDTO]{
			Success: true,
			Data:    deviceModelToDTO(d),
		},
	}, nil
}

func (h *DeviceHandler) RenameDevice(ctx context.Context, input *RenameDeviceInput) (*RenameDeviceOutput, error) {
	if h.deviceService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, ok := humamw.GetCurrentUserFromContext(ctx)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	existing, err := h.deviceService.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrDeviceNotFound) {
			return nil, huma.Error404NotFound("device not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if existing.UserID != user.ID {
		return nil, huma.Error404NotFound("device not found")
	}

	d, err := h.deviceService.Rename(ctx, input.ID, strings.TrimSpace(input.Body.Name))
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &RenameDeviceOutput{
		Body: base.ApiResponse[DeviceDTO]{
			Success: true,
			Data:    deviceModelToDTO(d),
		},
	}, nil
}

func (h *DeviceHandler) RevokeDevice(ctx context.Context, input *DeleteDeviceInput) (*DeleteDeviceOutput, error) {
	if h.deviceService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, ok := humamw.GetCurrentUserFromContext(ctx)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	existing, err := h.deviceService.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrDeviceNotFound) {
			return nil, huma.Error404NotFound("device not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if existing.UserID != user.ID {
		return nil, huma.Error404NotFound("device not found")
	}

	if err := h.deviceService.Revoke(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DeleteDeviceOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "device revoked"},
		},
	}, nil
}

// buildQRPayload returns the deep-link URI embedded in the QR code.
// Format: arcane://pair?u=<urlencoded server>&c=<token>[&insecure=1]
func buildQRPayload(serverURL string, qrToken string, insecure bool) string {
	q := url.Values{}
	q.Set("u", serverURL)
	q.Set("c", qrToken)
	if insecure {
		q.Set("insecure", "1")
	}
	return "arcane://pair?" + q.Encode()
}
