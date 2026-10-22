package types

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
