package di

import (
	"context"

	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/apns"
	"getarcane.dev/arcane/v3/internal/appimages"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/enrollment"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/federated"
	"getarcane.dev/arcane/v3/internal/ldap"
	"getarcane.dev/arcane/v3/internal/notification"
	"getarcane.dev/arcane/v3/internal/oidc"
	"getarcane.dev/arcane/v3/internal/passkey"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/scheduler"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/webhook"
	pkgoidc "getarcane.dev/arcane/v3/pkg/oidc"
)

var (
	managerServiceOptions = fx.Options(
		fx.Provide(
			federated.NewFederatedCredentialService,
			appimages.NewApplicationImagesService,
			passkey.NewPasskeyService,
			enrollment.NewEnrollmentService,
			oidc.NewOidcService,
			provideJWKSetManager,
		),
		fx.Provide(
			role.New,
			ldap.New,
			apikey.New,
			template.New,
			environment.New,
			enrollment.New,
			webhook.New,
			apns.New,
			notification.New,
			func(service *auth.AuthService, userService *user.UserService, settingsService *settings.SettingsService, passkeyService *passkey.PasskeyService) *auth.Module {
				return auth.New(service, userService, settingsService, passkeyService.BeginMFAAuthentication)
			},
			func(service *user.UserService, tokens *auth.TokenCache, settingsService *settings.SettingsService) *user.Module {
				return user.New(service, tokens.InvalidateUser, settingsService)
			},
		),
		fx.Invoke(func(lc fx.Lifecycle, service *webhook.WebhookService) {
			lc.Append(fx.Hook{OnStart: service.LoadTokenHashes})
		}),
	)

	managerJobOptions = fx.Options(
		fx.Provide(
			scheduler.NewExpiredSessionsCleanupJob,
			scheduler.NewApnsOutboxJob,
			scheduler.NewLdapSyncJob,
		),
	)
)

func provideJWKSetManager(ctx context.Context, lc fx.Lifecycle) *pkgoidc.KeySetManager {
	manager := pkgoidc.NewKeySetManager(ctx)
	lc.Append(fx.Hook{OnStop: manager.Shutdown})
	return manager
}
