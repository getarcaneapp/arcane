// Package federated contains DTOs for OIDC workload identity federation.
package federated

import "time"

const (
	TokenExchangeGrantType      = "urn:ietf:params:oauth:grant-type:token-exchange" // #nosec G101: RFC 8693 grant type identifier, not a credential.
	SubjectTokenTypeJWT         = "urn:ietf:params:oauth:token-type:jwt"            // #nosec G101: RFC 8693 token type identifier, not a credential.
	SubjectTokenTypeIDToken     = "urn:ietf:params:oauth:token-type:id_token"       // #nosec G101: RFC 8693 token type identifier, not a credential.
	IssuedTokenTypeAccessToken  = "urn:ietf:params:oauth:token-type:access_token"   // #nosec G101: RFC 8693 token type identifier, not a credential.
	RequestedTokenTypeAccessJWT = "urn:ietf:params:oauth:token-type:access_token"   // #nosec G101: RFC 8693 token type identifier, not a credential.

	MatchTypeExact = "exact"
	MatchTypeGlob  = "glob"
)

// FederatedCredential is a configured trust rule for one external OIDC
// workload identity subject.
type FederatedCredential struct {
	ID              string     `json:"id" doc:"Unique identifier of the federated credential"`
	Name            string     `json:"name" doc:"Display name"`
	Description     *string    `json:"description,omitempty" doc:"Optional description"`
	Enabled         bool       `json:"enabled" doc:"Whether exchanges are allowed"`
	IssuerURL       string     `json:"issuerUrl" doc:"Trusted external OIDC issuer URL"`
	Audiences       []string   `json:"audiences" doc:"Allowed external token audiences"`
	SubjectClaim    string     `json:"subjectClaim" doc:"Claim path to match against"`
	SubjectMatch    string     `json:"subjectMatch" doc:"Exact subject or anchored glob pattern"`
	MatchType       string     `json:"matchType" doc:"Subject match strategy" enum:"exact,glob"`
	RoleID          string     `json:"roleId" doc:"Mapped role ID"`
	EnvironmentID   *string    `json:"environmentId,omitempty" doc:"Optional environment scope for the role assignment"`
	IdentityUserID  string     `json:"identityUserId" doc:"Dedicated service user ID backing issued tokens"`
	TokenTTLSeconds int        `json:"tokenTtlSeconds" doc:"Issued token lifetime in seconds"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty" doc:"Last successful token exchange"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty" doc:"Optional credential expiration"`
	CreatedAt       time.Time  `json:"createdAt" doc:"Creation timestamp"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty" doc:"Last update timestamp"`
	ServiceUsername string     `json:"serviceUsername,omitempty" doc:"Dedicated service account username"`
	RoleName        string     `json:"roleName,omitempty" doc:"Mapped role name"`
	EnvironmentName string     `json:"environmentName,omitempty" doc:"Mapped environment name when scoped"`
}

// CreateFederatedCredential is the request body for creating a federated
// workload identity credential.
type CreateFederatedCredential struct {
	Name            string     `json:"name" minLength:"1" maxLength:"255" doc:"Display name"`
	Description     *string    `json:"description,omitempty" maxLength:"1000" doc:"Optional description"`
	Enabled         bool       `json:"enabled" doc:"Whether exchanges are allowed"`
	IssuerURL       string     `json:"issuerUrl" minLength:"1" format:"uri" doc:"Trusted external OIDC issuer URL"`
	Audiences       []string   `json:"audiences" minItems:"1" doc:"Allowed external token audiences"`
	SubjectClaim    string     `json:"subjectClaim,omitempty" doc:"Claim path to match against; defaults to sub"`
	SubjectMatch    string     `json:"subjectMatch" minLength:"1" doc:"Exact subject or anchored glob pattern"`
	MatchType       string     `json:"matchType,omitempty" enum:"exact,glob" doc:"Subject match strategy"`
	RoleID          string     `json:"roleId" minLength:"1" doc:"Mapped role ID"`
	EnvironmentID   *string    `json:"environmentId,omitempty" doc:"Optional environment scope for the role assignment"`
	TokenTTLSeconds int        `json:"tokenTtlSeconds,omitempty" minimum:"60" maximum:"3600" doc:"Issued token lifetime in seconds"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty" doc:"Optional credential expiration"`
}

// UpdateFederatedCredential is the request body for updating a federated
// workload identity credential.
type UpdateFederatedCredential struct {
	Name            *string    `json:"name,omitempty" maxLength:"255" doc:"Display name"`
	Description     *string    `json:"description,omitempty" maxLength:"1000" doc:"Optional description"`
	Enabled         *bool      `json:"enabled,omitempty" doc:"Whether exchanges are allowed"`
	IssuerURL       *string    `json:"issuerUrl,omitempty" format:"uri" doc:"Trusted external OIDC issuer URL"`
	Audiences       []string   `json:"audiences,omitempty" minItems:"1" doc:"Allowed external token audiences"`
	SubjectClaim    *string    `json:"subjectClaim,omitempty" doc:"Claim path to match against"`
	SubjectMatch    *string    `json:"subjectMatch,omitempty" minLength:"1" doc:"Exact subject or anchored glob pattern"`
	MatchType       *string    `json:"matchType,omitempty" enum:"exact,glob" doc:"Subject match strategy"`
	RoleID          *string    `json:"roleId,omitempty" minLength:"1" doc:"Mapped role ID"`
	EnvironmentID   *string    `json:"environmentId,omitempty" doc:"Optional environment scope for the role assignment"`
	TokenTTLSeconds *int       `json:"tokenTtlSeconds,omitempty" minimum:"60" maximum:"3600" doc:"Issued token lifetime in seconds"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty" doc:"Optional credential expiration"`
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
