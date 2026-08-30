package common

import (
	"fmt"
	"io"

	"emperror.dev/errors"
)

const (
	ErrBadRequest   = errors.Sentinel("kind: bad request")
	ErrValidation   = errors.Sentinel("kind: validation failed")
	ErrUnauthorized = errors.Sentinel("kind: unauthorized")
	ErrForbidden    = errors.Sentinel("kind: forbidden")
	ErrNotFound     = errors.Sentinel("kind: not found")
	ErrConflict     = errors.Sentinel("kind: conflict")
	ErrTimeout      = errors.Sentinel("kind: timeout")
	ErrUnavailable  = errors.Sentinel("kind: service unavailable")
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

func (e *classified) Format(s fmt.State, verb rune) {
	if verb == 'v' && s.Flag('+') {
		_, _ = fmt.Fprintf(s, "%+v", e.err)
		return
	}

	switch verb {
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.err)
	case 's', 'v':
		_, _ = io.WriteString(s, e.err.Error())
	}
}

var (
	ErrInvalidToken                            = errors.Sentinel("invalid token")
	ErrExpiredToken                            = errors.Sentinel("token expired")
	ErrTokenVersionMismatch                    = errors.Sentinel("token version mismatch")
	ErrUserNotFound                            = errors.Sentinel("user not found")
	ErrAmbiguousUserEmail                      = Classify(ErrConflict, errors.Sentinel("multiple accounts share this email"))
	ErrTokenValidation                         = Classify(ErrUnauthorized, errors.Sentinel("Invalid token claims"))
	ErrSessionRevoked                          = Classify(ErrUnauthorized, errors.Sentinel("Session has been revoked"))
	ErrUpgradeInProgress                       = Classify(ErrConflict, errors.Sentinel("an upgrade is already in progress"))
	ErrUpdateAllInProgress                     = Classify(ErrConflict, errors.Sentinel("an update-all job is already in progress"))
	ErrUpdaterNoContainersMatched              = Classify(ErrBadRequest, errors.Sentinel("no compose-managed containers matched the requested resources"))
	ErrTemplateNotFound                        = Classify(ErrNotFound, errors.Sentinel("Template not found"))
	ErrInvalidEnvKey                           = Classify(ErrValidation, errors.Sentinel("Invalid environment key"))
	ErrGlobalVariableNotFound                  = Classify(ErrNotFound, errors.Sentinel("Global variable not found"))
	ErrApnsDisabled                            = Classify(ErrForbidden, errors.Sentinel("Mobile push notifications are disabled"))
	ErrApnsDeviceNotFound                      = Classify(ErrNotFound, errors.Sentinel("Mobile push device not found"))
	ErrApnsDeviceConflict                      = Classify(ErrConflict, errors.Sentinel("Mobile push device is already registered"))
	ErrApnsRelay                               = Classify(ErrUnavailable, errors.Sentinel("Push relay request failed"))
	ErrGlobalVariableConflict                  = Classify(ErrConflict, errors.Sentinel("Global variable already exists"))
	ErrGlobalVariableScopeRequired             = Classify(ErrValidation, errors.Sentinel("At least one environment is required when a variable is not scoped to all environments"))
	ErrGlobalVariableSecretValueRequired       = Classify(ErrValidation, errors.Sentinel("A new value is required when making a secret variable readable"))
	ErrImageUntagged                           = Classify(ErrBadRequest, errors.Sentinel("image has no tag; only tagged images can be patched"))
	ErrImageLocalOnly                          = Classify(ErrBadRequest, errors.Sentinel("locally built image has no registry source to patch from; rebuild it to update its packages"))
	ErrPatchScanReportUnavailable              = Classify(ErrNotFound, errors.Sentinel("no stored scan report is available for this scan; re-scan the image or patch without a report"))
	ErrPatchScanImageMismatch                  = Classify(ErrBadRequest, errors.Sentinel("the selected scan does not belong to this image"))
	ErrContainerComposeManaged                 = Classify(ErrConflict, errors.Sentinel("container is managed by a compose project; edit it via the project editor"))
	ErrContainerNameTaken                      = Classify(ErrConflict, errors.Sentinel("a container with this name already exists"))
	ErrInvalidBackupSelection                  = Classify(ErrBadRequest, errors.Sentinel("invalid backup file selection"))
	ErrSwarmNotEnabled                         = Classify(ErrBadRequest, errors.Sentinel("Swarm mode is not enabled"))
	ErrSwarmManagerRequired                    = Classify(ErrForbidden, errors.Sentinel("Swarm manager access required"))
	ErrRoleNotFound                            = Classify(ErrNotFound, errors.Sentinel("Role not found"))
	ErrRoleBuiltIn                             = Classify(ErrForbidden, errors.Sentinel("Built-in role cannot be modified"))
	ErrRoleNameTaken                           = Classify(ErrConflict, errors.Sentinel("Role name already in use"))
	ErrUnknownPermission                       = Classify(ErrValidation, errors.Sentinel("Unknown permission"))
	ErrRolePermissionEscalation                = Classify(ErrForbidden, errors.Sentinel("cannot grant a permission you do not hold"))
	ErrInvalidRoleAssignment                   = Classify(ErrBadRequest, errors.Sentinel("invalid role assignment"))
	ErrFederatedCredentialNotFound             = Classify(ErrNotFound, errors.Sentinel("federated credential not found"))
	ErrFederatedCredentialInvalid              = Classify(ErrValidation, errors.Sentinel("invalid federated credential"))
	ErrFederatedCredentialInvalidRequest       = Classify(ErrBadRequest, errors.Sentinel("invalid federated token exchange request"))
	ErrFederatedCredentialInvalidGrant         = Classify(ErrUnauthorized, errors.Sentinel("invalid federated token grant"))
	ErrFederatedCredentialPermissionEscalation = Classify(ErrForbidden, errors.Sentinel("cannot map a federated credential to a role you do not hold"))
	ErrOidcMappingNotFound                     = Classify(ErrNotFound, errors.Sentinel("OIDC role mapping not found"))
	ErrOidcMappingEnvManaged                   = Classify(ErrConflict, errors.Sentinel("OIDC role mapping is managed by OIDC_ROLE_MAPPINGS and cannot be edited at runtime"))
	ErrNoGlobalAdminRemains                    = Classify(ErrConflict, errors.Sentinel("At least one user must retain a global Admin role assignment"))
	ErrProjectNotFound                         = Classify(ErrNotFound, errors.Sentinel("Project not found"))
	ErrProjectArchived                         = Classify(ErrConflict, errors.Sentinel("project is archived and must be unarchived before this action"))
	ErrProjectMustBeStopped                    = Classify(ErrConflict, errors.Sentinel("project must be stopped before archiving"))
	ErrProjectWorkspaceConflict                = Classify(ErrConflict, errors.Sentinel("Project workspace changed; refresh it and try again"))
	ErrProjectWorkspaceForbidden               = Classify(ErrForbidden, errors.Sentinel("Forbidden project workspace path"))
	ErrProjectWorkspaceBadRequest              = Classify(ErrBadRequest, errors.Sentinel("Invalid project workspace request"))
	ErrProjectWorkspaceNotFound                = Classify(ErrNotFound, errors.Sentinel("Project workspace file not found"))
	ErrVolumeWorkspaceConflict                 = Classify(ErrConflict, errors.Sentinel("Volume workspace changed; refresh it and try again"))
	ErrVolumeWorkspaceForbidden                = Classify(ErrForbidden, errors.Sentinel("Forbidden volume workspace path"))
	ErrVolumeWorkspaceBadRequest               = Classify(ErrBadRequest, errors.Sentinel("Invalid volume workspace request"))
	ErrVolumeWorkspaceNotFound                 = Classify(ErrNotFound, errors.Sentinel("Volume workspace file not found"))
	ErrVolumeRenameInvalid                     = Classify(ErrBadRequest, errors.Sentinel("source and target volume names must be non-empty and different"))
	ErrVolumeRenameProtected                   = Classify(ErrBadRequest, errors.Sentinel("Arcane's internal volumes cannot be renamed"))
	ErrProjectComposeFileNotFound              = Classify(ErrNotFound, errors.Sentinel("Project compose file not found"))
	ErrComposeFileNotFound                     = Classify(ErrNotFound, errors.Sentinel("no compose file found"))
	ErrComposeFileEnvInvalid                   = Classify(ErrValidation, errors.Sentinel("invalid COMPOSE_FILE selection"))
	ErrEnvironmentInvalidProxyTarget           = Classify(ErrBadRequest, errors.Sentinel("Invalid proxy target URL"))
	ErrEnvironmentConnectionTestFailed         = Classify(ErrBadRequest, errors.Sentinel("Environment connection test failed"))
	ErrUnsafeRemoteURL                         = Classify(ErrBadRequest, errors.Sentinel("Remote URL is not allowed"))
	ErrImageScanInProgress                     = Classify(ErrConflict, errors.Sentinel("an image update check is already in progress"))
	ErrVulnerabilityScanNotFound               = Classify(ErrNotFound, errors.Sentinel("Vulnerability scan not found"))
	ErrInvalidNotificationPayloadTemplate      = Classify(ErrValidation, errors.Sentinel("invalid generic webhook payload template"))
	ErrRedeployAfterSyncFailed                 = errors.Sentinel("redeploy failed")
	ErrGitOpsSyncProjectBindingBroken          = errors.Sentinel("GitOps sync project binding broken")
	ErrUploadSessionNotFound                   = Classify(ErrNotFound, errors.Sentinel("Upload session not found"))
	ErrUploadSessionIncomplete                 = Classify(ErrConflict, errors.Sentinel("Upload session is incomplete"))
	ErrUploadKindMismatch                      = Classify(ErrBadRequest, errors.Sentinel("Upload session kind does not match this endpoint"))
	ErrUploadChunkInvalid                      = Classify(ErrValidation, errors.Sentinel("Invalid upload chunk"))
	ErrUploadSessionInvalid                    = Classify(ErrValidation, errors.Sentinel("Invalid upload session request"))
)
