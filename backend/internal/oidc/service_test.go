package oidc

import (
	"context"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

func TestValidateMobileRedirectURI(t *testing.T) {
	ctx := t.Context()
	settingsService, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	require.NoError(t, settingsService.UpdateSetting(ctx, "oidcMobileRedirectUris", "arcane-mobile://oidc-callback, arcane-mobile://oauth"))
	s := &OidcService{settingsService: settingsService}

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
			validateErr := s.ValidateMobileRedirectURI(ctx, tc.uri)

			require.Equal(t, tc.wantErr, validateErr != nil,
				"ValidateMobileRedirectURI(%q): wantErr=%v got err=%v", tc.uri, tc.wantErr, validateErr)
		})
	}
}

func TestGetMobileRedirectAllowlistTrimsWhitespace(t *testing.T) {
	ctx := t.Context()
	settingsService, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	require.NoError(t, settingsService.UpdateSetting(ctx, "oidcMobileRedirectUris", "  arcane-mobile://a  ,arcane-mobile://b ,, arcane-mobile://c"))
	s := &OidcService{settingsService: settingsService}

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

	s := &OidcService{settingsService: settingsService}
	{

		validateMobileRedirectURIErr := s.ValidateMobileRedirectURI(ctx, "arcane-mobile://db-callback")
		require.NoError(t, validateMobileRedirectURIErr,
			"ValidateMobileRedirectURI db value: %v", validateMobileRedirectURIErr)
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
	svc, err := settings.NewSettingsService(ctx, db, &config.Config{}, settings.Env{})
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func TestGetOidcRedirectURLBasePath(t *testing.T) {
	tests := []struct {
		name   string
		appURL string
		origin string
		want   string
	}{
		{name: "root, from app URL", appURL: "https://example.com", want: "https://example.com/auth/oidc/callback"},
		{name: "root, from origin", appURL: "https://example.com", origin: "https://example.com", want: "https://example.com/auth/oidc/callback"},
		{name: "base path, from app URL", appURL: "https://example.com/arcane", want: "https://example.com/arcane/auth/oidc/callback"},
		{name: "base path, app URL with trailing slash", appURL: "https://example.com/arcane/", want: "https://example.com/arcane/auth/oidc/callback"},
		{name: "base path, from origin header", appURL: "https://example.com/arcane", origin: "https://example.com", want: "https://example.com/arcane/auth/oidc/callback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &OidcService{config: &config.Config{AppUrl: tt.appURL}}
			require.Equal(t, tt.want, s.GetOidcRedirectURL(tt.origin))
		})
	}
}
