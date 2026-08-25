package backups

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/spf13/cobra"
)

var (
	s3Name            string
	s3Endpoint        string
	s3Bucket          string
	s3Region          string
	s3AccessKeyID     string
	s3SecretAccessKey string
	s3Prefix          string
	s3UseSSL          bool
	s3ForcePathStyle  bool

	s3GetInUse bool
)

var s3Cmd = &cobra.Command{
	Use:   "s3",
	Short: "Manage S3 backup destinations",
}

var s3ListCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List S3 backup destinations",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.BackupsS3(), cmdutil.ListParams{
			Resource:        "s3 destinations",
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
			return errors.WrapIf(err, "failed to list S3 destinations")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list S3 destinations")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[backup.S3Destination]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Table(s3TableHeaders(), s3TableRows(result.Data))
		output.Showing(len(result.Data), result.Pagination.TotalItems, "S3 destinations")
		return nil
	},
}

func s3TableHeaders() []string {
	return []string{"ID", "NAME", "BUCKET", "REGION", "ENDPOINT", "SSL", "SECRET"}
}

func s3TableRows(destinations []backup.S3Destination) [][]string {
	rows := make([][]string, len(destinations))
	for i, dest := range destinations {
		secret := "Not set"
		if dest.SecretConfigured {
			secret = "Set"
		}
		rows[i] = []string{
			dest.ID,
			dest.Name,
			dest.Bucket,
			dest.Region,
			dest.Endpoint,
			strconv.FormatBool(dest.UseSSL),
			secret,
		}
	}
	return rows
}

var s3GetCmd = &cobra.Command{
	Use:          "get <destination>",
	Short:        "Get S3 destination details by name or ID",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resolved, err := resolveS3Destination(cmd.Context(), c, args[0], !cmdutil.JSONOutputEnabled(cmd))
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.BackupsS3Destination(resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to get S3 destination")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get S3 destination")
		}

		if !s3GetInUse {
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintRawJSON(body)
			}
			var dest backup.S3Destination
			if err := json.Unmarshal(body, &dest); err != nil {
				return errors.WrapIf(err, "failed to parse response")
			}
			printS3Destination(dest)
			return nil
		}

		var dest backup.S3Destination
		if err := json.Unmarshal(body, &dest); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		usageResp, err := c.Get(cmd.Context(), types.BackupsS3DestinationInUse(resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to check S3 destination usage")
		}
		defer func() { _ = usageResp.Body.Close() }()

		var usage struct {
			InUse bool `json:"inUse"`
		}
		if err := cmdutil.DecodeJSON(usageResp, &usage); err != nil {
			return err
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(map[string]any{"destination": dest, "inUse": usage.InUse})
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

func printS3Destination(dest backup.S3Destination) {
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

func s3DestinationFromFlags() backup.CreateS3Destination {
	return backup.CreateS3Destination{
		Name:            s3Name,
		Endpoint:        s3Endpoint,
		Bucket:          s3Bucket,
		Region:          s3Region,
		AccessKeyID:     s3AccessKeyID,
		SecretAccessKey: s3SecretAccessKey,
		Prefix:          s3Prefix,
		UseSSL:          s3UseSSL,
		ForcePathStyle:  s3ForcePathStyle,
	}
}

var s3CreateCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create an S3 backup destination",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.BackupsS3(), s3DestinationFromFlags())
		if err != nil {
			return errors.WrapIf(err, "failed to create S3 destination")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to create S3 destination")
		}

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to create S3 destination")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var dest backup.S3Destination
		if err := json.Unmarshal(body, &dest); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Success("S3 destination %s created successfully", dest.Name)
		printS3Destination(dest)
		return nil
	},
}

var s3UpdateCmd = &cobra.Command{
	Use:          "update <destination>",
	Short:        "Update an S3 backup destination by name or ID",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resolved, err := resolveS3Destination(cmd.Context(), c, args[0], !cmdutil.JSONOutputEnabled(cmd))
		if err != nil {
			return err
		}

		resp, err := c.Put(cmd.Context(), types.BackupsS3Destination(resolved.ID), s3DestinationFromFlags())
		if err != nil {
			return errors.WrapIf(err, "failed to update S3 destination")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update S3 destination")
		}

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to update S3 destination")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var dest backup.S3Destination
		if err := json.Unmarshal(body, &dest); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Success("S3 destination %s updated successfully", dest.Name)
		printS3Destination(dest)
		return nil
	},
}

