package secrets

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

const (
	secretsPath = "/environments/{id}/podman/secrets"
	secretsTag  = "Podman"
)

func RegisterRoutes(api huma.API, h *Handler) {
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("list-podman-secrets", http.MethodGet, secretsPath, "List Podman secrets", "", secretsTag),
		authz.PermPodmanRead, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-podman-secret", http.MethodPost, secretsPath, "Create Podman secret", "", secretsTag),
		authz.PermPodmanSecrets, h.Create)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-secret", http.MethodDelete, secretsPath+"/{secretId}", "Remove Podman secret", "", secretsTag),
		authz.PermPodmanSecrets, h.Delete)
}
