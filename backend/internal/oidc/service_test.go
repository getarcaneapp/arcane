package oidc

import (
	"context"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

func TestValidateMobileRedirectURI(t *testing.T) {
	ctx := t.Context()
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
	ctx := t.Context()
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
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	settingsService, err := newSettingsServiceForTest(t, ctx, db)

	require.NoError(t, err,
		"settings.NewSettingsService: %v", err)
	{

		updateSettingErr := settingsService.UpdateSetting(ctx, "oidcMobileRedirectUris", "arcane-mobile://db-callback")
		require.NoError(t, updateSettingErr,
			"UpdateSetting: %v", updateSettingErr)
	}

	s := &OidcService{
		settingsService: settingsService,
		config: &config.Config{
			OidcMobileRedirectUris: "arcane-mobile://config-callback",
		},
	}
	{

		validateMobileRedirectURIErr := s.ValidateMobileRedirectURI(ctx, "arcane-mobile://db-callback")
		require.NoError(t, validateMobileRedirectURIErr,
			"ValidateMobileRedirectURI db value: %v", validateMobileRedirectURIErr)
	}
	{

		validateMobileRedirectURIErr2 := s.ValidateMobileRedirectURI(ctx, "arcane-mobile://config-callback")
		require.Error(t, validateMobileRedirectURIErr2,
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

func newSettingsServiceForTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}
