// Package federatedcredentials provides the `arcane admin federated` command
// tree. Federated credentials are workload identity federation trust rules:
// an external OIDC token matching the rule is exchanged for a short-lived
// Arcane access token backed by a dedicated service user.
package federatedcredentials

import (
	"fmt"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
	"github.com/spf13/cobra"
)

var (
	limitFlag          int
	startFlag          int
	allFlag            bool
	forceFlag          bool
	jsonOutput         bool
	createName         string
	createDescription  string
	createDisabled     bool
	createIssuerURL    string
	createAudiences    []string
	createSubjectClaim string
	createSubjectMatch string
	createMatchType    string
	createRoleID       string
	createEnvironment  string
	createTokenTTL     int
	createExpiresAt    string
	updateName         string
	updateDescription  string
	updateEnabled      bool
	updateDisabled     bool
	updateIssuerURL    string
	updateAudiences    []string
	updateSubjectClaim string
	updateSubjectMatch string
	updateMatchType    string
	updateRoleID       string
	updateTokenTTL     int
	updateExpiresAt    string
)

// FederatedCredentialsCmd is the parent command for federated credential operations.
var FederatedCredentialsCmd = &cobra.Command{
	Use:     "federated",
	Aliases: []string{"fedcreds", "federated-credentials"},
	Short:   "Manage workload identity federation trust rules",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List federated credentials",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.FederatedCredentials(), cmdutil.ListParams{
			Resource:        "federated-credentials",
			Limit:           limitFlag,
			FallbackDefault: 20,
			Start:           startFlag,
			All:             allFlag,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list federated credentials")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[federatedtypes.FederatedCredential]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to list federated credentials")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		headers := []string{"ID", "NAME", "ISSUER", "SUBJECT", "ROLE", "ENABLED"}
		rows := make([][]string, len(result.Data))
		for i, cred := range result.Data {
			role := cred.RoleName
			if role == "" {
				role = cred.RoleID
			}
			enabled := "false"
			if cred.Enabled {
				enabled = "true"
			}
			rows[i] = []string{cred.ID, cred.Name, cred.IssuerURL, cred.SubjectMatch, role, enabled}
		}
		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "federated credentials")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <credential-id>",
	Short:        "Get federated credential details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.FederatedCredential(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to get federated credential")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[federatedtypes.FederatedCredential]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to get federated credential")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		printCredentialInternal(result.Data)
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a federated credential",
	Example: `  arcane admin federated create --name github-actions --issuer-url https://token.actions.githubusercontent.com \
    --audience arcane --subject-match "repo:myorg/*" --match-type glob --role role_deployer`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		req := federatedtypes.CreateFederatedCredential{
			Name:         createName,
			Enabled:      !createDisabled,
			IssuerURL:    createIssuerURL,
			Audiences:    createAudiences,
			SubjectClaim: createSubjectClaim,
			SubjectMatch: createSubjectMatch,
			MatchType:    createMatchType,
			RoleID:       createRoleID,
		}
		if cmd.Flags().Changed("description") {
			req.Description = &createDescription
		}
		if cmd.Flags().Changed("environment") && createEnvironment != "" {
			req.EnvironmentID = new(createEnvironment)
		}
		if cmd.Flags().Changed("token-ttl") {
			req.TokenTTLSeconds = createTokenTTL
		}
		if createExpiresAt != "" {
			parsed, err := time.Parse(time.RFC3339, createExpiresAt)
			if err != nil {
				return errors.WrapIf(err, "invalid --expires-at format (use RFC3339)")
			}
			req.ExpiresAt = &parsed
		}

		resp, err := c.Post(cmd.Context(), types.FederatedCredentials(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create federated credential")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[federatedtypes.FederatedCredential]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to create federated credential")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Federated credential created")
		printCredentialInternal(result.Data)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <credential-id>",
	Short:        "Update a federated credential",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("enabled") && cmd.Flags().Changed("disabled") {
			return errors.New("--enabled and --disabled are mutually exclusive")
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		var req federatedtypes.UpdateFederatedCredential
		if cmd.Flags().Changed("name") {
			req.Name = &updateName
		}
		if cmd.Flags().Changed("description") {
			req.Description = &updateDescription
		}
		if cmd.Flags().Changed("enabled") {
			req.Enabled = new(true)
		}
		if cmd.Flags().Changed("disabled") {
			req.Enabled = new(false)
		}
		if cmd.Flags().Changed("issuer-url") {
			req.IssuerURL = &updateIssuerURL
		}
		if cmd.Flags().Changed("audience") {
			req.Audiences = updateAudiences
		}
		if cmd.Flags().Changed("subject-claim") {
			req.SubjectClaim = &updateSubjectClaim
		}
		if cmd.Flags().Changed("subject-match") {
			req.SubjectMatch = &updateSubjectMatch
		}
		if cmd.Flags().Changed("match-type") {
			req.MatchType = &updateMatchType
		}
		if cmd.Flags().Changed("role") {
			req.RoleID = &updateRoleID
		}
		if cmd.Flags().Changed("environment") {
			env, _ := cmd.Flags().GetString("environment")
			if env != "" {
				req.EnvironmentID = &env
			}
		}
		if cmd.Flags().Changed("token-ttl") {
			req.TokenTTLSeconds = &updateTokenTTL
		}
		if cmd.Flags().Changed("expires-at") && updateExpiresAt != "" {
			parsed, err := time.Parse(time.RFC3339, updateExpiresAt)
			if err != nil {
				return errors.WrapIf(err, "invalid --expires-at format (use RFC3339)")
			}
			req.ExpiresAt = &parsed
		}

		resp, err := c.Put(cmd.Context(), types.FederatedCredential(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update federated credential")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[federatedtypes.FederatedCredential]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to update federated credential")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Federated credential updated")
		printCredentialInternal(result.Data)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <credential-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a federated credential",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Delete federated credential %s and its service user?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.FederatedCredential(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete federated credential")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete federated credential")
		}

		output.Success("Federated credential deleted")
		return nil
	},
}

