package api

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	humav2 "github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/api/handlers"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/di"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	basetypes "github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	envtypes "github.com/getarcaneapp/arcane/types/v2/env"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/labstack/echo/v4"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestCustomSchemaNamer_PrefixesArcaneTypesByPackage(t *testing.T) {
	imageName := customSchemaNamer(reflect.TypeFor[imagetypes.Summary](), "")
	envName := customSchemaNamer(reflect.TypeFor[envtypes.Summary](), "")

	if imageName != "ImageSummary" {
		t.Fatalf("expected ImageSummary, got %q", imageName)
	}
	if envName != "EnvSummary" {
		t.Fatalf("expected EnvSummary, got %q", envName)
	}
	if imageName == envName {
		t.Fatalf("expected unique schema names, got same value %q", imageName)
	}
}

func TestCustomSchemaNamer_PointerMatchesValue(t *testing.T) {
	valueName := customSchemaNamer(reflect.TypeFor[imagetypes.Summary](), "")
	pointerName := customSchemaNamer(reflect.TypeFor[*imagetypes.Summary](), "")

	if valueName != pointerName {
		t.Fatalf("expected pointer and value names to match, got %q and %q", valueName, pointerName)
	}

	genericValueName := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[containertypes.StatusCounts]](), "")
	genericPointerName := customSchemaNamer(reflect.TypeFor[*basetypes.ApiResponse[containertypes.StatusCounts]](), "")
	if genericValueName != genericPointerName {
		t.Fatalf("expected generic pointer and value names to match, got %q and %q", genericValueName, genericPointerName)
	}
}

func TestCustomSchemaNamer_PrefixesDockerTypes(t *testing.T) {
	name := customSchemaNamer(reflect.TypeFor[dockernetwork.Inspect](), "")
	if !strings.HasPrefix(name, "DockerNetwork") {
		t.Fatalf("expected DockerNetwork prefix, got %q", name)
	}
}

func TestCustomSchemaNamer_DisambiguatesGenericDomainTypes(t *testing.T) {
	volumeResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[volumetypes.UsageCounts]](), "")
	imageResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[imagetypes.UsageCounts]](), "")

	if !strings.Contains(volumeResp, "VolumeUsageCounts") {
		t.Fatalf("expected VolumeUsageCounts in name, got %q", volumeResp)
	}
	if !strings.Contains(imageResp, "ImageUsageCounts") {
		t.Fatalf("expected ImageUsageCounts in name, got %q", imageResp)
	}
	if volumeResp == imageResp {
		t.Fatalf("expected unique generic schema names, got %q", volumeResp)
	}

	containerResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[containertypes.StatusCounts]](), "")
	projectResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[projecttypes.StatusCounts]](), "")
	if !strings.Contains(containerResp, "ContainerStatusCounts") {
		t.Fatalf("expected ContainerStatusCounts in name, got %q", containerResp)
	}
	if !strings.Contains(projectResp, "ProjectStatusCounts") {
		t.Fatalf("expected ProjectStatusCounts in name, got %q", projectResp)
	}
	if containerResp == projectResp {
		t.Fatalf("expected unique generic schema names, got %q", containerResp)
	}

	baseResp := customSchemaNamer(reflect.TypeFor[basetypes.ApiResponse[basetypes.MessageResponse]](), "")
	if strings.Contains(baseResp, "BaseApiResponseBase") {
		t.Fatalf("expected base generic argument without redundant package prefix, got %q", baseResp)
	}

	multiArgument := customSchemaNamer(reflect.TypeFor[basetypes.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]](), "")
	if multiArgument != "BasePaginatedWithCountsNetworkSummaryNetworkUsageCounts" {
		t.Fatalf("expected both generic arguments to be package-qualified in order, got %q", multiArgument)
	}

	mixedPackages := customSchemaNamer(reflect.TypeFor[basetypes.PaginatedWithCounts[dockercontainer.Summary, imagetypes.Summary]](), "")
	if mixedPackages != "BasePaginatedWithCountsSummaryImageSummary" {
		t.Fatalf("expected only the Arcane argument to be qualified in place, got %q", mixedPackages)
	}
}

func TestSetupAPIForSpec_DefaultSecurity(t *testing.T) {
	api := SetupAPIForSpec()

	expectedSecurity := []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}

	if !reflect.DeepEqual(api.OpenAPI().Security, expectedSecurity) {
		t.Fatalf("expected default API security %v, got %v", expectedSecurity, api.OpenAPI().Security)
	}
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
			if err := api.Marshal(&body, contentType, response{}); err != nil {
				t.Fatalf("marshal %s: %v", contentType, err)
			}
			if got, want := body.String(), `{"items":[],"count":0}`; got != want {
				t.Fatalf("marshal %s = %s, want %s", contentType, got, want)
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
	if err := api.Marshal(&body, "application/json", response{HeartbeatPeriod: 5 * time.Second}); err != nil {
		t.Fatalf("marshal duration: %v", err)
	}
	if got, want := body.String(), `{"heartbeatPeriod":5000000000}`; got != want {
		t.Fatalf("marshal duration = %s, want %s", got, want)
	}
}

func TestSetupAPIForSpec_PublicRoutesOverrideSecurity(t *testing.T) {
	api := SetupAPIForSpec()

	getOperation := func(path, method string) *humav2.Operation {
		pathItem := api.OpenAPI().Paths[path]
		if pathItem == nil {
			t.Fatalf("expected path %q to be registered", path)
		}

		switch method {
		case "GET":
			return pathItem.Get
		case "POST":
			return pathItem.Post
		case "HEAD":
			return pathItem.Head
		default:
			t.Fatalf("unsupported method %q", method)
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
			if operation == nil {
				t.Fatalf("expected operation %s %s to be registered", testCase.method, testCase.path)
			}
			if operation.Security == nil {
				t.Fatalf("expected operation %s %s to explicitly override security", testCase.method, testCase.path)
			}
			if len(operation.Security) != 0 {
				t.Fatalf("expected operation %s %s to be public, got security %v", testCase.method, testCase.path, operation.Security)
			}
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
			if pathItem == nil || pathItem.Get == nil {
				t.Fatalf("expected GET %s to be registered", testCase.path)
			}
			if pathItem.Get.Security != nil {
				t.Fatalf("expected GET %s to inherit API security, got explicit security %v", testCase.path, pathItem.Get.Security)
			}
			if !reflect.DeepEqual(api.OpenAPI().Security, expectedSecurity) {
				t.Fatalf("expected API security %v, got %v", expectedSecurity, api.OpenAPI().Security)
			}
		})
	}
}

func TestSetupAPIForSpec_DoesNotRegisterPublicCreateEvent(t *testing.T) {
	api := SetupAPIForSpec()

	pathItem := api.OpenAPI().Paths["/events"]
	if pathItem == nil {
		t.Fatal("expected /events path to be registered for list events")
	}
	if pathItem.Post != nil {
		t.Fatal("expected POST /events to be absent from the public API")
	}
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
		handlers.NewActivityAppContext(context.Background()),
		&config.Config{AgentMode: true},
		&di.Services{},
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
