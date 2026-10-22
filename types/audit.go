package types

import "time"

const (
	AuditActorUser   = "user"
	AuditActorApiKey = "apikey"
	AuditActorAgent  = "agent"
	AuditActorSystem = "system"
	AuditActorOidc   = "oidc"
)

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeDenied  = "denied"
	AuditOutcomeError   = "error"
)

const (
	AuditActionRoleCreate         = "role.create"
	AuditActionRoleUpdate         = "role.update"
	AuditActionRoleDelete         = "role.delete"
	AuditActionBindingCreate      = "binding.create"
	AuditActionBindingDelete      = "binding.delete"
	AuditActionGroupCreate        = "group.create"
	AuditActionGroupUpdate        = "group.update"
	AuditActionGroupDelete        = "group.delete"
	AuditActionGroupMemberAdd     = "group.member.add"
	AuditActionGroupMemberRemove  = "group.member.remove"
	AuditActionGroupOidcAdminKept = "group.oidc_sync.admin_kept"
	AuditActionOidcMappingCreate  = "oidc_mapping.create"
	AuditActionOidcMappingUpdate  = "oidc_mapping.update"
	AuditActionOidcMappingDelete  = "oidc_mapping.delete"
	AuditActionApiKeyCreate       = "apikey.create"
	AuditActionApiKeyUpdate       = "apikey.update"
	AuditActionApiKeyDelete       = "apikey.delete"
	AuditActionUserCreate         = "user.create"
	AuditActionUserUpdate         = "user.update"
	AuditActionUserDelete         = "user.delete"
	AuditActionUserDisable        = "user.disable"
	AuditActionUserEnable         = "user.enable"
	AuditActionUserPasswordReset  = "user.password_reset"
	AuditActionFederatedCreate    = "federated.create"
	AuditActionFederatedUpdate    = "federated.update"
	AuditActionFederatedDelete    = "federated.delete"
	AuditActionSettingsUpdate     = "settings.update"
	AuditActionAuthzDenied        = "authz.denied"
)

const (
	AuditTargetRole                = "role"
	AuditTargetBinding             = "binding"
	AuditTargetGroup               = "group"
	AuditTargetOidcMapping         = "oidc_mapping"
	AuditTargetApiKey              = "apikey"
	AuditTargetUser                = "user"
	AuditTargetFederatedCredential = "federated_credential"
	AuditTargetSetting             = "setting"
)

const (
	AuditExportFormatCSV    = "csv"
	AuditExportFormatNDJSON = "ndjson"
)

type AuditLog struct {
	ID            string         `json:"id" doc:"Unique identifier of the audit entry"`
	CreatedAt     time.Time      `json:"createdAt" doc:"When the event was recorded"`
	ActorType     string         `json:"actorType" enum:"user,apikey,agent,system,oidc" doc:"Kind of caller that performed the action"`
	ActorUserID   *string        `json:"actorUserId,omitempty" doc:"ID of the acting user"`
	ActorUsername string         `json:"actorUsername,omitempty" doc:"Username of the acting user at the time of the event"`
	ApiKeyID      *string        `json:"apiKeyId,omitempty" doc:"ID of the API key used for the request"`
	Action        string         `json:"action" doc:"Audited action, such as role.create or authz.denied"`
	Outcome       string         `json:"outcome" enum:"success,denied,error" doc:"Result of the action"`
	TargetType    string         `json:"targetType,omitempty" doc:"Kind of object the action changed"`
	TargetID      string         `json:"targetId,omitempty" doc:"ID of the changed object"`
	TargetName    string         `json:"targetName,omitempty" doc:"Name of the changed object at the time of the event"`
	EnvironmentID *string        `json:"environmentId,omitempty" doc:"Environment the action applied to"`
	Permission    string         `json:"permission,omitempty" doc:"Permission that was checked"`
	RequestMethod string         `json:"requestMethod,omitempty" doc:"HTTP method of the request"`
	RequestPath   string         `json:"requestPath,omitempty" doc:"HTTP path of the request"`
	RemoteAddr    string         `json:"remoteAddr,omitempty" doc:"Remote address of the caller"`
	UserAgent     string         `json:"userAgent,omitempty" doc:"User agent of the caller"`
	Before        any            `json:"before,omitempty" doc:"State before the change"`
	After         any            `json:"after,omitempty" doc:"State after the change"`
	Metadata      map[string]any `json:"metadata,omitempty" doc:"Additional event details"`
}

type AuditEntry struct {
	Action        string
	Outcome       string
	ActorType     string
	TargetType    string
	TargetID      string
	TargetName    string
	EnvironmentID *string
	Permission    string
	Before        any
	After         any
	Metadata      map[string]any
}

type AuthzDenial struct {
	Permission    string
	EnvironmentID string
	Method        string
	Path          string
	RemoteAddr    string
	UserAgent     string
}

type AuditFilter struct {
	ActorUserID   string
	Action        string
	Outcome       string
	TargetType    string
	EnvironmentID string
	From          *time.Time
	To            *time.Time
}
