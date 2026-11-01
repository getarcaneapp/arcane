package podman

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/podman/children/kube"
	"getarcane.dev/arcane/v3/internal/podman/children/pods"
	"getarcane.dev/arcane/v3/internal/podman/children/quadlets"
	"getarcane.dev/arcane/v3/internal/podman/children/secrets"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service *Service
		event   *event.EventService
	}
)

const (
	podmanPath = "/environments/{id}/podman"
	podmanTag  = "Podman"
)

func New(service *Service, eventService *event.EventService) *Module {
	return &Module{service: service, event: eventService}
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		m = &Module{service: &Service{}}
	}

	h := &Handler{service: m.service, eventService: m.event}
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "get-podman-status",
		Method:      http.MethodGet,
		Path:        podmanPath + "/status",
		Summary:     "Get Podman status",
		Description: "Report whether the environment runs Podman and which Podman features it supports",
		Tags:        []string{podmanTag},
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermPodmanRead, h.GetStatus)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-info", http.MethodGet, podmanPath+"/info", "Get Podman info", "", podmanTag),
		authz.PermPodmanRead, h.GetInfo)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("run-podman-autoupdate", http.MethodPost, podmanPath+"/autoupdate", "Run Podman auto-update", "Run podman auto-update through the REST API or its systemd unit", podmanTag),
		authz.PermPodmanAutoUpdate, h.RunAutoUpdate)

	pods.RegisterRoutes(api, pods.NewHandler(m.service.pods, h.audit))
	quadlets.RegisterRoutes(api, quadlets.NewHandler(m.service.quadlets, h.audit))
	kube.RegisterRoutes(api, kube.NewHandler(m.service.kube, h.audit))
	secrets.RegisterRoutes(api, secrets.NewHandler(m.service.secrets, h.audit))
}
