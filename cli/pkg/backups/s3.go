package backups

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func newS3Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "s3",
		Short: "Manage S3 backup destinations",
	}
	cmd.AddCommand(newS3ListCmd())
	cmd.AddCommand(newS3GetCmd())
	cmd.AddCommand(newS3CreateCmd())
	cmd.AddCommand(newS3UpdateCmd())
	cmd.AddCommand(newS3DeleteCmd())
	cmd.AddCommand(newS3TestCmd())
	return cmd
}

func newS3ListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List S3 backup destinations",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.S3Destination]{
				Resource: "S3 destinations",
				Endpoint: clitypes.BackupsS3(),
				Params:   cmdutil.ListParams{Resource: "s3 destinations", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "BUCKET", "REGION", "ENDPOINT", "SSL", "SECRET"},
				Row: func(dest types.S3Destination) []string {
					return []string{
						dest.ID,
						dest.Name,
						dest.Bucket,
						dest.Region,
						dest.Endpoint,
						strconv.FormatBool(dest.UseSSL),
						kit.Ternary(dest.SecretConfigured, "Set", "Not set"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of destinations to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return cmd
}

func newS3GetCmd() *cobra.Command {
	var opts struct {
		inUse bool
	}
	cmd := &cobra.Command{
		Use:          "get <destination>",
		Short:        "Get S3 destination details by name or ID",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, args[0], cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}
			dest := *resolved

			if !opts.inUse {
				if cmdutil.JSONOutputEnabled(cmd) {
					return cmdutil.PrintJSON(dest)
				}
				printS3Destination(dest)
				return nil
			}

			usage, err := c.DoJSON[types.S3DestinationUsage](cmd.Context(), http.MethodGet, clitypes.BackupsS3DestinationInUse(resolved.ID), nil)
			if err != nil {
				return fmt.Errorf("failed to check S3 destination usage: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Destination types.S3Destination `json:"destination"`
					InUse       bool                `json:"inUse"`
				}{Destination: dest, InUse: usage.InUse})
			}

			printS3Destination(dest)
			if usage.InUse {
				output.KeyValue("In Use", "Yes (referenced by backups, policies, or settings)")
			} else {
				output.KeyValue("In Use", "No")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.inUse, "in-use", false, "Also report whether the destination is still referenced")
	return cmd
}

func printS3Destination(dest types.S3Destination) {
	output.Header("S3 Destination Details")
	output.KeyValue("ID", dest.ID)
	output.KeyValue("Name", dest.Name)
	output.KeyValue("Bucket", dest.Bucket)
	output.KeyValue("Region", dest.Region)
	if dest.Endpoint != "" {
		output.KeyValue("Endpoint", dest.Endpoint)
	}
	if dest.Prefix != "" {
		output.KeyValue("Prefix", dest.Prefix)
	}
	output.KeyValue("Access Key ID", dest.AccessKeyID)
	output.KeyValue("Secret Configured", dest.SecretConfigured)
	output.KeyValue("Use SSL", dest.UseSSL)
	output.KeyValue("Force Path Style", dest.ForcePathStyle)
	output.KeyValue("Created", dest.CreatedAt.Format("2006-01-02 15:04"))
}

func newS3CreateCmd() *cobra.Command {
	var opts types.S3Destination
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create an S3 backup destination",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			dest, err := c.DoJSON[types.S3Destination](cmd.Context(), http.MethodPost, clitypes.BackupsS3(), opts)
			if err != nil {
				return fmt.Errorf("failed to create S3 destination: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(dest)
			}

			output.Success("S3 destination %s created successfully", dest.Name)
			printS3Destination(dest)
			return nil
		},
	}
	addS3ConnectionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("bucket")
	_ = cmd.MarkFlagRequired("region")
	_ = cmd.MarkFlagRequired("access-key-id")
	_ = cmd.MarkFlagRequired("secret-access-key")
	return cmd
}

