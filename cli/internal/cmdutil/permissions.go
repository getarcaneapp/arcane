package cmdutil

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func ValidatePermissions(ctx context.Context, c *client.Client, perms []string) error {
	if len(perms) == 0 {
		return nil
	}
	manifest, err := c.GetJSON[types.PermissionsManifest](ctx, clitypes.RolesAvailablePermissions())
	if err != nil {
		return fmt.Errorf("failed to load permission manifest: %w", err)
	}
	known := map[string]struct{}{}
	for _, resource := range manifest.Data.Resources {
		for _, action := range resource.Actions {
			known[action.Permission] = struct{}{}
		}
	}
	var unknown []string
	for _, perm := range perms {
		if _, ok := known[perm]; !ok && !slices.Contains(unknown, perm) {
			unknown = append(unknown, perm)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown permission(s): %s (run `arcane admin roles permissions` to list valid values)", strings.Join(unknown, ", "))
	}
	return nil
}

func FormatSelector(selector types.ResourceSelector) string {
	parts := make([]string, 0, 4)
	if len(selector.Labels) > 0 {
		labels := make([]string, 0, len(selector.Labels))
		for _, key := range slices.Sorted(maps.Keys(selector.Labels)) {
			labels = append(labels, key+"="+selector.Labels[key])
		}
		parts = append(parts, "labels "+strings.Join(labels, ","))
	}
	if len(selector.Projects) > 0 {
		parts = append(parts, "projects "+strings.Join(selector.Projects, ","))
	}
	if selector.OwnerOnly {
		parts = append(parts, "owner only")
	}
	if len(selector.Names) > 0 {
		parts = append(parts, "names "+strings.Join(selector.Names, ","))
	}
	return strings.Join(parts, "; ")
}

func RenderEffectiveAccess(access types.EffectiveAccess) error {
	if access.IsGlobalAdmin {
		output.Info("Administrator: every permission in every environment")
	}
	if len(access.Scopes) == 0 {
		output.Info("No permissions granted")
		return nil
	}
	for _, scope := range access.Scopes {
		output.Header("%s", kit.Ternary(scope.EnvironmentID == nil, "Global", "Environment "+kit.FromPtr(scope.EnvironmentID)))
		rows := make([][]string, len(scope.Permissions))
		for i, perm := range scope.Permissions {
			selectors := make([]string, len(perm.Selectors))
			for j, selector := range perm.Selectors {
				selectors[j] = FormatSelector(selector)
			}
			via := make([]string, len(perm.Via))
			for j, ref := range perm.Via {
				via[j] = ref.RoleName + kit.Ternary(ref.GroupName == nil, "", " (group "+kit.FromPtr(ref.GroupName)+")")
			}
			rows[i] = []string{
				perm.Permission,
				kit.Ternary(perm.Unrestricted, "unrestricted", strings.Join(selectors, " | ")),
				strings.Join(via, ", "),
			}
		}
		if err := output.Table([]string{"PERMISSION", "ACCESS", "VIA"}, rows); err != nil {
			return err
		}
	}
	return nil
}
