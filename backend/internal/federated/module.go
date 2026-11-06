package federated

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterFederatedCredentials(api huma.API, federatedCredentialService *FederatedCredentialService) {
	h := &FederatedCredentialHandler{
		federatedCredentialService: federatedCredentialService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "exchange-federated-token",
		Method:      http.MethodPost,
		Path:        "/auth/federated/token",
		Summary:     "Exchange a federated subject token",
		Tags:        []string{"Federated Credentials"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessSubjectToken},
	}, middleware.EchoHandler(h.ExchangeToken))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-federated-credentials",
		Method:      http.MethodGet,
		Path:        "/federated/credentials",
		Summary:     "List federated credentials",
		Description: "Get a paginated list of workload identity federation trust rules",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedList, h.ListFederatedCredentials)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-federated-credential",
		Method:      http.MethodPost,
		Path:        "/federated/credentials",
		Summary:     "Create a federated credential",
		Description: "Create a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedCreate, h.CreateFederatedCredential)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-federated-credential",
		Method:      http.MethodGet,
		Path:        "/federated/credentials/{id}",
		Summary:     "Get a federated credential",
		Description: "Get details of a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedRead, h.GetFederatedCredential)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-federated-credential",
		Method:      http.MethodPatch,
		Path:        "/federated/credentials/{id}",
		Summary:     "Update a federated credential",
		Description: "Update a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedUpdate, h.UpdateFederatedCredential)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-federated-credential",
		Method:      http.MethodDelete,
		Path:        "/federated/credentials/{id}",
		Summary:     "Delete a federated credential",
		Description: "Delete a workload identity federation trust rule and its service user",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedDelete, h.DeleteFederatedCredential)
}
