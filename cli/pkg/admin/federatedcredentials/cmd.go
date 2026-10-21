// Package federatedcredentials manages workload identity federation trust rules.
package federatedcredentials

import (
	"cmp"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for federated credential operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "federated",
		Aliases: []string{"fedcreds", "federated-credentials"},
		Short:   "Manage workload identity federation trust rules",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	return command
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List federated credentials",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.FederatedCredential]{
				Resource: "federated credentials",
				Endpoint: clitypes.FederatedCredentials(),
				Params:   cmdutil.ListParams{Resource: "federated-credentials", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "ISSUER", "SUBJECT", "ROLE", "ENABLED"},
				Row: func(cred types.FederatedCredential) []string {
					return []string{cred.ID, cred.Name, cred.IssuerURL, cred.SubjectMatch, cmp.Or(cred.RoleName, cred.RoleID), strconv.FormatBool(cred.Enabled)}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of federated credentials to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <credential-id>",
		Short:        "Get federated credential details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.FederatedCredential]{
				Method:         http.MethodGet,
				Path:           clitypes.FederatedCredential(args[0]),
				FailureMessage: "failed to get federated credential",
				Render: func(data types.FederatedCredential) error {
					printCredential(data)
					return nil
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name         string
		description  string
		enabled      bool
		issuerURL    string
		audiences    []string
		subjectClaim string
		subjectMatch string
		matchType    string
		roleID       string
		environment  string
		tokenTTL     int
		expiresAt    string
	}
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a federated credential",
		Example: `  arcane admin federated create --name github-actions --issuer-url https://token.actions.githubusercontent.com \
    --audience arcane --subject-match "repo:myorg/*" --match-type glob --role role_deployer`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			var expiresAt *time.Time
			if opts.expiresAt != "" {
				parsed, parseErr := time.Parse(time.RFC3339, opts.expiresAt)
				if parseErr != nil {
					return fmt.Errorf("invalid --expires-at format (use RFC3339): %w", parseErr)
				}
				expiresAt = &parsed
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.FederatedCredential]{
				Method:         http.MethodPost,
				Path:           clitypes.FederatedCredentials(),
				FailureMessage: "failed to create federated credential",
				Body: types.FederatedCredential{
					Name:            opts.name,
					Enabled:         opts.enabled,
					IssuerURL:       opts.issuerURL,
					Audiences:       opts.audiences,
					SubjectClaim:    opts.subjectClaim,
					SubjectMatch:    opts.subjectMatch,
					MatchType:       opts.matchType,
					RoleID:          opts.roleID,
					Description:     kit.Ternary(cmd.Flags().Changed("description"), &opts.description, nil),
					EnvironmentID:   kit.Ternary(cmd.Flags().Changed("environment") && opts.environment != "", new(opts.environment), nil),
					TokenTTLSeconds: kit.Ternary(cmd.Flags().Changed("token-ttl"), opts.tokenTTL, 0),
					ExpiresAt:       expiresAt,
				},
				Render: func(data types.FederatedCredential) error {
					output.Success("Federated credential created")
					printCredential(data)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "Display name (required)")
	command.Flags().StringVar(&opts.description, "description", "", "Description")
	command.Flags().BoolVar(&opts.enabled, "enabled", true, "Enable the trust rule")
	command.Flags().StringVar(&opts.issuerURL, "issuer-url", "", "Trusted external OIDC issuer URL (required)")
	command.Flags().StringArrayVar(&opts.audiences, "audience", nil, "Allowed external token audience (repeatable, required)")
	command.Flags().StringVar(&opts.subjectClaim, "subject-claim", "", "Claim path to match against (defaults to sub)")
	command.Flags().StringVar(&opts.subjectMatch, "subject-match", "", "Exact subject or anchored glob pattern (required)")
	cmdutil.EnumVarP(command, command.Flags(), &opts.matchType, "match-type", "", "", "Subject match strategy",
		types.FederatedMatchTypeExact, types.FederatedMatchTypeGlob)
	command.Flags().StringVar(&opts.roleID, "role", "", "Role ID to map exchanged tokens to (required)")
	command.Flags().StringVar(&opts.environment, "environment", "", "Scope the role assignment to one environment (omit for global)")
	command.Flags().IntVar(&opts.tokenTTL, "token-ttl", 0, "Issued token lifetime in seconds (60-3600)")
	command.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Credential expiration (RFC3339 format)")
	_ = command.MarkFlagRequired("name")
	_ = command.MarkFlagRequired("issuer-url")
	_ = command.MarkFlagRequired("audience")
	_ = command.MarkFlagRequired("subject-match")
	_ = command.MarkFlagRequired("role")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name         string
		description  string
		enabled      bool
		issuerURL    string
		audiences    []string
		subjectClaim string
		subjectMatch string
		matchType    string
		roleID       string
		tokenTTL     int
		environment  string
		expiresAt    string
	}
	command := &cobra.Command{
		Use:          "update <credential-id>",
		Short:        "Update a federated credential",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			var expiresAt *time.Time
			if opts.expiresAt != "" {
				parsed, parseErr := time.Parse(time.RFC3339, opts.expiresAt)
				if parseErr != nil {
					return fmt.Errorf("invalid --expires-at format (use RFC3339): %w", parseErr)
				}
				expiresAt = &parsed
			}

			body := map[string]any{}
			for _, field := range []struct {
				changed bool
				key     string
				value   any
			}{
				{changed: cmd.Flags().Changed("name"), key: "name", value: opts.name},
				{changed: cmd.Flags().Changed("description"), key: "description", value: opts.description},
				{changed: cmd.Flags().Changed("enabled"), key: "enabled", value: opts.enabled},
				{changed: cmd.Flags().Changed("issuer-url"), key: "issuerUrl", value: opts.issuerURL},
				{changed: cmd.Flags().Changed("audience"), key: "audiences", value: opts.audiences},
				{changed: cmd.Flags().Changed("subject-claim"), key: "subjectClaim", value: opts.subjectClaim},
				{changed: cmd.Flags().Changed("subject-match"), key: "subjectMatch", value: opts.subjectMatch},
				{changed: cmd.Flags().Changed("match-type"), key: "matchType", value: opts.matchType},
				{changed: cmd.Flags().Changed("role"), key: "roleId", value: opts.roleID},
				{changed: cmd.Flags().Changed("environment"), key: "environmentId", value: kit.Ternary[any](opts.environment == "", nil, opts.environment)},
				{changed: cmd.Flags().Changed("token-ttl"), key: "tokenTtlSeconds", value: opts.tokenTTL},
				{changed: cmd.Flags().Changed("expires-at"), key: "expiresAt", value: expiresAt},
			} {
				if field.changed {
					body[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.FederatedCredential]{
				Method:         http.MethodPut,
				Path:           clitypes.FederatedCredential(args[0]),
				FailureMessage: "failed to update federated credential",
				Body:           body,
				Render: func(data types.FederatedCredential) error {
					output.Success("Federated credential updated")
					printCredential(data)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "New display name")
	command.Flags().StringVar(&opts.description, "description", "", "New description")
	command.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable the trust rule")
	command.Flags().StringVar(&opts.issuerURL, "issuer-url", "", "New issuer URL")
	command.Flags().StringArrayVar(&opts.audiences, "audience", nil, "Replace allowed audiences (repeatable)")
	command.Flags().StringVar(&opts.subjectClaim, "subject-claim", "", "New claim path to match against")
	command.Flags().StringVar(&opts.subjectMatch, "subject-match", "", "New subject match pattern")
	cmdutil.EnumVarP(command, command.Flags(), &opts.matchType, "match-type", "", "", "New match strategy",
		types.FederatedMatchTypeExact, types.FederatedMatchTypeGlob)
	command.Flags().StringVar(&opts.roleID, "role", "", "New role ID")
	command.Flags().StringVar(&opts.environment, "environment", "", "New environment scope")
	command.Flags().IntVar(&opts.tokenTTL, "token-ttl", 0, "New issued token lifetime in seconds (60-3600)")
	command.Flags().StringVar(&opts.expiresAt, "expires-at", "", "New credential expiration (RFC3339 format)")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <credential-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a federated credential",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Delete federated credential %s and its service user?", args[0]),
				Path:           clitypes.FederatedCredential(args[0]),
				FailureMessage: "failed to delete federated credential",
				SuccessMessage: "Federated credential deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func printCredential(cred types.FederatedCredential) {
	output.KeyValue("ID", cred.ID)
	output.KeyValue("Name", cred.Name)
	if cred.Description != nil && *cred.Description != "" {
		output.KeyValue("Description", *cred.Description)
	}
	output.KeyValue("Enabled", strconv.FormatBool(cred.Enabled))
	output.KeyValue("Issuer URL", cred.IssuerURL)
	output.KeyValue("Audiences", cred.Audiences)
	output.KeyValue("Subject claim", cred.SubjectClaim)
	output.KeyValue("Subject match", cred.SubjectMatch)
	output.KeyValue("Match type", cred.MatchType)
	output.KeyValue("Role", cmp.Or(cred.RoleName, cred.RoleID))
	scope := "global"
	if cred.EnvironmentID != nil {
		scope = *cred.EnvironmentID
		if cred.EnvironmentName != "" {
			scope = fmt.Sprintf("%s (%s)", cred.EnvironmentName, *cred.EnvironmentID)
		}
	}
	output.KeyValue("Scope", scope)
	output.KeyValue("Token TTL (s)", cred.TokenTTLSeconds)
	if cred.ServiceUsername != "" {
		output.KeyValue("Service user", cred.ServiceUsername)
	}
	if cred.LastUsedAt != nil {
		output.KeyValue("Last used", cred.LastUsedAt.Format("2006-01-02 15:04"))
	}
	if cred.ExpiresAt != nil {
		output.KeyValue("Expires", cred.ExpiresAt.Format("2006-01-02 15:04"))
	}
}
