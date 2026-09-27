package iconcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolve_SingleVariantFallsBackWithinLevel(t *testing.T) {
	resolved := Resolve(CatalogSelfhst, IconSet{Light: "postgres"})
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/postgres-light.svg", resolved.IconLightURL)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/postgres-dark.svg", resolved.IconDarkURL)
}