func printCredentialInternal(cred federatedtypes.FederatedCredential) {
	output.KeyValue("ID", cred.ID)
	output.KeyValue("Name", cred.Name)
	if cred.Description != nil && *cred.Description != "" {
		output.KeyValue("Description", *cred.Description)
	}
	output.KeyValue("Enabled", cred.Enabled)
	output.KeyValue("Issuer URL", cred.IssuerURL)
	output.KeyValue("Audiences", cred.Audiences)
	output.KeyValue("Subject claim", cred.SubjectClaim)
	output.KeyValue("Subject match", cred.SubjectMatch)
	output.KeyValue("Match type", cred.MatchType)
	role := cred.RoleName
	if role == "" {
		role = cred.RoleID
	}
	output.KeyValue("Role", role)
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

func init() {
	FederatedCredentialsCmd.AddCommand(listCmd)
	FederatedCredentialsCmd.AddCommand(getCmd)
	FederatedCredentialsCmd.AddCommand(createCmd)
	FederatedCredentialsCmd.AddCommand(updateCmd)
	FederatedCredentialsCmd.AddCommand(deleteCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of federated credentials to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&createName, "name", "", "Display name (required)")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Description")
	createCmd.Flags().BoolVar(&createDisabled, "disabled", false, "Create the trust rule in disabled state")
	createCmd.Flags().StringVar(&createIssuerURL, "issuer-url", "", "Trusted external OIDC issuer URL (required)")
	createCmd.Flags().StringArrayVar(&createAudiences, "audience", nil, "Allowed external token audience (repeatable, required)")
	createCmd.Flags().StringVar(&createSubjectClaim, "subject-claim", "", "Claim path to match against (defaults to sub)")
	createCmd.Flags().StringVar(&createSubjectMatch, "subject-match", "", "Exact subject or anchored glob pattern (required)")
	createCmd.Flags().StringVar(&createMatchType, "match-type", "", "Subject match strategy: exact or glob")
	createCmd.Flags().StringVar(&createRoleID, "role", "", "Role ID to map exchanged tokens to (required)")
	createCmd.Flags().StringVar(&createEnvironment, "environment", "", "Scope the role assignment to one environment (omit for global)")
	createCmd.Flags().IntVar(&createTokenTTL, "token-ttl", 0, "Issued token lifetime in seconds (60-3600)")
	createCmd.Flags().StringVar(&createExpiresAt, "expires-at", "", "Credential expiration (RFC3339 format)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("issuer-url")
	_ = createCmd.MarkFlagRequired("audience")
	_ = createCmd.MarkFlagRequired("subject-match")
	_ = createCmd.MarkFlagRequired("role")

	updateCmd.Flags().StringVar(&updateName, "name", "", "New display name")
	updateCmd.Flags().StringVar(&updateDescription, "description", "", "New description")
	updateCmd.Flags().BoolVar(&updateEnabled, "enabled", false, "Enable the trust rule")
	updateCmd.Flags().BoolVar(&updateDisabled, "disabled", false, "Disable the trust rule")
	updateCmd.Flags().StringVar(&updateIssuerURL, "issuer-url", "", "New issuer URL")
	updateCmd.Flags().StringArrayVar(&updateAudiences, "audience", nil, "Replace allowed audiences (repeatable)")
	updateCmd.Flags().StringVar(&updateSubjectClaim, "subject-claim", "", "New claim path to match against")
	updateCmd.Flags().StringVar(&updateSubjectMatch, "subject-match", "", "New subject match pattern")
	updateCmd.Flags().StringVar(&updateMatchType, "match-type", "", "New match strategy: exact or glob")
	updateCmd.Flags().StringVar(&updateRoleID, "role", "", "New role ID")
	updateCmd.Flags().String("environment", "", "New environment scope")
	updateCmd.Flags().IntVar(&updateTokenTTL, "token-ttl", 0, "New issued token lifetime in seconds (60-3600)")
	updateCmd.Flags().StringVar(&updateExpiresAt, "expires-at", "", "New credential expiration (RFC3339 format)")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
}
