package secrets

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
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
	}, authz.PermPodmanSecretsList, h.List)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-podman-secret", http.MethodPost, secretsPath, "Create Podman secret", "", secretsTag),
		authz.PermPodmanSecretsCreate, h.Create)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-podman-secret", http.MethodGet, secretsPath+"/{secretId}", "Inspect Podman secret", "Returns secret metadata; the value is never included", secretsTag),
		authz.PermPodmanSecretsRead, h.Get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("update-podman-secret", http.MethodPut, secretsPath+"/{secretId}", "Replace Podman secret", "Replace the value, driver, and labels of an existing secret", secretsTag),
		authz.PermPodmanSecretsUpdate, h.Update)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-podman-secret", http.MethodDelete, secretsPath+"/{secretId}", "Remove Podman secret", "", secretsTag),
		authz.PermPodmanSecretsDelete, h.Delete)
}
