// Package notification owns notification settings, dispatch, and HTTP routes.
package notification

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service *NotificationService
	config  *config.Config
}

func New(service *NotificationService, cfg *config.Config) *Module {
	return &Module{service: service, config: cfg}
}

func (m *Module) Service() *NotificationService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterNotifications(api, nil, nil)
		return
	}
	RegisterNotifications(api, m.service, m.config)
}

// RegisterNotifications registers notification endpoints.
func RegisterNotifications(api huma.API, notificationSvc *NotificationService, cfg *config.Config) {
	h := &NotificationHandler{
		notificationService: notificationSvc,
		config:              cfg,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-all-notification-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/settings",
		Summary:     "Get all notification settings",
		Tags:        []string{"Notifications"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNotificationsManage, h.GetAllNotificationSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-notification-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Get notification settings by provider",
		Tags:        []string{"Notifications"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNotificationsManage, h.GetNotificationSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-or-update-notification-settings",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/settings",
		Summary:     "Create or update notification settings",
		Tags:        []string{"Notifications"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNotificationsManage, h.CreateOrUpdateNotificationSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-notification-settings",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Delete notification settings",
		Tags:        []string{"Notifications"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermNotificationsManage, h.DeleteNotificationSettings)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "notification-settings-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Test notification",
		Tags:        []string{"Notifications"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"test": authz.PermNotificationsManage,
	}, h.TestNotification)

	// Environment tokens are authenticated by ApiKeyAuth and revalidated by the
	// handler. RBAC middleware cannot scope this route because it has no environment ID.
	huma.Register(api, huma.Operation{
		OperationID: "dispatch-notification",
		Method:      http.MethodPost,
		Path:        "/notifications/dispatch",
		Summary:     "Dispatch notification from remote agent to manager",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.DispatchNotification)
}
