package api

import (
	"bytes"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	humav2 "github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/health"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

func TestWorkspaceRoutesReplaceProjectAndVolumeLegacyRoutes(t *testing.T) {
	api := SetupAPIForSpec()
	paths := api.OpenAPI().Paths

	canonical := []struct {
		path   string
		method string
	}{
		{path: "/environments/{id}/projects/{projectId}/workspace", method: http.MethodGet},
		{path: "/environments/{id}/projects/{projectId}/workspace", method: http.MethodPatch},
		{path: "/environments/{id}/projects/{projectId}/workspace/file", method: http.MethodGet},
		{path: "/environments/{id}/projects/{projectId}/workspace/file/download", method: http.MethodGet},
		{path: "/environments/{id}/volumes/{volumeName}/workspace", method: http.MethodGet},
		{path: "/environments/{id}/volumes/{volumeName}/workspace", method: http.MethodPatch},
		{path: "/environments/{id}/volumes/{volumeName}/workspace/file", method: http.MethodGet},
		{path: "/environments/{id}/volumes/{volumeName}/workspace/file/download", method: http.MethodGet},
		{path: "/environments/{id}/builds/browse", method: http.MethodGet},
	}
	for _, route := range canonical {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			item := paths[route.path]
			require.NotNil(t, item)
			switch route.method {
			case http.MethodGet:
				require.NotNil(t, item.Get)
			case http.MethodPatch:
				require.NotNil(t, item.Patch)
			}
		})
	}

	legacy := []string{
		"/environments/{id}/projects/{projectId}/files",
		"/environments/{id}/projects/{projectId}/file",
		"/environments/{id}/projects/{projectId}/includes",
		"/environments/{id}/volumes/{volumeName}/files",
		"/environments/{id}/volumes/{volumeName}/file",
		"/environments/{id}/volumes/{volumeName}/browse",
		"/environments/{id}/volumes/{volumeName}/browse/content",
		"/environments/{id}/volumes/{volumeName}/browse/download",
		"/environments/{id}/volumes/{volumeName}/browse/upload",
		"/environments/{id}/volumes/{volumeName}/browse/mkdir",
	}
	for _, path := range legacy {
		t.Run("absent "+path, func(t *testing.T) {
			require.Nil(t, paths[path])
		})
	}
}

func TestSchemaRegistry_UsesDescriptiveTypeNames(t *testing.T) {
	registry := SetupAPIForSpec().OpenAPI().Components.Schemas

	require.Equal(t, "#/components/schemas/ImageSummary", registry.Schema(reflect.TypeFor[types.ImageSummary](), true, "").Ref)
	require.Equal(t, "#/components/schemas/EnvSummary", registry.Schema(reflect.TypeFor[types.EnvSummary](), true, "").Ref)
}

func TestSchemaRegistry_PointerMatchesValue(t *testing.T) {
	registry := SetupAPIForSpec().OpenAPI().Components.Schemas

	require.Equal(t,
		registry.Schema(reflect.TypeFor[types.ImageSummary](), true, "").Ref,
		registry.Schema(reflect.TypeFor[*types.ImageSummary](), true, "").Ref,
	)
	require.Equal(t,
		registry.Schema(reflect.TypeFor[types.ApiResponse[types.ContainerStatusCounts]](), true, "").Ref,
		registry.Schema(reflect.TypeFor[*types.ApiResponse[types.ContainerStatusCounts]](), true, "").Ref,
	)
}

func TestSchemaRegistry_AliasesCollidingDockerTypes(t *testing.T) {
	registry := SetupAPIForSpec().OpenAPI().Components.Schemas

	require.Equal(t, "#/components/schemas/DockerSwarmPlacement", registry.Schema(reflect.TypeFor[swarm.Placement](), true, "").Ref)
	require.Equal(t, "#/components/schemas/DockerNetworkConfigReference", registry.Schema(reflect.TypeFor[*network.ConfigReference](), true, "").Ref)
	require.Equal(t, "#/components/schemas/Placement", registry.Schema(reflect.TypeFor[composetypes.Placement](), true, "").Ref)
}

func TestSchemaRegistry_DisambiguatesGenericDomainTypes(t *testing.T) {
	registry := SetupAPIForSpec().OpenAPI().Components.Schemas

	testCases := []struct {
		typ  reflect.Type
		want string
	}{
		{typ: reflect.TypeFor[types.ApiResponse[types.VolumeUsageCounts]](), want: "ApiResponseVolumeUsageCounts"},
		{typ: reflect.TypeFor[types.ApiResponse[types.ImageUsageCounts]](), want: "ApiResponseImageUsageCounts"},
		{typ: reflect.TypeFor[types.ApiResponse[types.ContainerStatusCounts]](), want: "ApiResponseContainerStatusCounts"},
		{typ: reflect.TypeFor[types.ApiResponse[types.ProjectStatusCounts]](), want: "ApiResponseProjectStatusCounts"},
		{typ: reflect.TypeFor[types.Paginated[types.ContainerSummary]](), want: "PaginatedContainerSummary"},
		{typ: reflect.TypeFor[types.PaginatedWithCounts[types.NetworkSummary, types.NetworkUsageCounts]](), want: "PaginatedWithCountsNetworkSummaryNetworkUsageCounts"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			require.Equal(t, "#/components/schemas/"+testCase.want, registry.Schema(testCase.typ, true, "").Ref)
		})
	}
}

