package auth

// OidcUserInfo represents user information retrieved from an OIDC provider.
type OidcUserInfo struct {
	// Subject is the unique identifier for the user at the OIDC provider.
	//
	// Required: true
	Subject string `json:"sub"`

	// Name is the full name of the user.
	//
	// Required: false
	Name string `json:"name,omitempty"`

	// Email is the email address of the user.
	//
	// Required: false
	Email string `json:"email,omitempty"`

	// EmailVerified indicates if the user's email has been verified.
	//
	// Required: false
	EmailVerified bool `json:"email_verified,omitempty"`

	// PreferredUsername is the user's preferred username.
	//
	// Required: false
	PreferredUsername string `json:"preferred_username,omitempty"`

	// GivenName is the user's given name (first name).
	//
	// Required: false
	GivenName string `json:"given_name,omitempty"`

	// FamilyName is the user's family name (last name).
	//
	// Required: false
	FamilyName string `json:"family_name,omitempty"`

	// Admin indicates if the user is an administrator.
	//
	// Required: false
	Admin bool `json:"admin,omitempty"`

	// Roles is a list of roles assigned to the user.
	//
	// Required: false
	Roles []string `json:"roles,omitempty"`

	// Groups is a list of groups the user belongs to.
	//
	// Required: false
	Groups []string `json:"groups,omitempty"`

	// Extra contains additional claims from the userinfo endpoint that are not
	// part of the standard OIDC claims. This field is not serialized to JSON.
	//
	// Required: false
	Extra map[string]any `json:"-"`
}

// OidcTokenResponse represents the response from an OIDC token endpoint.
type OidcTokenResponse struct {
	// AccessToken is the OAuth 2.0 access token.
	//
	// Required: true
	AccessToken string `json:"access_token"`

	// TokenType specifies the type of the access token (typically "Bearer").
	//
	// Required: true
	TokenType string `json:"token_type"`

	// RefreshToken is the OAuth 2.0 refresh token.
	//
	// Required: false
	RefreshToken string `json:"refresh_token,omitempty"`

	// ExpiresIn is the lifetime of the access token in seconds.
	//
	// Required: false
	ExpiresIn int `json:"expires_in,omitempty"`

	// IDToken is the OpenID Connect ID token.
	//
	// Required: false
	IDToken string `json:"id_token,omitempty"`
}

// OidcStatusInfo represents the status of OIDC configuration and usage.
type OidcStatusInfo struct {
	// EnvForced indicates if OIDC is forced via environment configuration.
	//
	// Required: true
	EnvForced bool `json:"envForced"`

	// EnvConfigured indicates if OIDC is configured via environment variables.
	//
	// Required: true
	EnvConfigured bool `json:"envConfigured"`

	// MergeAccounts indicates if accounts should be merged when using OIDC.
	//
	// Required: true
	MergeAccounts bool `json:"mergeAccounts"`
}

// OidcAuthUrlRequest is used to request an OIDC authorization URL.
type OidcAuthUrlRequest struct {
	// RedirectUri is the URI to redirect to after successful authentication.
	//
	// Required: true
	RedirectUri string `json:"redirectUri"`
}
