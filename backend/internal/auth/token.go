package auth

import (
	"context"
	"crypto/mldsa"
	"time"

	"emperror.dev/errors"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

const (
	browserSessionSigningKeySize = 64

	accessTokenSubject  = "access"
	browserTokenSubject = "browser_session"
	refreshTokenSubject = "refresh"

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

func parseAccessTokenInternal(ctx context.Context, rawToken string, key *mldsa.PublicKey) (*accessTokenClaims, error) {
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

func parseBrowserTokenInternal(ctx context.Context, rawToken string, key []byte) (*accessTokenClaims, error) {
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

func signedTokenAlgorithmInternal(rawToken string) (string, bool) {
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

func parseRefreshTokenInternal(ctx context.Context, rawToken string, key *mldsa.PublicKey) (*refreshTokenClaims, error) {
	token, err := jwt.ParseString(rawToken, append(tokenParseOptions, jwt.WithKey(jwa.MLDSA87(), key), jwt.WithContext(ctx))...)
	if err != nil {
		if errors.Is(err, jwt.MissingRequiredClaimError{}) || errors.Is(err, jwt.ClaimValidationError{}) {
			return nil, common.ErrTokenValidation
		}
		return nil, common.ErrInvalidToken
	}

	subject, _ := token.Subject()
	id, _ := token.JwtID()
	userID, _ := jwt.Get[string](token, claimUserID)
	sessionID, _ := jwt.Get[string](token, claimSessionID)
	if subject != refreshTokenSubject || id == "" || userID == "" || sessionID == "" {
		return nil, common.ErrTokenValidation
	}

	appVersion, appVersionErr := jwt.Get[string](token, claimAppVersion)
	if errors.Is(appVersionErr, jwt.ClaimTypeMismatchError{}) {
		return nil, common.ErrTokenValidation
	}
	return new(refreshTokenClaims{ID: id, UserID: userID, SessionID: sessionID, AppVersion: appVersion}), nil
}
