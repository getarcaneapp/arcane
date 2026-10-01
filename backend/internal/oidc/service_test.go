package oidc

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateMobileRedirectURI(t *testing.T) {
	ctx := context.Background()
	s := &OidcService{
		config: &config.Config{
			OidcMobileRedirectUris: "arcane-mobile://oidc-callback, arcane-mobile://oauth",
		},
	}

	cases := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"exact match first", "arcane-mobile://oidc-callback", false},
		{"exact match second", "arcane-mobile://oauth", false},
		{"empty rejected", "", true},
		{"scheme-only attack", "arcane-mobile://attacker", true},
		{"different scheme", "https://oidc-callback", true},
		{"trailing-slash mismatch", "arcane-mobile://oidc-callback/", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.ValidateMobileRedirectURI(ctx, tc.uri)

			require.Equal(t, tc.wantErr, err != nil,
				"ValidateMobileRedirectURI(%q): wantErr=%v got err=%v", tc.uri, tc.wantErr, err)
		})
	}
}

func TestGetMobileRedirectAllowlistTrimsWhitespace(t *testing.T) {
	ctx := context.Background()
	s := &OidcService{
		config: &config.Config{
			OidcMobileRedirectUris: "  arcane-mobile://a  ,arcane-mobile://b ,, arcane-mobile://c",
		},
	}

	got := s.GetMobileRedirectAllowlist(ctx)
	want := []string{"arcane-mobile://a", "arcane-mobile://b", "arcane-mobile://c"}
	if len(got) != len(want) {
		require.Len(t, got, len(want),
			"got %d entries (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, w := range want {
		require.Equal(t, w, got[i],
			"entry %d: got %q, want %q", i, got[i], w)
	}
}

func TestGetMobileRedirectAllowlistUsesSettings(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)

	require.NoError(t, err,
		"settings.NewSettingsService: %v", err)
	{

		err := settingsService.UpdateSetting(ctx, "oidcMobileRedirectUris", "arcane-mobile://db-callback")
		require.NoError(t, err,
			"UpdateSetting: %v", err)
	}

	s := &OidcService{
		settingsService: settingsService,
		config: &config.Config{
			OidcMobileRedirectUris: "arcane-mobile://config-callback",
		},
	}
	{

		err := s.ValidateMobileRedirectURI(ctx, "arcane-mobile://db-callback")
		require.NoError(t, err,
			"ValidateMobileRedirectURI db value: %v", err)
	}
	{

		err := s.ValidateMobileRedirectURI(ctx, "arcane-mobile://config-callback")
		require.Error(t, err,
			"ValidateMobileRedirectURI config fallback should fail when DB setting is configured")
	}
}

// Test fixtures shared by this package's tests.

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.Background())) })
	}
	return svc, err
}
