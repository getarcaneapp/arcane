package api

import (
	"bytes"
	"context"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	humav2 "github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	basetypes "github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	envtypes "github.com/getarcaneapp/arcane/types/v2/env"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/labstack/echo/v5"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceRoutesReplaceProjectAndVolumeLegacyRoutes(t *testing.T) {
	api := SetupAPIForSpec()
	paths := api.OpenAPI().Paths

	canonical := []struct {
		path   string
		method string
	}{
		{path: "/environments/{id}/projects/{projectId}/workspace", method: http.MethodGet},
		{path: "/environments/{id}/projects/{projectId}/workspace", method: http.MethodPut},
		{path: "/environments/{id}/projects/{projectId}/workspace/file", method: http.MethodGet},
		{path: "/environments/{id}/projects/{projectId}/workspace/file/download", method: http.MethodGet},
		{path: "/environments/{id}/volumes/{volumeName}/workspace", method: http.MethodGet},
		{path: "/environments/{id}/volumes/{volumeName}/workspace", method: http.MethodPut},
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
			case http.MethodPut:
				require.NotNil(t, item.Put)
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

func TestCustomSchemaNamer_PrefixesArcaneTypesByPackage(t *testing.T) {
	imageName := customSchemaNamer(reflect.TypeFor[imagetypes.Summary](), "")
	envName := customSchemaNamer(reflect.TypeFor[envtypes.Summary](), "")

	require.Equal(t, "ImageSummary", imageName,
		"expected ImageSummary, got %q", imageName)

	require.Equal(t, "EnvSummary", envName,
		"expected EnvSummary, got %q", envName)

}

func TestCustomSchemaNamer_PointerMatchesValue(t *testing.T) {
	valueName := customSchemaNamer(reflect.TypeFor[imagetypes.Summary](), "")
	pointerName := customSchemaNamer(reflect.TypeFor[*imagetypes.Summary](), "")

	require.Equal(t, pointerName, valueName,
		"expected pointer and value names to match, got %q and %q", valueName, pointerName)

	genericValueName := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[containertypes.StatusCounts]](), "")
	genericPointerName := customSchemaNamer(reflect.TypeFor[*basetypes.ApiResponse[containertypes.StatusCounts]](), "")

	require.Equal(t, genericPointerName, genericValueName,
		"expected generic pointer and value names to match, got %q and %q", genericValueName, genericPointerName)

}

func TestCustomSchemaNamer_PrefixesDockerTypes(t *testing.T) {
	name := customSchemaNamer(reflect.TypeFor[dockernetwork.Inspect](), "")

	require.True(t, strings.HasPrefix(name, "DockerNetwork"),
		"expected DockerNetwork prefix, got %q", name)

}

func TestCustomSchemaNamer_DisambiguatesGenericDomainTypes(t *testing.T) {
	volumeResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[volumetypes.UsageCounts]](), "")
	imageResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[imagetypes.UsageCounts]](), "")

	require.Contains(t, volumeResp, "VolumeUsageCounts",
		"expected VolumeUsageCounts in name, got %q", volumeResp)

	require.Contains(t, imageResp, "ImageUsageCounts",
		"expected ImageUsageCounts in name, got %q", imageResp)

	require.NotEqual(t, imageResp, volumeResp,
		"expected unique generic schema names, got %q", volumeResp)

	containerResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[containertypes.StatusCounts]](), "")
	projectResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[projecttypes.StatusCounts]](), "")

	require.Contains(t, containerResp, "ContainerStatusCounts",
		"expected ContainerStatusCounts in name, got %q", containerResp)

	require.Contains(t, projectResp, "ProjectStatusCounts",
		"expected ProjectStatusCounts in name, got %q", projectResp)

	require.NotEqual(t, projectResp, containerResp,
		"expected unique generic schema names, got %q", containerResp)

	baseResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[basetypes.MessageResponse]](), "")

	require.NotContains(t, baseResp, "BaseApiResponseBase",
		"expected base generic argument without redundant package prefix, got %q", baseResp)

	multiArgument := customSchemaNamer(reflect.TypeFor[basetypes.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]](), "")

	require.Equal(t, "BasePaginatedWithCountsNetworkSummaryNetworkUsageCounts", multiArgument,
		"expected both generic arguments to be package-qualified in order, got %q", multiArgument)

	mixedPackages := customSchemaNamer(reflect.TypeFor[basetypes.PaginatedWithCounts[dockercontainer.Summary, imagetypes.Summary]](), "")

	require.Equal(t, "BasePaginatedWithCountsSummaryImageSummary", mixedPackages,
		"expected only the Arcane argument to be qualified in place, got %q", mixedPackages)

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
		{path: "/app-images/logo", method: "GET"},
		{path: "/app-images/logo-email", method: "GET"},
		{path: "/app-images/favicon", method: "GET"},
		{path: "/app-images/profile", method: "GET"},
		{path: "/app-images/pwa/{filename}", method: "GET"},
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
		{path: "/app-version", method: "GET"},
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
	_, found := managerMatcher.Lookup("GET", "/templates/variables").Get()
	require.False(t, found)
	_, found = managerMatcher.Lookup("PUT", "/templates/variables").Get()
	require.False(t, found)

	router := echo.New()
	agentAPI := SetupAPI(
		router,
		router.Group("/api"),
		handlerutil.NewActivityAppContext(context.Background()),
		&config.Config{AgentMode: true},
		HandlerDeps{},
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
		{path: "/environments/{id}/swarm/join-candidates", method: "GET"},
		{path: "/environments/{id}/swarm/join-environments", method: "POST"},
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
			permission, found := matcher.Lookup(tt.method, suffix).Get()
			require.True(t, found)
			require.Equal(t, authz.PermSwarmJoin, permission)
		})
	}
}

// TestEnvScopedOperationsDeclarePermission guards the remote-environment proxy
// authorization model. Every authenticated environment-scoped operation must
// declare the permission that the proxy enforces before forwarding it.
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
		for method, op := range envScopedTestOperationsInternal(item) {
			if op == nil {
				continue
			}
			if op.Security != nil && len(op.Security) == 0 {
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

func envScopedTestOperationsInternal(item *humav2.PathItem) map[string]*humav2.Operation {
	return map[string]*humav2.Operation{
		http.MethodGet:     item.Get,
		http.MethodPost:    item.Post,
		http.MethodPut:     item.Put,
		http.MethodDelete:  item.Delete,
		http.MethodPatch:   item.Patch,
		http.MethodHead:    item.Head,
		http.MethodOptions: item.Options,
	}
}
