package huma

import (
	"reflect"
	"strings"
	"testing"

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
