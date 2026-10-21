// Package apns owns mobile push device pairing, the relay outbox, and its HTTP routes.
package apns

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service *ApnsService
}

func New(service *ApnsService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *ApnsService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterApns(api, nil)
		return
	}
	RegisterApns(api, m.service)
}

func RegisterApns(api huma.API, service *ApnsService) {
	h := &ApnsHandler{service: service}

	huma.Register(
		api,
		securedApnsOperation(
			"get-apns-status",
			http.MethodGet,
			"/apns/status",
			"Get mobile push status",
			"Whether mobile push is enabled and the caller's registered devices",
		),
		h.Status,
	)
	huma.Register(
		api,
		securedApnsOperation(
			"create-apns-pairing-token",
			http.MethodPost,
			"/apns/pairing",
			"Issue a pairing token",
			"Issue a short-lived signed token the mobile app presents to the push relay",
		),
		h.PairingToken,
	)
	huma.Register(api, securedApnsOperation("register-apns-device", http.MethodPost, "/apns/devices", "Register a mobile device", ""), h.RegisterDevice)
	huma.Register(api, securedApnsOperation("update-apns-device", http.MethodPatch, "/apns/devices/{id}", "Update a mobile device", ""), h.UpdateDevice)
	huma.Register(api, securedApnsOperation("delete-apns-device", http.MethodDelete, "/apns/devices/{id}", "Remove a mobile device", ""), h.DeleteDevice)
	huma.Register(api, securedApnsOperation("apns-device-action", http.MethodPost, "/apns/devices/{id}", "Send a test push", ""), h.TestDevice)
}
