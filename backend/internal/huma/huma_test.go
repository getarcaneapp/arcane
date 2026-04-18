package huma

import (
	"reflect"
	"strings"
	"testing"

	humav2 "github.com/danielgtaylor/huma/v2"
	basetypes "github.com/getarcaneapp/arcane/types/base"
	envtypes "github.com/getarcaneapp/arcane/types/env"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	volumetypes "github.com/getarcaneapp/arcane/types/volume"
	dockernetwork "github.com/moby/moby/api/types/network"
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
}

func TestCustomSchemaNamer_PrefixesDockerTypes(t *testing.T) {
	name := customSchemaNamer(reflect.TypeFor[dockernetwork.Inspect](), "")
	if !strings.HasPrefix(name, "DockerNetwork") {
		t.Fatalf("expected DockerNetwork prefix, got %q", name)
	}
}

func TestCustomSchemaNamer_DisambiguatesGenericUsageCounts(t *testing.T) {
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
		{path: "/fonts/sans", method: "GET"},
		{path: "/fonts/mono", method: "GET"},
		{path: "/fonts/serif", method: "GET"},
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
