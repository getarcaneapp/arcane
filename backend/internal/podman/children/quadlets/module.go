package quadlets

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

const (
	quadletsPath = "/agents/{id}/podman/quadlets"
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
	}, authz.PermPodmanQuadletsList, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("install-podman-quadlet", http.MethodPost, quadletsPath, "Install quadlet", "", quadletsTag),
		authz.PermPodmanQuadletsCreate, h.Install)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-quadlet", http.MethodGet, quadletsPath+"/{name}", "Get quadlet", "Get a quadlet with its unit status and backing containers", quadletsTag),
		authz.PermPodmanQuadletsRead, h.Get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-quadlet-file", http.MethodGet, quadletsPath+"/{name}/file", "Get quadlet file", "", quadletsTag),
		authz.PermPodmanQuadletsRead, h.File)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("update-podman-quadlet", http.MethodPut, quadletsPath+"/{name}", "Update quadlet", "Replace a quadlet file, keeping its application", quadletsTag),
		authz.PermPodmanQuadletsUpdate, h.Update)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-quadlet", http.MethodDelete, quadletsPath+"/{name}", "Remove quadlet", "", quadletsTag),
		authz.PermPodmanQuadletsDelete, h.Delete)
	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "podman-quadlet-action",
		Method:      http.MethodPost,
		Path:        quadletsPath + "/{name}",
		Summary:     "Run quadlet lifecycle action",
		Description: "Start, stop, or restart the quadlet's systemd unit",
		Tags:        []string{quadletsTag},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"start":   authz.PermPodmanQuadletsStart,
		"stop":    authz.PermPodmanQuadletsStop,
		"restart": authz.PermPodmanQuadletsRestart,
	}, h.Action)
}