func TestSchemaRegistry_RefsResolve(t *testing.T) {
	api := SetupAPIForSpec()
	document, err := api.OpenAPI().MarshalJSON()
	require.NoError(t, err)
	schemas := api.OpenAPI().Components.Schemas.Map()

	matches := regexp.MustCompile(`"\$ref":"#/components/schemas/([^"]+)"`).FindAllSubmatch(document, -1)
	require.NotEmpty(t, matches)

	var unresolved []string
	for _, match := range matches {
		if _, ok := schemas[string(match[1])]; !ok {
			unresolved = append(unresolved, string(match[1]))
		}
	}
	require.Empty(t, unresolved)
}

func TestHandlerDeps_ZeroValue(t *testing.T) {
	var deps HandlerDeps

	require.Nil(t, deps.Auth)
	require.Nil(t, deps.Docker)
}

func TestSetupAPIForSpec_DefaultSecurity(t *testing.T) {
	api := SetupAPIForSpec()

	expectedSecurity := []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}

	require.True(t, reflect.DeepEqual(api.OpenAPI().Security, expectedSecurity),
		"expected default API security %v, got %v", expectedSecurity, api.OpenAPI().Security)
}

func TestSetupAPIForSpecUsesV2JSONFormats(t *testing.T) {
	type response struct {
		Items []string `json:"items"`
		Count int      `json:"count,omitempty"`
	}

	api := SetupAPIForSpec()
	for _, contentType := range []string{"application/json", "application/problem+json"} {
		t.Run(contentType, func(t *testing.T) {
			var body bytes.Buffer
			{
				err := api.Marshal(&body, contentType, response{})
				require.NoError(t, err,
					"marshal %s: %v", contentType, err)
			}
			{

				got, want := body.String(), `{"items":[],"count":0}`
				require.Equal(t, want, got,
					"marshal %s = %s, want %s", contentType, got, want)
			}
		})
	}
}

func TestSetupAPIForSpecPreservesDurationNanoseconds(t *testing.T) {
	type response struct {
		HeartbeatPeriod time.Duration `json:"heartbeatPeriod"`
	}

	api := SetupAPIForSpec()
	var body bytes.Buffer
	{
		err := api.Marshal(&body, "application/json", response{HeartbeatPeriod: 5 * time.Second})
		require.NoError(t, err,
			"marshal duration: %v", err)
	}
	{

		got, want := body.String(), `{"heartbeatPeriod":5000000000}`
		require.Equal(t, want, got,
			"marshal duration = %s, want %s", got, want)
	}
}

func TestSetupAPIForSpec_PublicRoutesOverrideSecurity(t *testing.T) {
	api := SetupAPIForSpec()

	getOperation := func(path, method string) *humav2.Operation {
		pathItem := api.OpenAPI().Paths[path]

		require.NotNil(t, pathItem,
			"expected path %q to be registered", path)

		switch method {
		case "GET":
			return pathItem.Get
		case "POST":
			return pathItem.Post
		case "HEAD":
			return pathItem.Head
		default:
			require.FailNowf(t, "unexpected failure", "unsupported method %q", method)
			return nil
		}
	}

	testCases := []struct {
		path   string
		method string
	}{
		{path: "/app/images/logo", method: "GET"},
		{path: "/app/images/favicon", method: "GET"},
		{path: "/app/images/profile", method: "GET"},
		{path: "/app/images/pwa/{filename}", method: "GET"},
		{path: "/auth/login", method: "POST"},
		{path: "/auth/logout", method: "POST"},
		{path: "/auth/refresh", method: "POST"},
		{path: "/health", method: "GET"},
		{path: "/health", method: "HEAD"},
		{path: "/oidc/status", method: "GET"},
		{path: "/oidc/config", method: "GET"},
		{path: "/oidc/url", method: "POST"},
		{path: "/oidc/callback", method: "POST"},
		{path: "/oidc/device/code", method: "POST"},
		{path: "/oidc/device/token", method: "POST"},
		{path: "/environments/{id}/settings/public", method: "GET"},
		{path: "/environments/pair", method: "POST"},
		{path: "/version", method: "GET"},
		{path: "/app/version", method: "GET"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			operation := getOperation(testCase.path, testCase.method)

			require.NotNil(t, operation,
				"expected operation %s %s to be registered", testCase.method, testCase.path)

			require.NotNil(t, operation.Security,
				"expected operation %s %s to explicitly override security", testCase.method, testCase.path)

			require.Empty(t, operation.Security,
				"expected operation %s %s to be public, got security %v", testCase.method, testCase.path, operation.Security)
		})
	}
}

