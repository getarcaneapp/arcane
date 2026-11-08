package dhi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestInfo(t *testing.T) {
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)

	t.Run("returns nil without hardened labels", func(t *testing.T) {
		assert.Nil(t, Info(map[string]string{"org.opencontainers.image.title": "nginx"}, now))
		assert.Nil(t, Info(nil, now))
	})

	t.Run("parses hardened labels", func(t *testing.T) {
		info := Info(map[string]string{
			"com.docker.dhi.chain-id":         "sha256:58b14bc4",
			"com.docker.dhi.compliance":       "cis, fips",
			"com.docker.dhi.created":          "2026-11-04T07:58:28Z",
			"com.docker.dhi.date.end-of-life": "2026-11-10",
			"com.docker.dhi.date.release":     "2023-11-14",
			"com.docker.dhi.definition":       "image/aspnetcore/alpine-3.24/8",
			"com.docker.dhi.distro":           "alpine-3.24",
			"com.docker.dhi.entitlement":      "public",
			"com.docker.dhi.flavor":           "",
			"com.docker.dhi.name":             "dhi/aspnetcore",
			"com.docker.dhi.package-manager":  "",
			"com.docker.dhi.shell":            "",
			"com.docker.dhi.title":            "ASP.NET Core 8.x",
			"com.docker.dhi.url":              "https://dhi.io/catalog/aspnetcore",
			"com.docker.dhi.variant":          "runtime",
			"com.docker.dhi.version":          "8.0.33-alpine3.24",
		}, now)

		require.NotNil(t, info)
		assert.Equal(t, new(""), info.Shell)
		assert.Equal(t, new(""), info.PackageManager)
		assert.Equal(t, types.HardenedImageEndingSoon, info.SupportStatus)
		assert.Equal(t, []string{"cis", "fips"}, info.Compliance)
		assert.Equal(t, "dhi/aspnetcore", info.Name)
		assert.Equal(t, "ASP.NET Core 8.x", info.Title)
		assert.Equal(t, "8.0.33-alpine3.24", info.Version)
		assert.Equal(t, "runtime", info.Variant)
		assert.Equal(t, "2026-11-04T07:58:28Z", info.Created)
		assert.Equal(t, "2023-11-14", info.ReleaseDate)
	})

	t.Run("leaves missing shell and package manager unknown", func(t *testing.T) {
		info := Info(map[string]string{"com.docker.dhi.name": "dhi/node"}, now)

		require.NotNil(t, info)
		assert.Nil(t, info.Shell)
		assert.Nil(t, info.PackageManager)
		assert.Empty(t, info.Compliance)
	})

	tests := []struct {
		name      string
		endOfLife string
		status    types.HardenedImageSupportStatus
	}{
		{name: "past end of life", endOfLife: "2026-10-19", status: types.HardenedImageEndOfLife},
		{name: "end of life today", endOfLife: "2026-10-20", status: types.HardenedImageEndingSoon},
		{name: "at the ending soon threshold", endOfLife: "2026-11-19", status: types.HardenedImageEndingSoon},
		{name: "supported", endOfLife: "2026-11-20", status: types.HardenedImageSupported},
		{name: "missing end of life", endOfLife: "", status: types.HardenedImageSupportUnknown},
		{name: "invalid end of life", endOfLife: "soon", status: types.HardenedImageSupportUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := Info(map[string]string{"com.docker.dhi.name": "dhi/node", "com.docker.dhi.date.end-of-life": tt.endOfLife}, now)

			require.NotNil(t, info)
			assert.Equal(t, tt.status, info.SupportStatus)
		})
	}
}
