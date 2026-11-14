package authz

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
)

func TestBuildPermissionExport(t *testing.T) {
	_, testAPI := humatest.New(t)
	api := &OperationRecorder{API: testAPI}
	handler := func(_ context.Context, _ *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-users",
		Method:      http.MethodGet,
		Path:        "/users",
		Summary:     "List users",
		Tags:        []string{"Users"},
		Metadata:    map[string]any{MetaRequiredPermission: PermUsersList},
	}, handler)
	huma.Register(api, huma.Operation{
		OperationID: "backup-action",
		Method:      http.MethodPost,
		Path:        "/backups/{id}",
		Metadata: map[string]any{
			MetaActionParam: "action",
			MetaActionPermissions: map[string]string{
				"restore":      PermSystemBackupsRestore,
				"restorefiles": PermSystemBackupsRestore,
			},
			MetaRequireGlobalAdmin: true,
		},
	}, func(_ context.Context, _ *struct {
		ID string `path:"id"`
	},
	) (*struct{}, error) {
		return &struct{}{}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Security:    []map[string][]string{},
	}, handler)
	huma.Register(api, huma.Operation{
		OperationID: "create-role",
		Method:      http.MethodPost,
		Path:        "/roles",
		Metadata:    map[string]any{MetaRequireGlobalAdmin: true},
	}, handler)
	huma.Register(api, huma.Operation{
		OperationID: "materialized-variables",
		Method:      http.MethodGet,
		Path:        "/variables",
		Metadata:    map[string]any{MetaRequireSudo: true},
	}, handler)
	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/me",
	}, handler)
	huma.Register(api, huma.Operation{
		OperationID: "build-image",
		Method:      http.MethodPost,
		Path:        "/images/build",
		Metadata: map[string]any{
			MetaRequiredPermission:   PermImagesBuild,
			MetaPermissionConditions: BuildPushConditions,
		},
	}, handler)

	huma.Register(api, huma.Operation{
		OperationID: "ingest-agent-event",
		Method:      http.MethodPost,
		Path:        "/events",
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{MetaAccess: EndpointAccessAgentToken},
	}, handler)

	diagnosticsConditions := PermissionConditions{
		{
			In:         ConditionInMessage,
			Field:      "type",
			Value:      "profile",
			Permission: PermDiagnosticsProfile,
		},
	}
	export := BuildPermissionExport(api, []PermissionRoute{
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/containers/:containerId/logs",
			Permission: PermContainersLogs,
		},
		{
			Method:     http.MethodGet,
			Path:       "/diagnostics/stream",
			Permission: PermDiagnosticsRead,
			Conditions: diagnosticsConditions,
		},
		{
			Method: http.MethodPost,
			Path:   "/webhooks/trigger/:token",
			Access: EndpointAccessWebhookToken,
		},
	})

	require.Equal(t, []PermissionExportEndpoint{
		{
			Method:     http.MethodGet,
			Path:       "/agents/{id}/ws/containers/{containerId}/logs",
			Access:     EndpointAccessPermission,
			Permission: PermContainersLogs,
		},
		{
			Method:      http.MethodPost,
			Path:        "/backups/{id}",
			OperationID: "backup-action",
			Access:      EndpointAccessActionPermissions,
			ActionParam: "action",
			ActionPermissions: map[string]string{
				"restore":      PermSystemBackupsRestore,
				"restorefiles": PermSystemBackupsRestore,
			},
			GlobalAdmin: true,
		},
		{
			Method:     http.MethodGet,
			Path:       "/diagnostics/stream",
			Access:     EndpointAccessPermission,
			Permission: PermDiagnosticsRead,
			Conditions: diagnosticsConditions,
		},
		{
			Method:      http.MethodPost,
			Path:        "/events",
			OperationID: "ingest-agent-event",
			Access:      EndpointAccessAgentToken,
		},
		{
			Method:      http.MethodGet,
			Path:        "/health",
			OperationID: "health",
			Access:      EndpointAccessPublic,
		},
		{
			Method:      http.MethodPost,
			Path:        "/images/build",
			OperationID: "build-image",
			Access:      EndpointAccessPermission,
			Permission:  PermImagesBuild,
			Conditions:  BuildPushConditions,
		},
		{
			Method:      http.MethodGet,
			Path:        "/me",
			OperationID: "me",
			Access:      EndpointAccessAuthenticated,
		},
		{
			Method:      http.MethodPost,
			Path:        "/roles",
			OperationID: "create-role",
			Access:      EndpointAccessGlobalAdmin,
		},
		{
			Method:      http.MethodGet,
			Path:        "/users",
			OperationID: "list-users",
			Summary:     "List users",
			Tags:        []string{"Users"},
			Access:      EndpointAccessPermission,
			Permission:  PermUsersList,
		},
		{
			Method:      http.MethodGet,
			Path:        "/variables",
			OperationID: "materialized-variables",
			Access:      EndpointAccessSudo,
		},
		{
			Method: http.MethodPost,
			Path:   "/webhooks/trigger/{token}",
			Access: EndpointAccessWebhookToken,
		},
	}, export.Endpoints)
	require.Nil(t, api.OpenAPI().Paths["/events"])

	actions := map[string]PermissionExportAction{}
	for _, resource := range export.Resources {
		for _, action := range resource.Actions {
			actions[action.Permission] = action
		}
	}

	require.Equal(t, []PermissionExportEndpointRef{
		{
			Method:      http.MethodGet,
			Path:        "/users",
			OperationID: "list-users",
			Summary:     "List users",
		},
	}, actions[PermUsersList].Endpoints)
	require.Equal(t, []PermissionExportEndpointRef{
		{
			Method:      http.MethodPost,
			Path:        "/backups/{id}",
			OperationID: "backup-action",
			Action: &PermissionExportActionSelect{
				Param: "action",
				Value: "restore",
			},
		},
		{
			Method:      http.MethodPost,
			Path:        "/backups/{id}",
			OperationID: "backup-action",
			Action: &PermissionExportActionSelect{
				Param: "action",
				Value: "restorefiles",
			},
		},
	}, actions[PermSystemBackupsRestore].Endpoints)
	require.Equal(t, []PermissionExportEndpointRef{
		{
			Method:      http.MethodPost,
			Path:        "/images/build",
			OperationID: "build-image",
			Condition:   new(BuildPushConditions[0]),
		},
	}, actions[PermImagesPush].Endpoints)
	require.Equal(t, []PermissionExportEndpointRef{
		{
			Method:    http.MethodGet,
			Path:      "/diagnostics/stream",
			Condition: new(diagnosticsConditions[0]),
		},
	}, actions[PermDiagnosticsProfile].Endpoints)
	require.Equal(t, []string{PermUsersList}, actions[PermUsersRead].Requires)
	require.Contains(t, actions[PermUsersCreate].RequiresExpanded, PermUsersList)
	require.NotContains(t, actions[PermUsersCreate].RequiresExpanded, PermUsersCreate)

	require.Contains(t, export.UnreferencedPermissions, PermUsersDelete)
	require.NotContains(t, export.UnreferencedPermissions, PermUsersList)
	require.NotContains(t, export.UnreferencedPermissions, PermSystemBackupsRestore)
	require.NotContains(t, export.UnreferencedPermissions, PermContainersLogs)
	require.NotContains(t, export.UnreferencedPermissions, PermImagesPush)
	require.NotContains(t, export.UnreferencedPermissions, PermDiagnosticsProfile)
}

func TestPermissionConditionsRequired(t *testing.T) {
	require.Equal(t, []string{PermVolumesUpload, PermVolumesDelete}, VolumeWorkspaceConditions.Required("fileChanges.operation", "rename"))
	require.Equal(t, []string{PermImagesPush}, BuildPushConditions.Required("push", "true"))
	require.Empty(t, BuildPushConditions.Required("push", "false"))
	require.Empty(t, UploadKindConditions.Required("kind", "unknown"))
	require.Equal(t, []string{PermGitOpsBackup}, GitOpsSyncModeConditions.Required("mode", "backup"))
	require.Equal(t, []string{PermGitOpsBidirectional}, GitOpsSyncModeConditions.Required("mode", "bidirectional"))
	require.Equal(t, []string{PermGitOpsBidirectional}, GitOpsModeConditions.Required("mode", "bidirectional"))
}