func newS3UpdateCmd() *cobra.Command {
	var opts types.S3Destination
	cmd := &cobra.Command{
		Use:          "update <destination>",
		Short:        "Update an S3 backup destination by name or ID",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, args[0], cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}

			req := map[string]any{}
			for _, field := range []struct {
				flag  string
				key   string
				value any
			}{
				{flag: "name", key: "name", value: opts.Name},
				{flag: "endpoint", key: "endpoint", value: opts.Endpoint},
				{flag: "bucket", key: "bucket", value: opts.Bucket},
				{flag: "region", key: "region", value: opts.Region},
				{flag: "access-key-id", key: "accessKeyId", value: opts.AccessKeyID},
				{flag: "secret-access-key", key: "secretAccessKey", value: opts.SecretAccessKey},
				{flag: "prefix", key: "prefix", value: opts.Prefix},
				{flag: "use-ssl", key: "useSsl", value: opts.UseSSL},
				{flag: "force-path-style", key: "forcePathStyle", value: opts.ForcePathStyle},
			} {
				if cmd.Flags().Changed(field.flag) {
					req[field.key] = field.value
				}
			}
			if len(req) == 0 {
				return errors.New("no updates provided (set at least one flag)")
			}

			dest, err := c.DoJSON[types.S3Destination](cmd.Context(), http.MethodPatch, clitypes.BackupsS3Destination(resolved.ID), req)
			if err != nil {
				return fmt.Errorf("failed to update S3 destination: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(dest)
			}

			output.Success("S3 destination %s updated successfully", dest.Name)
			printS3Destination(dest)
			return nil
		},
	}
	addS3ConnectionFlags(cmd, &opts)
	return cmd
}

func newS3DeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <destination>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete an S3 backup destination by name or ID",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := s3DestinationRef.Resolve(cmd.Context(), c, args[0], !opts.force && cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete S3 destination %s?", resolved.Name),
				Path:           clitypes.BackupsS3Destination(resolved.ID),
				Body:           nil,
				FailureMessage: "failed to delete S3 destination",
				SuccessMessage: fmt.Sprintf("S3 destination %s deleted successfully", resolved.Name),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Delete without confirmation")
	return cmd
}

func newS3TestCmd() *cobra.Command {
	var opts types.S3Destination
	cmd := &cobra.Command{
		Use:   "test [destination]",
		Short: "Test an S3 destination connection",
		Long: `Test a saved S3 destination by name or ID, or an unsaved configuration by
passing the connection flags (--name, --bucket, --region, ...) with no argument.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			flagNames := []string{"name", "endpoint", "bucket", "region", "access-key-id", "secret-access-key", "prefix", "use-ssl", "force-path-style"}
			connectionFlagsSet := slices.ContainsFunc(flagNames, cmd.Flags().Changed)

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			testPath := clitypes.BackupsS3Test()
			var testBody any = opts
			if len(args) == 1 {
				if connectionFlagsSet {
					return errors.New("pass either a saved destination name or ID, or the connection flags, not both")
				}
				resolved, _, resolveErr := s3DestinationRef.Resolve(cmd.Context(), c, args[0], cmdutil.AllowPrompt(cmd))
				if resolveErr != nil {
					return resolveErr
				}
				testPath, testBody = clitypes.BackupsS3DestinationAction(resolved.ID, "test"), nil
			} else if opts.Bucket == "" {
				return errors.New("either a destination name or ID, or connection flags (--name, --bucket, --region, ...) are required")
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           testPath,
				Body:           testBody,
				FailureMessage: "failed to test S3 destination",
				Render:         func(types.MessageResponse) error { output.Success("%s", "S3 connection test succeeded"); return nil },
			})
		},
	}
	addS3ConnectionFlags(cmd, &opts)
	return cmd
}

func addS3ConnectionFlags(cmd *cobra.Command, opts *types.S3Destination) {
	cmd.Flags().StringVar(&opts.Name, "name", "", "Destination name")
	cmd.Flags().StringVar(&opts.Endpoint, "endpoint", "", "S3 endpoint (empty for AWS S3)")
	cmd.Flags().StringVar(&opts.Bucket, "bucket", "", "Bucket name")
	cmd.Flags().StringVar(&opts.Region, "region", "", "Region")
	cmd.Flags().StringVar(&opts.AccessKeyID, "access-key-id", "", "Access key ID")
	cmd.Flags().StringVar(&opts.SecretAccessKey, "secret-access-key", "", "Secret access key")
	cmd.Flags().StringVar(&opts.Prefix, "prefix", "", "Object key prefix")
	cmd.Flags().BoolVar(&opts.UseSSL, "use-ssl", true, "Use SSL for the connection")
	cmd.Flags().BoolVar(&opts.ForcePathStyle, "force-path-style", false, "Use path-style bucket addressing")
}
