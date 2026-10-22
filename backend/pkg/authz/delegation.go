package authz

import (
	"fmt"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func CanDefineRole(caller *PermissionSet, perms []string) error {
	for _, p := range perms {
		if !caller.Scope(p, "").IsUnrestricted() {
			return fmt.Errorf("%w: %s", common.ErrRolePermissionEscalation, p)
		}
	}
	return nil
}

func CanBind(caller *PermissionSet, perms []string, envID string, sel *types.ResourceSelector) error {
	granted, _ := EffectiveBindingPermissions(perms, sel)
	for _, p := range granted {
		if !caller.Scope(p, envID).Covers(sel) {
			return fmt.Errorf("%w: %s", common.ErrRolePermissionEscalation, p)
		}
	}
	return nil
}

func EffectiveBindingPermissions(perms []string, sel *types.ResourceSelector) (granted, dropped []string) {
	if sel == nil {
		return slices.Clone(perms), nil
	}
	kinds := selectorKinds(*sel)
	for _, p := range perms {
		supported := permissionSelectors[p]
		if len(supported) > 0 && !slices.ContainsFunc(kinds, func(kind types.SelectorKind) bool {
			return !slices.Contains(supported, kind)
		}) {
			granted = append(granted, p)
		} else {
			dropped = append(dropped, p)
		}
	}
	return granted, dropped
}

func SelectorIsSubset(narrow, broad types.ResourceSelector) bool {
	if broad.OwnerOnly || len(selectorKinds(broad)) == 0 || len(selectorKinds(narrow)) == 0 {
		return false
	}
	for key, value := range broad.Labels {
		if got, ok := narrow.Labels[key]; !ok || got != value {
			return false
		}
	}
	if len(broad.Projects) > 0 && (len(narrow.Projects) == 0 || slices.ContainsFunc(narrow.Projects, func(project string) bool {
		return !slices.Contains(broad.Projects, project)
	})) {
		return false
	}
	return len(broad.Names) == 0 || len(narrow.Names) > 0 && !slices.ContainsFunc(narrow.Names, func(name string) bool {
		return !slices.ContainsFunc(broad.Names, func(glob string) bool {
			return globCovers(glob, name)
		})
	})
}

func globCovers(broad, narrow string) bool {
	if broad == narrow {
		return true
	}
	prefix, ok := strings.CutSuffix(broad, "*")
	return ok &&
		!strings.ContainsAny(prefix, `*?[\`) &&
		strings.HasPrefix(narrow, prefix) &&
		!strings.Contains(narrow[len(prefix):], "/")
}
