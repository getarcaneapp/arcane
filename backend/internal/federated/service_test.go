package federated

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
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
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
	"github.com/stretchr/testify/assert"
)

type federatedTestIssuerInternal struct {
	IssuerURL string
	private   *rsa.PrivateKey
	keyID     string
	server    *httptest.Server
}

func newFederatedTestIssuerInternal(t *testing.T) *federatedTestIssuerInternal {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	issuer := &federatedTestIssuerInternal{
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
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})) {
			return
		}
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := privateKey.PublicKey
		if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"use": "sig",
					"kid": issuer.keyID,
					"alg": "RS256",
					"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
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

func (i *federatedTestIssuerInternal) tokenInternal(t *testing.T, subject string, audience []string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": i.IssuerURL,
		"sub": subject,
		"aud": audience,
		"iat": now.Unix(),
		"nbf": now.Add(-time.Minute).Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = i.keyID
	signed, err := token.SignedString(i.private)
	require.NoError(t, err)
	return signed
}

func setupFederatedCredentialServiceTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&common.User{},
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

func setupFederatedCredentialServiceInternal(t *testing.T, issuer *federatedTestIssuerInternal) (*FederatedCredentialService, *auth.AuthService, *database.DB) {
	t.Helper()

	ctx := context.Background()
	db := setupFederatedCredentialServiceTestDBInternal(t)
	roleSvc := role.NewRoleService(db)
	userSvc := user.NewUserService(db).WithRoleService(roleSvc)
	sessionSvc := session.NewSessionService(db)
	settingsSvc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	eventSvc := event.NewEventService(db, &config.Config{}, nil)
	authSvc := auth.NewAuthService(userSvc, settingsSvc, eventSvc, sessionSvc, roleSvc, "test-federated-secret", &config.Config{
		JWTRefreshExpiry: 24 * time.Hour,
	}, nil)

	service := NewFederatedCredentialService(db, authSvc, userSvc, settingsSvc, eventSvc, issuer.server.Client()).WithRoleService(roleSvc)

	viewerRole := role.Role{
		BaseModel:   database.BaseModel{ID: "role-federated-viewer"},
		Name:        "Federated Viewer",
		Permissions: database.StringSlice{authz.PermProjectsList},
	}
	require.NoError(t, db.WithContext(ctx).Create(&viewerRole).Error)

	serviceUser := common.User{
		BaseModel:        database.BaseModel{ID: "user-federated-service"},
		Username:         "svc-federated-demo",
		IsServiceAccount: true,
	}
	require.NoError(t, db.WithContext(ctx).Create(&serviceUser).Error)
	require.NoError(t, db.WithContext(ctx).Create(&role.UserRoleAssignment{
		UserID: serviceUser.ID,
		RoleID: viewerRole.ID,
	}).Error)

	credential := FederatedCredential{
		BaseModel:       database.BaseModel{ID: "cred-github-actions"},
		Name:            "GitHub Actions",
		Enabled:         true,
		IssuerURL:       issuer.IssuerURL,
		Audiences:       database.StringSlice{"arcane-ci"},
		SubjectClaim:    "sub",
		SubjectMatch:    "repo:getarcaneapp/arcane:*",
		MatchType:       federatedtypes.MatchTypeGlob,
		RoleID:          viewerRole.ID,
		IdentityUserID:  serviceUser.ID,
		TokenTTLSeconds: 900,
	}
	require.NoError(t, db.WithContext(ctx).Create(&credential).Error)

	return service, authSvc, db
}

