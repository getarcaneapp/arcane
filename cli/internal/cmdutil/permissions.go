package cmdutil

import (
	"context"
	"fmt"
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
			via := make([]string, len(perm.Via))
			for j, ref := range perm.Via {
				via[j] = ref.RoleName + kit.Ternary(ref.GroupName == nil, "", " (group "+kit.FromPtr(ref.GroupName)+")")
			}
			rows[i] = []string{
				perm.Permission,
				strings.Join(via, ", "),
			}
		}
		if err := output.Table([]string{"PERMISSION", "VIA"}, rows); err != nil {
			return err
		}
	}
	return nil
}
