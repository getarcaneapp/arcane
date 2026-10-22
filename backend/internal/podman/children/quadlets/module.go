package quadlets

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

const (
	quadletsPath = "/environments/{id}/podman/quadlets"
	quadletsTag  = "Podman"
)

func RegisterRoutes(api huma.API, h *Handler) {
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "list-podman-quadlets",
		Method:      http.MethodGet,
		Path:        quadletsPath,
		Summary:     "List quadlets",
		Description: "List installed Podman quadlets with their systemd units",
		Tags:        []string{quadletsTag},
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermPodmanRead, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("install-podman-quadlet", http.MethodPost, quadletsPath, "Install quadlet", "", quadletsTag),
		authz.PermPodmanQuadlets, h.Install)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-quadlet", http.MethodGet, quadletsPath+"/{name}", "Get quadlet", "Get a quadlet with its unit status and backing containers", quadletsTag),
		authz.PermPodmanRead, h.Get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-quadlet-file", http.MethodGet, quadletsPath+"/{name}/file", "Get quadlet file", "", quadletsTag),
		authz.PermPodmanRead, h.File)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("update-podman-quadlet", http.MethodPut, quadletsPath+"/{name}", "Update quadlet", "Replace a quadlet file, keeping its application", quadletsTag),
		authz.PermPodmanQuadlets, h.Update)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-quadlet", http.MethodDelete, quadletsPath+"/{name}", "Remove quadlet", "", quadletsTag),
		authz.PermPodmanQuadlets, h.Delete)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("podman-quadlet-action", http.MethodPost, quadletsPath+"/{name}", "Run quadlet lifecycle action", "Start, stop, or restart the quadlet's systemd unit", quadletsTag),
		authz.PermPodmanQuadletsLifecycle, h.Action)
}
