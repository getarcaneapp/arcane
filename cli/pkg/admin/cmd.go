package admin

import (
	"github.com/getarcaneapp/arcane/cli/pkg/admin/apikeys"
	"github.com/getarcaneapp/arcane/cli/pkg/admin/events"
	"github.com/getarcaneapp/arcane/cli/pkg/admin/notifications"
	"github.com/getarcaneapp/arcane/cli/pkg/admin/users"
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
	AdminCmd.AddCommand(apikeys.ApiKeysCmd)
	AdminCmd.AddCommand(events.EventsCmd)
	AdminCmd.AddCommand(notifications.NotificationsCmd)
}
