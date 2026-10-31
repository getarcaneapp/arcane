package admin

import (
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/apikeys"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/events"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/federatedcredentials"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/notifications"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/oidcmappings"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/roles"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin/users"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/backups"
)

// Command is the parent command for administrative operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:         "admin",
		Aliases:     []string{"adm"},
		Short:       "Administration & platform management",
	}
	command.AddCommand(backups.Command())
	command.AddCommand(users.Command())
	command.AddCommand(roles.Command())
	command.AddCommand(oidcmappings.Command())
	command.AddCommand(apikeys.Command())
	command.AddCommand(federatedcredentials.Command())
	command.AddCommand(events.Command())
	command.AddCommand(notifications.Command())
	return command
}
