package podman

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/kube"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/pods"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/quadlets"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/secrets"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

const (
	podmanPath = "/environments/{id}/podman"
	podmanTag  = "Podman"
)

type Module struct {
	service *Service
	event   *event.EventService
}

func New(service *Service, eventService *event.EventService) *Module {
	return &Module{service: service, event: eventService}
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		m = &Module{service: &Service{}}
	}

	h := &Handler{service: m.service, eventService: m.event}
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-status", http.MethodGet, podmanPath+"/status", "Get Podman status", "Report whether the environment runs Podman and which Podman features it supports", podmanTag),
		authz.PermPodmanRead, h.GetStatus)
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
