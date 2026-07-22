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
	ErrTokenValidation                         = Classify(ErrUnauthorized, errors.Sentinel("Invalid token claims"))
	ErrSessionRevoked                          = Classify(ErrUnauthorized, errors.Sentinel("Session has been revoked"))
	ErrUpgradeInProgress                       = Classify(ErrConflict, errors.Sentinel("an upgrade is already in progress"))
	ErrUpdateAllInProgress                     = Classify(ErrConflict, errors.Sentinel("an update-all job is already in progress"))
	ErrTemplateNotFound                        = Classify(ErrNotFound, errors.Sentinel("Template not found"))
	ErrInvalidEnvKey                           = Classify(ErrValidation, errors.Sentinel("Invalid environment key"))
	ErrGlobalVariableNotFound                  = Classify(ErrNotFound, errors.Sentinel("Global variable not found"))
	ErrGlobalVariableConflict                  = Classify(ErrConflict, errors.Sentinel("Global variable already exists"))
	ErrGlobalVariableScopeRequired             = Classify(ErrValidation, errors.Sentinel("At least one environment is required when a variable is not scoped to all environments"))
	ErrGlobalVariableSecretValueRequired       = Classify(ErrValidation, errors.Sentinel("A new value is required when making a secret variable readable"))
	ErrSwarmNotEnabled                         = Classify(ErrBadRequest, errors.Sentinel("Swarm mode is not enabled"))
	ErrSwarmManagerRequired                    = Classify(ErrForbidden, errors.Sentinel("Swarm manager access required"))
	ErrSwarmConfigImmutable                    = Classify(ErrConflict, errors.Sentinel("Swarm configs are immutable; create a new config and update services to use it"))
	ErrSwarmSecretImmutable                    = Classify(ErrConflict, errors.Sentinel("Swarm secrets are immutable; create a new secret and update services to use it"))
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
	ErrProjectArchived                         = Classify(ErrConflict, errors.Sentinel("project is archived and must be unarchived before this action"))
	ErrProjectMustBeStopped                    = Classify(ErrConflict, errors.Sentinel("project must be stopped before archiving"))
	ErrProjectFileConflict                     = Classify(ErrConflict, errors.Sentinel("Project files changed; refresh the project and try again"))
	ErrProjectFileForbidden                    = Classify(ErrForbidden, errors.Sentinel("Forbidden project file path"))
	ErrProjectFileBadRequest                   = Classify(ErrBadRequest, errors.Sentinel("Invalid project file request"))
	ErrProjectFileNotFound                     = Classify(ErrNotFound, errors.Sentinel("Project file not found"))
	ErrProjectComposeFileNotFound              = Classify(ErrNotFound, errors.Sentinel("Project compose file not found"))
	ErrComposeFileNotFound                     = Classify(ErrNotFound, errors.Sentinel("no compose file found"))
	ErrEnvironmentInvalidProxyTarget           = Classify(ErrBadRequest, errors.Sentinel("Invalid proxy target URL"))
	ErrUnsafeRemoteURL                         = Classify(ErrBadRequest, errors.Sentinel("Remote URL is not allowed"))
	ErrImageScanInProgress                     = Classify(ErrConflict, errors.Sentinel("an image update check is already in progress"))
	ErrRedeployAfterSyncFailed                 = errors.Sentinel("redeploy failed")
	ErrGitOpsSyncProjectBindingBroken          = errors.Sentinel("GitOps sync project binding broken")
)
