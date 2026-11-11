package pods

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

const (
	podsPath = "/agents/{id}/podman/pods"
	podsTag  = "Podman"
)

func RegisterRoutes(api huma.API, h *Handler) {
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "list-podman-pods",
		Method:      http.MethodGet,
		Path:        podsPath,
		Summary:     "List pods",
		Description: "List Podman pods with their member containers",
		Tags:        []string{podsTag},
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermPodmanPodsList, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-podman-pod", http.MethodPost, podsPath, "Create pod", "", podsTag),
		authz.PermPodmanPodsCreate, h.Create)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("prune-podman-pods", http.MethodPost, podsPath+"/prune", "Prune pods", "Remove every stopped pod", podsTag),
		authz.PermPodmanPodsPrune, h.Prune)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-pod-stats", http.MethodGet, podsPath+"/stats", "Get pod stats", "", podsTag),
		authz.PermPodmanPodsRead, h.Stats)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-pod", http.MethodGet, podsPath+"/{podId}", "Inspect pod", "", podsTag),
		authz.PermPodmanPodsRead, h.Get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-pod-top", http.MethodGet, podsPath+"/{podId}/top", "List pod processes", "", podsTag),
		authz.PermPodmanPodsRead, h.Top)
	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "podman-pod-action",
		Method:      http.MethodPost,
		Path:        podsPath + "/{podId}",
		Summary:     "Run pod lifecycle action",
		Description: "Start, stop, restart, pause, unpause, or kill a pod",
		Tags:        []string{podsTag},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"start":   authz.PermPodmanPodsStart,
		"stop":    authz.PermPodmanPodsStop,
		"restart": authz.PermPodmanPodsRestart,
		"pause":   authz.PermPodmanPodsPause,
		"unpause": authz.PermPodmanPodsPause,
		"kill":    authz.PermPodmanPodsKill,
	}, h.Action)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-pod", http.MethodDelete, podsPath+"/{podId}", "Remove pod", "", podsTag),
		authz.PermPodmanPodsDelete, h.Delete)
}
