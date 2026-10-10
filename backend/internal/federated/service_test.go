package federated

import (
	"context"
	"crypto/mldsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/federated"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
)

type federatedTestIssuer struct {
	IssuerURL string
	private   *mldsa.PrivateKey
	keyID     string
	server    *httptest.Server
}

func newFederatedTestIssuer(t *testing.T) *federatedTestIssuer {
	t.Helper()

	privateKey, err := mldsa.GenerateKey(mldsa.MLDSA87())
	require.NoError(t, err)

	issuer := &federatedTestIssuer{
		private: privateKey,
		keyID:   "federated-test-key",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer.IssuerURL,
			"jwks_uri":                              issuer.IssuerURL + "/jwks",
			"authorization_endpoint":                issuer.IssuerURL + "/authorize",
			"token_endpoint":                        issuer.IssuerURL + "/token",
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{jwa.MLDSA87().String()},
		})) {
			return
		}
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := privateKey.PublicKey()
		if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "AKP",
					"use": "sig",
					"kid": issuer.keyID,
					"alg": jwa.MLDSA87().String(),
					"pub": base64.RawURLEncoding.EncodeToString(pub.Bytes()),
				},
			},
		})) {
			return
		}
	})

	issuer.server = httptest.NewServer(mux)
	issuer.IssuerURL = issuer.server.URL
	t.Cleanup(issuer.server.Close)

	return issuer
}

func (i *federatedTestIssuer) token(t *testing.T, subject string, audience []string) string {
	t.Helper()

	now := time.Now()
	token, err := jwt.NewBuilder().
		Issuer(i.IssuerURL).
		Subject(subject).
		Audience(audience).
		IssuedAt(now).
		NotBefore(now.Add(-time.Minute)).
		Expiration(now.Add(5 * time.Minute)).
		Build()
	require.NoError(t, err)
	headers := jws.NewHeaders()
	require.NoError(t, headers.Set(jws.KeyIDKey, i.keyID))
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.MLDSA87(), i.private, jws.WithProtectedHeaders(headers)))
	require.NoError(t, err)
	return string(signed)
}

func setupFederatedCredentialServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&user.User{},
		&session.UserSession{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&FederatedCredential{},
		&FederatedTokenReplay{},
		&event.Event{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return &database.DB{DB: db}
}

func setupFederatedCredentialService(t *testing.T, issuer *federatedTestIssuer) (*FederatedCredentialService, *auth.AuthService, *database.DB) {
	t.Helper()

	ctx := t.Context()
	db := setupFederatedCredentialServiceTestDB(t)
	roleSvc := role.NewRoleService(db)
	userSvc := user.NewUserService(db, roleSvc, session.RevokeAllUserSessionsExceptInDB)
	sessionSvc := session.NewSessionService(db)
	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	eventSvc := event.NewEventService(db, &config.Config{}, nil)
	signingKey, err := mldsa.GenerateKey(mldsa.MLDSA87())
	require.NoError(t, err)
	authSvc := auth.NewAuthService(userSvc, settingsSvc, eventSvc, sessionSvc, roleSvc, &config.Config{
		JWTRefreshExpiry: 24 * time.Hour,
	}).WithSigningKey(signingKey)

	keySetManager := oidcjwk.NewKeySetManager(t.Context())
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		require.NoError(t, keySetManager.Shutdown(shutdownCtx))
	})
	service := NewFederatedCredentialService(db, authSvc, userSvc, settingsSvc, eventSvc, issuer.server.Client(), keySetManager, roleSvc)

	viewerRole := role.Role{
		ID:          "role-federated-viewer",
		Name:        "Federated Viewer",
		Permissions: database.StringSlice{authz.PermProjectsList},
	}
	require.NoError(t, db.WithContext(ctx).Create(&viewerRole).Error)

	serviceUser := user.User{
		ID:               "user-federated-service",
		Username:         "svc-federated-demo",
		IsServiceAccount: true,
	}
	require.NoError(t, db.WithContext(ctx).Create(&serviceUser).Error)
	require.NoError(t, db.WithContext(ctx).Create(&role.UserRoleAssignment{
		UserID: serviceUser.ID,
		RoleID: viewerRole.ID,
	}).Error)

	credential := FederatedCredential{
		ID:              "cred-github-actions",
		Name:            "GitHub Actions",
		Enabled:         true,
		IssuerURL:       issuer.IssuerURL,
		Audiences:       database.StringSlice{"arcane-ci"},
		SubjectClaim:    "sub",
		SubjectMatch:    "repo:getarcaneapp/arcane:*",
		MatchType:       federated.MatchTypeGlob,
		RoleID:          viewerRole.ID,
		IdentityUserID:  serviceUser.ID,
		TokenTTLSeconds: 900,
	}
	require.NoError(t, db.WithContext(ctx).Create(&credential).Error)

	return service, authSvc, db
}

