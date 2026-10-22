package network

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterNetworks registers network endpoints.
func RegisterNetworks(api huma.API, networkSvc *NetworkService, dockerSvc *docker.DockerClientService, activitySvc *activity.ActivityService, appCtx handlerutil.ActivityAppContext) {
	h := &NetworkHandler{
		networkService:  networkSvc,
		dockerService:   dockerSvc,
		activityService: activitySvc,
		appCtx:          appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-networks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks",
		Summary:     "List networks",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermNetworksList, h.ListNetworks)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "network-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/counts",
		Summary:     "Network counts",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNetworksList, h.GetNetworkCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-network",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks",
		Summary:     "Create network",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNetworksCreate, h.CreateNetwork)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-network-topology",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/topology",
		Summary:     "Get network topology",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNetworksRead, h.GetNetworkTopology)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-network",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Get network",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNetworksRead, h.GetNetwork)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-network",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Delete network",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNetworksDelete, h.DeleteNetwork)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-networks",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks/prune",
		Summary:     "Prune networks",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNetworksPrune, h.PruneNetworks)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "network-container-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Connect or disconnect a container",
		Description: "Connect a container to the network or disconnect it. Aliases and static addresses apply to connect; force applies to disconnect.",
		Tags:        []string{"Networks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"connect":    authz.PermNetworksConnect,
		"disconnect": authz.PermNetworksDisconnect,
	}, h.RunNetworkAction)
}
