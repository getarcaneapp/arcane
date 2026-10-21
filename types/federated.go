package types

import (
	"time"
)

const (
	TokenExchangeGrantType      = "urn:ietf:params:oauth:grant-type:token-exchange" // #nosec G101: RFC 8693 grant type identifier, not a credential.
	SubjectTokenTypeJWT         = "urn:ietf:params:oauth:token-type:jwt"            // #nosec G101: RFC 8693 token type identifier, not a credential.
	SubjectTokenTypeIDToken     = "urn:ietf:params:oauth:token-type:id_token"       // #nosec G101: RFC 8693 token type identifier, not a credential.
	IssuedTokenTypeAccessToken  = "urn:ietf:params:oauth:token-type:access_token"   // #nosec G101: RFC 8693 token type identifier, not a credential.
	RequestedTokenTypeAccessJWT = "urn:ietf:params:oauth:token-type:access_token"   // #nosec G101: RFC 8693 token type identifier, not a credential.

	FederatedMatchTypeExact = "exact"
	FederatedMatchTypeGlob  = "glob"
)

// FederatedCredential is a configured trust rule for one external OIDC
// workload identity subject.
type FederatedCredential struct {
	ID              string     `json:"id" readOnly:"true" doc:"Unique identifier of the federated credential"`
	Name            string     `json:"name" required:"false" minLength:"1" maxLength:"255" doc:"Display name" unorm:"nfc" trim:"true"`
	Description     *string    `json:"description,omitempty" maxLength:"1000" doc:"Optional description" unorm:"nfc" trim:"true"`
	Enabled         bool       `json:"enabled" required:"false" doc:"Whether exchanges are allowed"`
	IssuerURL       string     `json:"issuerUrl" required:"false" doc:"Trusted external OIDC issuer URL"`
	Audiences       []string   `json:"audiences" required:"false" doc:"Allowed external token audiences"`
	SubjectClaim    string     `json:"subjectClaim" required:"false" doc:"Claim path to match against; defaults to sub"`
	SubjectMatch    string     `json:"subjectMatch" required:"false" doc:"Exact subject or anchored glob pattern"`
	MatchType       string     `json:"matchType" required:"false" doc:"Subject match strategy" enum:"exact,glob"`
	RoleID          string     `json:"roleId" required:"false" doc:"Mapped role ID"`
	EnvironmentID   *string    `json:"environmentId,omitempty" doc:"Optional environment scope for the role assignment"`
	IdentityUserID  string     `json:"identityUserId" readOnly:"true" doc:"Dedicated service user ID backing issued tokens"`
	TokenTTLSeconds int        `json:"tokenTtlSeconds" required:"false" minimum:"60" maximum:"3600" doc:"Issued token lifetime in seconds"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty" readOnly:"true" doc:"Last successful token exchange"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty" doc:"Optional credential expiration"`
	CreatedAt       time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
	ServiceUsername string     `json:"serviceUsername,omitempty" readOnly:"true" doc:"Dedicated service account username"`
	RoleName        string     `json:"roleName,omitempty" readOnly:"true" doc:"Mapped role name"`
	EnvironmentName string     `json:"environmentName,omitempty" readOnly:"true" doc:"Mapped environment name when scoped"`
}

// TokenExchangeRequest is the RFC 8693 token exchange form payload after
// server-side parsing.
type TokenExchangeRequest struct {
	GrantType          string
	SubjectToken       string
	SubjectTokenType   string
	Audience           string
	Scope              string
	RequestedTokenType string
}

// FederatedTokenResponse is the RFC 8693 successful token exchange response.
type FederatedTokenResponse struct {
	AccessToken     string `json:"access_token"`      //nolint:tagliatelle // RFC 8693 wire shape is snake_case.
	TokenType       string `json:"token_type"`        //nolint:tagliatelle // RFC 8693 wire shape is snake_case.
	ExpiresIn       int    `json:"expires_in"`        //nolint:tagliatelle // RFC 8693 wire shape is snake_case.
	IssuedTokenType string `json:"issued_token_type"` //nolint:tagliatelle // RFC 8693 wire shape is snake_case.
}