func TestFederatedCredentialServiceExchangeToken(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, authSvc, db := setupFederatedCredentialServiceInternal(t, issuer)
	ctx := context.Background()

	tests := []struct {
		name      string
		token     string
		wantError func(error) bool
	}{
		{
			name:  "issues an Arcane bearer token for a matching issuer audience and subject",
			token: issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		},
		{
			name:  "rejects audience mismatch",
			token: issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"other-audience"}),
			wantError: func(err error) bool {
				return errors.Is(err, common.ErrFederatedCredentialInvalidGrant)
			},
		},
		{
			name:  "rejects subject mismatch",
			token: issuer.tokenInternal(t, "repo:other/repo:ref:refs/heads/main", []string{"arcane-ci"}),
			wantError: func(err error) bool {
				return errors.Is(err, common.ErrFederatedCredentialInvalidGrant)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.ExchangeToken(ctx, federatedtypes.TokenExchangeRequest{
				GrantType:        federatedtypes.TokenExchangeGrantType,
				SubjectToken:     tt.token,
				SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
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
			require.Equal(t, federatedtypes.IssuedTokenTypeAccessToken, resp.IssuedTokenType)
			require.Positive(t, resp.ExpiresIn)
			require.NotEmpty(t, resp.AccessToken)

			user, sessionID, err := authSvc.VerifyToken(ctx, resp.AccessToken)
			require.NoError(t, err)
			require.Equal(t, "user-federated-service", user.ID)

			var userSession session.UserSession
			require.NoError(t, db.WithContext(ctx).Where("id = ?", sessionID).First(&userSession).Error)
			require.Equal(t, session.UserSessionSourceFederated, userSession.Source)
			require.NotNil(t, userSession.FederatedCredentialID)
			require.Equal(t, "cred-github-actions", *userSession.FederatedCredentialID)
		})
	}
}

func TestFederatedCredentialServiceExchangeTokenRejectsIssuerWithoutCredentialInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	otherIssuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)

	resp, err := service.ExchangeToken(context.Background(), federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     otherIssuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrFederatedCredentialInvalidGrant), "unexpected error: %v", err)
	require.Nil(t, resp)
}

func TestFederatedCredentialServiceExchangeTokenDoesNotRequireGlobalFeatureFlagInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)
	service.settingsService = nil

	resp, err := service.ExchangeToken(context.Background(), federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.AccessToken)
}

func TestFederatedCredentialServiceExchangeTokenRejectsExpiredCredentialInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, db := setupFederatedCredentialServiceInternal(t, issuer)
	expiredAt := time.Now().Add(-time.Minute)
	require.NoError(t, db.WithContext(context.Background()).
		Model(&FederatedCredential{}).
		Where("id = ?", "cred-github-actions").
		Update("expires_at", expiredAt).Error)

	resp, err := service.ExchangeToken(context.Background(), federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrFederatedCredentialInvalidGrant), "unexpected error: %v", err)
	require.Nil(t, resp)
}

func TestFederatedCredentialServiceUpdateDisableRevokesIssuedSessionsInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, authSvc, _ := setupFederatedCredentialServiceInternal(t, issuer)
	ctx := context.Background()

	resp, err := service.ExchangeToken(ctx, federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = service.Update(ctx, "admin-user", "cred-github-actions", federatedtypes.UpdateFederatedCredential{
		Enabled: new(false),
	})
	require.NoError(t, err)

	_, _, err = authSvc.VerifyToken(ctx, resp.AccessToken)
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrSessionRevoked), "unexpected error: %v", err)
}

func TestFederatedCredentialServiceRejectsReplayedSubjectTokenInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)
	ctx := context.Background()
	subjectToken := issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"})
	req := federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     subjectToken,
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	}

	first, err := service.ExchangeToken(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := service.ExchangeToken(ctx, req)
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrFederatedCredentialInvalidGrant), "unexpected error: %v", err)
	require.Nil(t, second)
}

func TestFederatedCredentialServiceCreateRejectsBareWildcardGlob(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)

	_, err := service.Create(context.Background(), "admin-user", federatedtypes.CreateFederatedCredential{
		Name:            "Unsafe wildcard",
		IssuerURL:       "https://token.actions.githubusercontent.com",
		Audiences:       []string{"arcane-ci"},
		SubjectMatch:    "*",
		MatchType:       federatedtypes.MatchTypeGlob,
		RoleID:          "role-federated-viewer",
		TokenTTLSeconds: 900,
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrFederatedCredentialInvalid), "unexpected error: %v", err)
}

// Test fixtures shared by this package's tests.

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(t.Context(), runtime, "settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, executor.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, executor, effects)
}
