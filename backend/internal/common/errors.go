package common

import "errors"

const (
	// ConfigurationErrorCodeEnvFileUnreadable is the ConfigurationError code reported
	// alongside ErrProjectEnvUnreadable.
	ConfigurationErrorCodeEnvFileUnreadable = "env_file_unreadable"
)

type classified struct {
	kind error
	err  error
}

// Classify gives err a stable semantic identity without changing its message.
func Classify(kind, err error) error {
	if err == nil {
		return nil
	}
	if kind == nil {
		return err
	}
	return &classified{kind: kind, err: err}
}

func (e *classified) Error() string { return e.err.Error() }

func (e *classified) Unwrap() error { return e.err }

func (e *classified) Is(target error) bool { return errors.Is(e.kind, target) }

//nolint:staticcheck // Preserve existing API error messages.
var (
	ErrBadRequest   = errors.New("kind: bad request")
	ErrValidation   = errors.New("kind: validation failed")
	ErrUnauthorized = errors.New("kind: unauthorized")
	ErrForbidden    = errors.New("kind: forbidden")
	ErrNotFound     = errors.New("kind: not found")
	ErrConflict     = errors.New("kind: conflict")
	ErrTimeout      = errors.New("kind: timeout")
	ErrUnavailable  = errors.New("kind: service unavailable")

	ErrFeatureDisabled                         = Classify(ErrForbidden, errors.New("feature disabled"))
	ErrInvalidToken                            = errors.New("invalid token")
	ErrExpiredToken                            = errors.New("token expired")
	ErrTokenVersionMismatch                    = errors.New("token version mismatch")
	ErrUserNotFound                            = errors.New("user not found")
	ErrAmbiguousUserEmail                      = Classify(ErrConflict, errors.New("multiple accounts share this email"))
	ErrTokenValidation                         = Classify(ErrUnauthorized, errors.New("Invalid token claims"))
	ErrSessionRevoked                          = Classify(ErrUnauthorized, errors.New("Session has been revoked"))
	ErrUpgradeInProgress                       = Classify(ErrConflict, errors.New("an upgrade is already in progress"))
	ErrUpdateAllInProgress                     = Classify(ErrConflict, errors.New("an update-all job is already in progress"))
	ErrUpdaterNoContainersMatched              = Classify(ErrBadRequest, errors.New("no compose-managed containers matched the requested resources"))
	ErrTemplateNotFound                        = Classify(ErrNotFound, errors.New("Template not found"))
	ErrInvalidEnvKey                           = Classify(ErrValidation, errors.New("Invalid environment key"))
	ErrGlobalVariableNotFound                  = Classify(ErrNotFound, errors.New("Global variable not found"))
	ErrApnsDisabled                            = Classify(ErrForbidden, errors.New("Mobile push notifications are disabled"))
	ErrApnsDeviceNotFound                      = Classify(ErrNotFound, errors.New("Mobile push device not found"))
	ErrApnsDeviceConflict                      = Classify(ErrConflict, errors.New("Mobile push device is already registered"))
	ErrApnsRelay                               = Classify(ErrUnavailable, errors.New("Push relay request failed"))
	ErrGlobalVariableConflict                  = Classify(ErrConflict, errors.New("Global variable already exists"))
	ErrGlobalVariableScopeRequired             = Classify(ErrValidation, errors.New("At least one environment is required when a variable is not scoped to all environments"))
	ErrGlobalVariableSecretValueRequired       = Classify(ErrValidation, errors.New("A new value is required when making a secret variable readable"))
	ErrImageUntagged                           = Classify(ErrBadRequest, errors.New("image has no tag; only tagged images can be patched"))
	ErrImageLocalOnly                          = Classify(ErrBadRequest, errors.New("locally built image has no registry source to patch from; rebuild it to update its packages"))
	ErrPatchRequiresContainerdImageStore       = Classify(ErrBadRequest, errors.New("image patching requires Docker's containerd image store; enable the containerd-snapshotter feature in daemon.json and restart Docker"))
	ErrPatchScanReportUnavailable              = Classify(ErrNotFound, errors.New("no stored scan report is available for this scan; re-scan the image or patch without a report"))
	ErrPatchScanImageMismatch                  = Classify(ErrBadRequest, errors.New("the selected scan does not belong to this image"))
	ErrContainerComposeManaged                 = Classify(ErrConflict, errors.New("container is managed by a compose project; edit it via the project editor"))
	ErrContainerNameTaken                      = Classify(ErrConflict, errors.New("a container with this name already exists"))
	ErrInvalidBackupSelection                  = Classify(ErrBadRequest, errors.New("invalid backup file selection"))
	ErrSwarmNotEnabled                         = Classify(ErrBadRequest, errors.New("Swarm mode is not enabled"))
	ErrSwarmManagerRequired                    = Classify(ErrForbidden, errors.New("Swarm manager access required"))
	ErrRoleNotFound                            = Classify(ErrNotFound, errors.New("Role not found"))
	ErrRoleBuiltIn                             = Classify(ErrForbidden, errors.New("Built-in role cannot be modified"))
	ErrRoleNameTaken                           = Classify(ErrConflict, errors.New("Role name already in use"))
	ErrUnknownPermission                       = Classify(ErrValidation, errors.New("Unknown permission"))
	ErrRolePermissionEscalation                = Classify(ErrForbidden, errors.New("cannot grant a permission you do not hold"))
	ErrInvalidRoleAssignment                   = Classify(ErrBadRequest, errors.New("invalid role assignment"))
	ErrFederatedCredentialNotFound             = Classify(ErrNotFound, errors.New("federated credential not found"))
	ErrFederatedCredentialInvalid              = Classify(ErrValidation, errors.New("invalid federated credential"))
	ErrFederatedCredentialInvalidRequest       = Classify(ErrBadRequest, errors.New("invalid federated token exchange request"))
	ErrFederatedCredentialInvalidGrant         = Classify(ErrUnauthorized, errors.New("invalid federated token grant"))
	ErrFederatedCredentialPermissionEscalation = Classify(ErrForbidden, errors.New("cannot map a federated credential to a role you do not hold"))
	ErrOidcMappingNotFound                     = Classify(ErrNotFound, errors.New("OIDC role mapping not found"))
	ErrOidcMappingEnvManaged                   = Classify(ErrConflict, errors.New("OIDC role mapping is managed by OIDC_ROLE_MAPPINGS and cannot be edited at runtime"))
	ErrNoGlobalAdminRemains                    = Classify(ErrConflict, errors.New("At least one user must retain a global Admin role assignment"))
	ErrProjectNotFound                         = Classify(ErrNotFound, errors.New("Project not found"))
	ErrContainerRegistryNotFound               = Classify(ErrNotFound, errors.New("Container registry not found"))
	ErrProjectArchived                         = Classify(ErrConflict, errors.New("project is archived and must be unarchived before this action"))
	ErrProjectMustBeStopped                    = Classify(ErrConflict, errors.New("project must be stopped before archiving"))
	ErrProjectWorkspaceConflict                = Classify(ErrConflict, errors.New("Project workspace changed; refresh it and try again"))
	ErrProjectWorkspaceForbidden               = Classify(ErrForbidden, errors.New("Forbidden project workspace path"))
	ErrProjectWorkspaceBadRequest              = Classify(ErrBadRequest, errors.New("Invalid project workspace request"))
	ErrProjectWorkspaceNotFound                = Classify(ErrNotFound, errors.New("Project workspace file not found"))
	ErrVolumeWorkspaceConflict                 = Classify(ErrConflict, errors.New("Volume workspace changed; refresh it and try again"))
	ErrVolumeWorkspaceForbidden                = Classify(ErrForbidden, errors.New("Forbidden volume workspace path"))
	ErrVolumeWorkspaceBadRequest               = Classify(ErrBadRequest, errors.New("Invalid volume workspace request"))
	ErrVolumeWorkspaceNotFound                 = Classify(ErrNotFound, errors.New("Volume workspace file not found"))
	ErrVolumeRenameInvalid                     = Classify(ErrBadRequest, errors.New("source and target volume names must be non-empty and different"))
	ErrVolumeRenameProtected                   = Classify(ErrBadRequest, errors.New("Arcane's internal volumes cannot be renamed"))
	ErrProjectComposeFileNotFound              = Classify(ErrNotFound, errors.New("Project compose file not found"))
	ErrComposeFileNotFound                     = Classify(ErrNotFound, errors.New("no compose file found"))
	ErrComposeFileEnvInvalid                   = Classify(ErrValidation, errors.New("invalid COMPOSE_FILE selection"))
	ErrProjectEnvUnreadable                    = Classify(ErrValidation, errors.New("project env file is not readable"))
	ErrEnvironmentInvalidProxyTarget           = Classify(ErrBadRequest, errors.New("Invalid proxy target URL"))
	ErrEnvironmentConnectionTestFailed         = Classify(ErrBadRequest, errors.New("Environment connection test failed"))
	ErrUnsafeRemoteURL                         = Classify(ErrBadRequest, errors.New("Remote URL is not allowed"))
	ErrImageScanInProgress                     = Classify(ErrConflict, errors.New("an image update check is already in progress"))
	ErrVulnerabilityScanNotFound               = Classify(ErrNotFound, errors.New("Vulnerability scan not found"))
	ErrInvalidNotificationPayloadTemplate      = Classify(ErrValidation, errors.New("invalid generic webhook payload template"))
	ErrRedeployAfterSyncFailed                 = errors.New("redeploy failed")
	ErrGitOpsSyncProjectBindingBroken          = errors.New("GitOps sync project binding broken")
	ErrUploadSessionNotFound                   = Classify(ErrNotFound, errors.New("Upload session not found"))
	ErrUploadSessionIncomplete                 = Classify(ErrConflict, errors.New("Upload session is incomplete"))
	ErrUploadKindMismatch                      = Classify(ErrBadRequest, errors.New("Upload session kind does not match this endpoint"))
	ErrUploadChunkInvalid                      = Classify(ErrValidation, errors.New("Invalid upload chunk"))
	ErrUploadSessionInvalid                    = Classify(ErrValidation, errors.New("Invalid upload session request"))
)
