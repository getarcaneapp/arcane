//go:build buildables

package auth

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/buildables"
)

// GetAutoLoginConfig returns the auto-login configuration for the frontend.
// The password is never returned. Auto-login is disabled if local auth is disabled.
func (s *AuthService) GetAutoLoginConfig(ctx context.Context) (*types.AutoLoginConfig, error) {
	if !buildables.HasBuildFeature("autologin") {
		return &types.AutoLoginConfig{
			Enabled:  false,
			Username: "",
		}, nil
	}

	localEnabled, err := s.IsLocalAuthEnabled(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to check local auth status for auto-login", "error", err)
		return &types.AutoLoginConfig{
			Enabled:  false,
			Username: "",
		}, nil
	}

	if !localEnabled {
		slog.DebugContext(ctx, "Auto-login disabled because local auth is disabled")
		return &types.AutoLoginConfig{
			Enabled:  false,
			Username: "",
		}, nil
	}

	return &types.AutoLoginConfig{
		Enabled:  true,
		Username: s.config.AutoLoginUsername,
	}, nil
}

// GetAutoLoginPassword returns the auto-login password for internal use only.
// This should only be called by the login handler to validate auto-login credentials.
// WARNING: Never expose this value through any API response!
func (s *AuthService) GetAutoLoginPassword() string {
	if !buildables.HasBuildFeature("autologin") {
		return ""
	}
	return s.config.AutoLoginPassword
}