var s3DeleteCmd = &cobra.Command{
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

		resolved, err := resolveS3Destination(cmd.Context(), c, args[0], !forceFlag && !cmdutil.JSONOutputEnabled(cmd))
		if err != nil {
			return err
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete S3 destination %s?", resolved.Name))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.Delete(cmd.Context(), types.BackupsS3Destination(resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to delete S3 destination")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete S3 destination")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			body, err := cmdutil.ReadJSONBody(resp)
			if err != nil {
				return errors.WrapIf(err, "failed to delete S3 destination")
			}
			return cmdutil.PrintRawJSON(body)
		}

		output.Success("S3 destination %s deleted successfully", resolved.Name)
		return nil
	},
}

var s3TestCmd = &cobra.Command{
	Use:   "test [destination]",
	Short: "Test an S3 destination connection",
	Long: `Test a saved S3 destination by name or ID, or an unsaved configuration by
passing the connection flags (--name, --bucket, --region, ...) with no argument.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		connectionFlagsSet := false
		for _, name := range []string{"name", "endpoint", "bucket", "region", "access-key-id", "secret-access-key", "prefix", "use-ssl", "force-path-style"} {
			if cmd.Flags().Changed(name) {
				connectionFlagsSet = true
				break
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		var resp *http.Response
		if len(args) == 1 {
			if connectionFlagsSet {
				return errors.New("pass either a saved destination name or ID, or the connection flags, not both")
			}
			resolved, err := resolveS3Destination(cmd.Context(), c, args[0], !cmdutil.JSONOutputEnabled(cmd))
			if err != nil {
				return err
			}
			resp, err = c.Post(cmd.Context(), types.BackupsS3DestinationTest(resolved.ID), nil)
			if err != nil {
				return errors.WrapIf(err, "failed to test S3 destination")
			}
		} else {
			if s3Bucket == "" {
				return errors.New("either a destination name or ID, or connection flags (--name, --bucket, --region, ...) are required")
			}
			resp, err = c.Post(cmd.Context(), types.BackupsS3Test(), s3DestinationFromFlags())
			if err != nil {
				return errors.WrapIf(err, "failed to test S3 destination")
			}
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to test S3 destination")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			body, err := cmdutil.ReadJSONBody(resp)
			if err != nil {
				return errors.WrapIf(err, "failed to test S3 destination")
			}
			return cmdutil.PrintRawJSON(body)
		}

		output.Success("S3 connection test succeeded")
		return nil
	},
}

func addS3ConnectionFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s3Name, "name", "", "Destination name")
	cmd.Flags().StringVar(&s3Endpoint, "endpoint", "", "S3 endpoint (empty for AWS S3)")
	cmd.Flags().StringVar(&s3Bucket, "bucket", "", "Bucket name")
	cmd.Flags().StringVar(&s3Region, "region", "", "Region")
	cmd.Flags().StringVar(&s3AccessKeyID, "access-key-id", "", "Access key ID")
	cmd.Flags().StringVar(&s3SecretAccessKey, "secret-access-key", "", "Secret access key")
	cmd.Flags().StringVar(&s3Prefix, "prefix", "", "Object key prefix")
	cmd.Flags().BoolVar(&s3UseSSL, "use-ssl", true, "Use SSL for the connection")
	cmd.Flags().BoolVar(&s3ForcePathStyle, "force-path-style", false, "Use path-style bucket addressing")
}

func init() {
	BackupsCmd.AddCommand(s3Cmd)

	s3Cmd.AddCommand(s3ListCmd)
	s3Cmd.AddCommand(s3GetCmd)
	s3Cmd.AddCommand(s3CreateCmd)
	s3Cmd.AddCommand(s3UpdateCmd)
	s3Cmd.AddCommand(s3DeleteCmd)
	s3Cmd.AddCommand(s3TestCmd)

	s3ListCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of destinations to show")
	s3ListCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	s3ListCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	s3ListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	s3GetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	s3GetCmd.Flags().BoolVar(&s3GetInUse, "in-use", false, "Also report whether the destination is still referenced")

	addS3ConnectionFlags(s3CreateCmd)
	s3CreateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = s3CreateCmd.MarkFlagRequired("name")
	_ = s3CreateCmd.MarkFlagRequired("bucket")
	_ = s3CreateCmd.MarkFlagRequired("region")
	_ = s3CreateCmd.MarkFlagRequired("access-key-id")
	_ = s3CreateCmd.MarkFlagRequired("secret-access-key")

	addS3ConnectionFlags(s3UpdateCmd)
	s3UpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = s3UpdateCmd.MarkFlagRequired("name")
	_ = s3UpdateCmd.MarkFlagRequired("bucket")
	_ = s3UpdateCmd.MarkFlagRequired("region")
	_ = s3UpdateCmd.MarkFlagRequired("access-key-id")

	addS3ConnectionFlags(s3TestCmd)
	s3TestCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	s3DeleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Delete without confirmation")
	s3DeleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
