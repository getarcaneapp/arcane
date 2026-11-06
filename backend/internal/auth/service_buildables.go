//go:build buildables

package auth

import (
	"context"
	"log/slog"

	"getarcane.dev/arcane/v3/buildables"
	"getarcane.dev/types/v3"
)

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

func (s *AuthService) GetAutoLoginPassword() string {
	if !buildables.HasBuildFeature("autologin") {
		return ""
	}
	return s.config.AutoLoginPassword
}
