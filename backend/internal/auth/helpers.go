package auth

import (
	"context"
	"crypto/mldsa"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
)

const (
	browserSessionSigningKeySize = 64

	accessTokenSubject    = "access"
	browserTokenSubject   = "browser_session"
	refreshTokenSubject   = "refresh"
	browserRefreshSubject = "browser_refresh"

	claimSessionID             = "sid"
	claimUserID                = "user_id"
	claimUsername              = "username"
	claimEmail                 = "email"
	claimDisplayName           = "display_name"
	claimAppVersion            = "app_version"
	claimTokenType             = "token_type"
	claimFederatedCredentialID = "federated_credential_id"
)

type accessTokenClaims struct {
	UserID     string
	SessionID  string
	Username   string
	AppVersion string
	ExpiresAt  time.Time
}

type refreshTokenClaims struct {
	ID         string
	UserID     string
	SessionID  string
	AppVersion string
}

var tokenParseOptions = []jwt.ParseOption{
	jwt.WithStrictStringClaims(true),
	jwt.WithRequiredClaim(jwt.SubjectKey),
	jwt.WithRequiredClaim(jwt.JwtIDKey),
	jwt.WithRequiredClaim(jwt.IssuedAtKey),
	jwt.WithRequiredClaim(jwt.ExpirationKey),
	jwt.WithRequiredClaim(claimSessionID),
	jwt.WithRequiredClaim(claimUserID),
}

func parseAccessToken(ctx context.Context, rawToken string, key *mldsa.PublicKey) (*accessTokenClaims, error) {
	token, err := jwt.ParseString(rawToken, append(tokenParseOptions, jwt.WithKey(jwa.MLDSA87(), key), jwt.WithContext(ctx))...)
	if err != nil {
		switch {
		case errors.Is(err, jwt.TokenExpiredError{}):
			return nil, common.ErrExpiredToken
		case errors.Is(err, jwt.MissingRequiredClaimError{}), errors.Is(err, jwt.ClaimValidationError{}):
			return nil, common.ErrTokenValidation
		default:
			return nil, common.ErrInvalidToken
		}
	}

	subject, _ := token.Subject()
	id, _ := token.JwtID()
	userID, _ := jwt.Get[string](token, claimUserID)
	sessionID, _ := jwt.Get[string](token, claimSessionID)
	if subject != accessTokenSubject || userID == "" || sessionID == "" || id != userID {
		return nil, common.ErrTokenValidation
	}

	username, usernameErr := jwt.Get[string](token, claimUsername)
	appVersion, appVersionErr := jwt.Get[string](token, claimAppVersion)
	if errors.Is(usernameErr, jwt.ClaimTypeMismatchError{}) || errors.Is(appVersionErr, jwt.ClaimTypeMismatchError{}) {
		return nil, common.ErrTokenValidation
	}
	expiresAt, _ := token.Expiration()
	return new(accessTokenClaims{UserID: userID, SessionID: sessionID, Username: username, AppVersion: appVersion, ExpiresAt: expiresAt}), nil
}

func parseBrowserToken(ctx context.Context, rawToken string, key []byte) (*accessTokenClaims, error) {
	token, err := jwt.ParseString(rawToken, append(tokenParseOptions, jwt.WithKey(jwa.HS512(), key), jwt.WithContext(ctx))...)
	if err != nil {
		switch {
		case errors.Is(err, jwt.TokenExpiredError{}):
			return nil, common.ErrExpiredToken
		case errors.Is(err, jwt.MissingRequiredClaimError{}), errors.Is(err, jwt.ClaimValidationError{}):
			return nil, common.ErrTokenValidation
		default:
			return nil, common.ErrInvalidToken
		}
	}

	subject, _ := token.Subject()
	id, _ := token.JwtID()
	userID, _ := jwt.Get[string](token, claimUserID)
	sessionID, _ := jwt.Get[string](token, claimSessionID)
	if subject != browserTokenSubject || id == "" || userID == "" || sessionID == "" {
		return nil, common.ErrTokenValidation
	}

	appVersion, appVersionErr := jwt.Get[string](token, claimAppVersion)
	if errors.Is(appVersionErr, jwt.ClaimTypeMismatchError{}) {
		return nil, common.ErrTokenValidation
	}
	expiresAt, _ := token.Expiration()
	return new(accessTokenClaims{UserID: userID, SessionID: sessionID, AppVersion: appVersion, ExpiresAt: expiresAt}), nil
}

func signedTokenAlgorithm(rawToken string) (string, bool) {
	message, err := jws.Parse([]byte(rawToken), jws.WithCompact())
	if err != nil {
		return "", false
	}
	signatures := message.Signatures()
	if len(signatures) != 1 || signatures[0].ProtectedHeaders() == nil {
		return "", false
	}
	algorithm, ok := signatures[0].ProtectedHeaders().Algorithm()
	if !ok {
		return "", false
	}
	return algorithm.String(), true
}

func parseRefreshToken(ctx context.Context, rawToken, wantSubject string, key jwt.ParseOption) (*refreshTokenClaims, error) {
	token, err := jwt.ParseString(rawToken, append(tokenParseOptions, key, jwt.WithContext(ctx))...)
	if err != nil {
		claimErr := errors.Is(err, jwt.MissingRequiredClaimError{}) || errors.Is(err, jwt.ClaimValidationError{})
		return nil, kit.Ternary[error](claimErr, common.ErrTokenValidation, common.ErrInvalidToken)
	}

	subject, _ := token.Subject()
	id, _ := token.JwtID()
	userID, _ := jwt.Get[string](token, claimUserID)
	sessionID, _ := jwt.Get[string](token, claimSessionID)
	if subject != wantSubject || id == "" || userID == "" || sessionID == "" {
		return nil, common.ErrTokenValidation
	}

	appVersion, appVersionErr := jwt.Get[string](token, claimAppVersion)
	if errors.Is(appVersionErr, jwt.ClaimTypeMismatchError{}) {
		return nil, common.ErrTokenValidation
	}
	return new(refreshTokenClaims{ID: id, UserID: userID, SessionID: sessionID, AppVersion: appVersion}), nil
}

func requireUser(ctx context.Context) (*user.User, error) {
	currentUser, exists := user.CurrentUserFromContext(ctx)
	if !exists || currentUser == nil {
		return nil, huma.Error401Unauthorized("Not authenticated")
	}
	return currentUser, nil
}
