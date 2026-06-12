package admin

import (
	"github.com/getarcaneapp/arcane/cli/v2/pkg/admin/apikeys"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/admin/events"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/admin/notifications"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/admin/oidcmappings"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/admin/roles"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/admin/users"
	"github.com/spf13/cobra"
)

// AdminCmd is the parent command for administrative operations.
var AdminCmd = &cobra.Command{
	Use:     "admin",
	Aliases: []string{"adm"},
	Short:   "Administration & platform management",
}

func init() {
	AdminCmd.AddCommand(users.UsersCmd)
	AdminCmd.AddCommand(roles.RolesCmd)
	AdminCmd.AddCommand(oidcmappings.OidcMappingsCmd)
	AdminCmd.AddCommand(apikeys.ApiKeysCmd)
	AdminCmd.AddCommand(events.EventsCmd)
	AdminCmd.AddCommand(notifications.NotificationsCmd)
}
