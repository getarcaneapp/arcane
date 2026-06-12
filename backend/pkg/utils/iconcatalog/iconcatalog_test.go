package iconcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirstNonEmpty_LevelIsolation(t *testing.T) {
	containerSet := IconSet{Light: "postgres"}
	projectSet := IconSet{Light: "umami", Dark: "umami"}

	require.Equal(t, containerSet, FirstNonEmpty(containerSet, IconSet{}, projectSet))
	require.Equal(t, projectSet, FirstNonEmpty(IconSet{}, IconSet{}, projectSet))
}

func TestResolve_SingleVariantFallsBackWithinLevel(t *testing.T) {
	resolved := Resolve(CatalogSelfhst, IconSet{Light: "postgres"})
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/postgres-light.svg", resolved.IconLightURL)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/postgres-dark.svg", resolved.IconDarkURL)
}