func TestSetupAPIForSpec_TemplateReadRoutesProtected(t *testing.T) {
	api := SetupAPIForSpec()

	expectedSecurity := []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}

	testCases := []struct {
		path string
	}{
		{path: "/templates"},
		{path: "/templates/all"},
		{path: "/templates/{id}"},
		{path: "/templates/{id}/content"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.path, func(t *testing.T) {
			pathItem := api.OpenAPI().Paths[testCase.path]

			require.False(t, pathItem == nil || pathItem.Get == nil,
				"expected GET %s to be registered", testCase.path)

			require.Nil(t, pathItem.Get.Security,
				"expected GET %s to inherit API security, got explicit security %v", testCase.path, pathItem.Get.Security)

			require.True(t, reflect.DeepEqual(api.OpenAPI().Security, expectedSecurity),
				"expected API security %v, got %v", expectedSecurity, api.OpenAPI().Security)
		})
	}
}

func TestSetupAPIForSpec_DoesNotRegisterPublicCreateEvent(t *testing.T) {
	api := SetupAPIForSpec()

	pathItem := api.OpenAPI().Paths["/events"]

	require.NotNil(t, pathItem,
		"expected /events path to be registered for list events")

	require.Nil(t, pathItem.Post,
		"expected POST /events to be absent from the public API")
}

func TestVariableMaterializationRoutesAreAgentOnly(t *testing.T) {
	managerAPI := SetupAPIForSpec()
	require.NotNil(t, managerAPI.OpenAPI().Paths["/variables"])
	require.Nil(t, managerAPI.OpenAPI().Paths["/environments/{id}/templates/variables"])

	managerMatcher := authz.NewPermissionMatcher()
	managerMatcher.CollectFromHumaAPI(managerAPI)
	_, found := managerMatcher.Lookup("GET", "/templates/variables", nil).Get()
	require.False(t, found)
	_, found = managerMatcher.Lookup("PUT", "/templates/variables", nil).Get()
	require.False(t, found)

	router := echo.New()
	agentAPI := SetupAPI(
		router,
		router.Group("/api"),
		handlerutil.NewActivityAppContext(t.Context()),
		&config.Config{AgentMode: true},
		HandlerDeps{Health: health.New(&health.Service{})},
	)
	require.Nil(t, agentAPI.OpenAPI().Paths["/variables"])
	materialized := agentAPI.OpenAPI().Paths["/environments/{id}/templates/variables"]
	require.NotNil(t, materialized)
	require.NotNil(t, materialized.Get)
	require.NotNil(t, materialized.Put)
}

func TestEasyJoinRoutesDeclareSwarmJoinPermission(t *testing.T) {
	api := SetupAPIForSpec()
	tests := []struct {
		path   string
		method string
	}{
		{path: "/environments/{id}/swarm/join/candidates", method: "GET"},
		{path: "/environments/{id}/swarm/join/environments", method: "POST"},
	}

	matcher := authz.NewPermissionMatcher()
	matcher.CollectFromHumaAPI(api)
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			pathItem := api.OpenAPI().Paths[tt.path]
			require.NotNil(t, pathItem)
			var operation *humav2.Operation
			switch tt.method {
			case "GET":
				operation = pathItem.Get
			case "POST":
				operation = pathItem.Post
			}
			require.NotNil(t, operation)
			require.Equal(t, authz.PermSwarmJoin, operation.Metadata[authz.MetaRequiredPermission])

			suffix := strings.TrimPrefix(tt.path, "/environments/{id}")
			permission, found := matcher.Lookup(tt.method, suffix, nil).Get()
			require.True(t, found)
			require.Equal(t, authz.PermSwarmJoin, permission)
		})
	}
}

// TestEnvScopedOperationsDeclarePermission ensures every authenticated env-scoped operation
// declares the permission the remote-environment proxy enforces before forwarding.
func TestEnvScopedOperationsDeclarePermission(t *testing.T) {
	api := SetupAPIForSpec()
	oapi := api.OpenAPI()

	require.False(t, oapi == nil || oapi.Paths == nil,
		"expected an OpenAPI document with paths")

	var missing []string
	for path, item := range oapi.Paths {
		if !strings.HasPrefix(path, "/environments/{id}/") {
			continue
		}
		// Upload-session routes derive their permission from the {kind} segment at request time.
		if strings.HasPrefix(path, "/environments/{id}/uploads/{kind}") {
			continue
		}
		operations := map[string]*humav2.Operation{
			http.MethodGet:     item.Get,
			http.MethodPost:    item.Post,
			http.MethodPut:     item.Put,
			http.MethodDelete:  item.Delete,
			http.MethodPatch:   item.Patch,
			http.MethodHead:    item.Head,
			http.MethodOptions: item.Options,
		}
		for method, op := range operations {
			if op == nil {
				continue
			}
			if op.Security != nil && len(op.Security) == 0 {
				continue
			}
			if _, isAction := op.Metadata[authz.MetaActionPermissions].(map[string]string); isAction {
				continue
			}
			permission, ok := op.Metadata[authz.MetaRequiredPermission].(string)
			if !ok || permission == "" {
				missing = append(missing, method+" "+path)
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		assert.Failf(t, "unexpected failure", "%d env-scoped operation(s) missing required-permission metadata; register them with middleware.RegisterWithPermission:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}
