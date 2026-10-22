package secrets

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

const (
	secretsPath = "/environments/{id}/podman/secrets"
	secretsTag  = "Podman"
)

func RegisterRoutes(api huma.API, h *Handler) {
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "list-podman-secrets",
		Method:      http.MethodGet,
		Path:        secretsPath,
		Summary:     "List Podman secrets",
		Tags:        []string{secretsTag},
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermPodmanRead, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-podman-secret", http.MethodPost, secretsPath, "Create Podman secret", "", secretsTag),
		authz.PermPodmanSecrets, h.Create)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-secret", http.MethodGet, secretsPath+"/{secretId}", "Inspect Podman secret", "Returns secret metadata; the value is never included", secretsTag),
		authz.PermPodmanRead, h.Get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("update-podman-secret", http.MethodPut, secretsPath+"/{secretId}", "Replace Podman secret", "Replace the value, driver, and labels of an existing secret", secretsTag),
		authz.PermPodmanSecrets, h.Update)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-secret", http.MethodDelete, secretsPath+"/{secretId}", "Remove Podman secret", "", secretsTag),
		authz.PermPodmanSecrets, h.Delete)
}
