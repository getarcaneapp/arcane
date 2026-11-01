package admin

import (
	"github.com/spf13/cobra"

	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/pkg/admin/apikeys"
	"getarcane.dev/cli/v3/pkg/admin/events"
	"getarcane.dev/cli/v3/pkg/admin/federatedcredentials"
	"getarcane.dev/cli/v3/pkg/admin/notifications"
	"getarcane.dev/cli/v3/pkg/admin/oidcmappings"
	"getarcane.dev/cli/v3/pkg/admin/roles"
	"getarcane.dev/cli/v3/pkg/admin/users"
	"getarcane.dev/cli/v3/pkg/backups"
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
