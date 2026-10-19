package pods

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

const (
	podsPath = "/environments/{id}/podman/pods"
	podsTag  = "Podman"
)

func RegisterRoutes(api huma.API, h *Handler) {
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("list-podman-pods", http.MethodGet, podsPath, "List pods", "List Podman pods with their member containers", podsTag),
		authz.PermPodmanRead, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-podman-pod", http.MethodPost, podsPath, "Create pod", "", podsTag),
		authz.PermPodmanPods, h.Create)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("prune-podman-pods", http.MethodPost, podsPath+"/prune", "Prune pods", "Remove every stopped pod", podsTag),
		authz.PermPodmanPods, h.Prune)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-pod-stats", http.MethodGet, podsPath+"/stats", "Get pod stats", "", podsTag),
		authz.PermPodmanRead, h.Stats)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-pod", http.MethodGet, podsPath+"/{podId}", "Inspect pod", "", podsTag),
		authz.PermPodmanRead, h.Get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-pod-top", http.MethodGet, podsPath+"/{podId}/top", "List pod processes", "", podsTag),
		authz.PermPodmanRead, h.Top)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("podman-pod-action", http.MethodPost, podsPath+"/{podId}/{action}", "Run pod lifecycle action", "Start, stop, restart, pause, unpause, or kill a pod", podsTag),
		authz.PermPodmanPods, h.Action)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-pod", http.MethodDelete, podsPath+"/{podId}", "Remove pod", "", podsTag),
		authz.PermPodmanPods, h.Delete)
}
