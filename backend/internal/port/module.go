package port

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

func RegisterPorts(api huma.API, portSvc *PortService) {
	h := &PortHandler{portService: portSvc}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-ports",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/ports",
		Summary:     "List port mappings",
		Tags:        []string{"Ports"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermContainersList, h.ListPorts)
}