func TestFederatedCredentialServiceExchangeToken(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	service, authSvc, db := setupFederatedCredentialService(t, issuer)
	ctx := t.Context()

	tests := []struct {
		name      string
		token     string
		wantError func(error) bool
	}{
		{
			name:  "issues an Arcane bearer token for a matching issuer audience and subject",
			token: issuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		},
		{
			name:  "rejects audience mismatch",
			token: issuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"other-audience"}),
			wantError: func(err error) bool {
				return errors.Is(err, common.ErrFederatedCredentialInvalidGrant)
			},
		},
		{
			name:  "rejects subject mismatch",
			token: issuer.token(t, "repo:other/repo:ref:refs/heads/main", []string{"arcane-ci"}),
			wantError: func(err error) bool {
				return errors.Is(err, common.ErrFederatedCredentialInvalidGrant)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.ExchangeToken(ctx, federated.TokenExchangeRequest{
				GrantType:        federated.TokenExchangeGrantType,
				SubjectToken:     tt.token,
				SubjectTokenType: federated.SubjectTokenTypeJWT,
				Audience:         "https://arcane.example.com",
			})
			if tt.wantError != nil {
				require.Error(t, err)
				require.True(t, tt.wantError(err), "unexpected error: %v", err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "Bearer", resp.TokenType)
			require.Equal(t, federated.IssuedTokenTypeAccessToken, resp.IssuedTokenType)
			require.Positive(t, resp.ExpiresIn)
			require.NotEmpty(t, resp.AccessToken)

			localUser, sessionID, err := authSvc.VerifyToken(ctx, resp.AccessToken)
			require.NoError(t, err)
			require.Equal(t, "user-federated-service", localUser.ID)

			var userSession session.UserSession
			require.NoError(t, db.WithContext(ctx).Where("id = ?", sessionID).First(&userSession).Error)
			require.Equal(t, session.UserSessionSourceFederated, userSession.Source)
			require.NotNil(t, userSession.FederatedCredentialID)
			require.Equal(t, "cred-github-actions", *userSession.FederatedCredentialID)
		})
	}
}

func TestFederatedCredentialServiceExchangeTokenRejectsIssuerWithoutCredential(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	otherIssuer := newFederatedTestIssuer(t)
	service, _, _ := setupFederatedCredentialService(t, issuer)

	resp, err := service.ExchangeToken(t.Context(), federated.TokenExchangeRequest{
		GrantType:        federated.TokenExchangeGrantType,
		SubjectToken:     otherIssuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federated.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrFederatedCredentialInvalidGrant, "unexpected error: %v", err)
	require.Nil(t, resp)
}

func TestFederatedCredentialServiceExchangeTokenDoesNotRequireGlobalFeatureFlag(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	service, _, _ := setupFederatedCredentialService(t, issuer)
	service.settingsService = nil

	resp, err := service.ExchangeToken(t.Context(), federated.TokenExchangeRequest{
		GrantType:        federated.TokenExchangeGrantType,
		SubjectToken:     issuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federated.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.AccessToken)
}

func TestFederatedCredentialServiceExchangeTokenRejectsExpiredCredential(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	service, _, db := setupFederatedCredentialService(t, issuer)
	expiredAt := time.Now().Add(-time.Minute)
	require.NoError(t, db.WithContext(t.Context()).
		Model(&FederatedCredential{}).
		Where("id = ?", "cred-github-actions").
		Update("expires_at", expiredAt).Error)

	resp, err := service.ExchangeToken(t.Context(), federated.TokenExchangeRequest{
		GrantType:        federated.TokenExchangeGrantType,
		SubjectToken:     issuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federated.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrFederatedCredentialInvalidGrant, "unexpected error: %v", err)
	require.Nil(t, resp)
}

func TestFederatedCredentialServiceUpdateDisableRevokesIssuedSessions(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	service, authSvc, _ := setupFederatedCredentialService(t, issuer)
	ctx := t.Context()

	resp, err := service.ExchangeToken(ctx, federated.TokenExchangeRequest{
		GrantType:        federated.TokenExchangeGrantType,
		SubjectToken:     issuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federated.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = service.Update(ctx, "admin-user", "cred-github-actions", federated.UpdateFederatedCredential{
		Enabled: new(false),
	})
	require.NoError(t, err)

	_, _, err = authSvc.VerifyToken(ctx, resp.AccessToken)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrSessionRevoked, "unexpected error: %v", err)
}

func TestFederatedCredentialServiceRejectsReplayedSubjectToken(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	service, _, _ := setupFederatedCredentialService(t, issuer)
	ctx := t.Context()
	subjectToken := issuer.token(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"})
	req := federated.TokenExchangeRequest{
		GrantType:        federated.TokenExchangeGrantType,
		SubjectToken:     subjectToken,
		SubjectTokenType: federated.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	}

	first, err := service.ExchangeToken(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := service.ExchangeToken(ctx, req)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrFederatedCredentialInvalidGrant, "unexpected error: %v", err)
	require.Nil(t, second)
}

func TestFederatedCredentialServiceCreateRejectsBareWildcardGlob(t *testing.T) {
	issuer := newFederatedTestIssuer(t)
	service, _, _ := setupFederatedCredentialService(t, issuer)

	_, err := service.Create(t.Context(), "admin-user", federated.CreateFederatedCredential{
		Name:            "Unsafe wildcard",
		IssuerURL:       "https://token.actions.githubusercontent.com",
		Audiences:       []string{"arcane-ci"},
		SubjectMatch:    "*",
		MatchType:       federated.MatchTypeGlob,
		RoleID:          "role-federated-viewer",
		TokenTTLSeconds: 900,
	})

	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrFederatedCredentialInvalid, "unexpected error: %v", err)
}

func newSettingsServiceForTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}
