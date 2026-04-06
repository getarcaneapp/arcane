package admin

import (
	"github.com/getarcaneapp/arcane/cli/pkg/admin/apikeys"
	"github.com/getarcaneapp/arcane/cli/pkg/admin/events"
	"github.com/getarcaneapp/arcane/cli/pkg/admin/notifications"
	"github.com/getarcaneapp/arcane/cli/pkg/admin/users"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:     "admin",
	Aliases: []string{"adm"},
	Short:   "Administration & platform management",
}

func init() {
	Cmd.AddCommand(users.Cmd)
	Cmd.AddCommand(apikeys.Cmd)
	Cmd.AddCommand(events.Cmd)
	Cmd.AddCommand(notifications.Cmd)
}
