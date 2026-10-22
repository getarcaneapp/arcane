package authz

import (
	"fmt"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func CanDefineRole(caller *PermissionSet, perms []string) error {
	for _, p := range perms {
		if !caller.Allows(p, "") {
			return fmt.Errorf("%w: %s", common.ErrRolePermissionEscalation, p)
		}
	}
	return nil
}

func CanBind(caller *PermissionSet, perms []string, envID string) error {
	for _, p := range perms {
		if !caller.Allows(p, envID) {
			return fmt.Errorf("%w: %s", common.ErrRolePermissionEscalation, p)
		}
	}
	return nil
}
