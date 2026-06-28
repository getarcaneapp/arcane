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

// Command is the parent command for administrative operations.
var Command = &cobra.Command{
	Use:     "admin",
	Aliases: []string{"adm"},
	Short:   "Administration & platform management",
}

func init() {
	Command.AddCommand(users.Command)
	Command.AddCommand(roles.Command)
	Command.AddCommand(oidcmappings.Command)
	Command.AddCommand(apikeys.Command)
	Command.AddCommand(events.Command)
	Command.AddCommand(notifications.Command)
}
