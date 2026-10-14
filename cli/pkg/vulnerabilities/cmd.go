// Package vulnerabilities provides image vulnerability scanning commands.
package vulnerabilities

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/images"
)

// Command builds the vulnerabilities command tree.
func Command() *cobra.Command {
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeResource},
		Use:         "vulnerabilities",
		Aliases:     []string{"vulns", "vuln", "cve"},
		Short:       "Manage image vulnerability scans",
	}
	command.AddCommand(newStatusCmd(), newSummaryCmd(), newListCmd(), newScanCmd(), newImageCmd(), newIgnoreCmd(), newIgnoredCmd(), newUnignoreCmd())
	return command
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeRead},
		Use:          "status",
		Short:        "Show vulnerability scanner status",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[vulnerability.ScannerStatus]{
				Method:         http.MethodGet,
				Path:           types.VulnerabilitiesScannerStatus(c.EnvID()),
				FailureMessage: "failed to get scanner status",
				Render: func(data vulnerability.ScannerStatus) error {
					output.Header("Vulnerability Scanner")
					output.KeyValue("Available", data.Available)
					if data.Version != "" {
						output.KeyValue("Version", data.Version)
					}
					return nil
				},
			})
		},
	}
}

func newSummaryCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeRead},
		Use:          "summary",
		Short:        "Show environment-wide vulnerability summary",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[vulnerability.EnvironmentVulnerabilitySummary]{
				Method:         http.MethodGet,
				Path:           types.VulnerabilitiesSummary(c.EnvID()),
				FailureMessage: "failed to get vulnerability summary",
				Render: func(data vulnerability.EnvironmentVulnerabilitySummary) error {
					output.Header("Vulnerability Summary")
					output.KeyValue("Total Images", data.TotalImages)
					output.KeyValue("Scanned Images", data.ScannedImages)
					printSeveritySummary(data.Summary)
					return nil
				},
			})
		},
	}
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit    int
		start    int
		all      bool
		severity string
		image    string
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeMerged},
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List vulnerabilities across all scanned images",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			query := url.Values{}
			if opts.severity != "" {
				query.Set("severity", strings.ToUpper(opts.severity))
			}
			if opts.image != "" {
				query.Set("imageName", opts.image)
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[vulnerability.VulnerabilityWithImage]{
				Resource: "vulnerabilities",
				Endpoint: types.VulnerabilitiesAll(c.EnvID()),
				Params:   cmdutil.ListParams{Resource: "vulnerabilities", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Query:    query,
				Headers:  []string{"CVE ID", "SEVERITY", "IMAGE", "PACKAGE", "VERSION", "FIXED IN"},
				Row: func(item vulnerability.VulnerabilityWithImage) []string {
					return []string{
						item.VulnerabilityID,
						string(item.Severity),
						item.ImageName,
						item.PkgName,
						item.InstalledVersion,
						cmp.Or(item.FixedVersion, "-"),
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of vulnerabilities to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	command.Flags().StringVar(&opts.severity, "severity", "", "Comma-separated severity filter (critical,high,medium,low,unknown)")
	command.Flags().StringVar(&opts.image, "image", "", "Filter by image name (substring)")
	return command
}

func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "scan <image>",
		Short:        "Scan an image for vulnerabilities",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			imageID, err := resolveImageID(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}

			// Scans can take a long time on large images.
			c.SetTimeout(30 * time.Minute)

			return cmdutil.Run(cmd, c, cmdutil.Spec[vulnerability.ScanResult]{
				Method:         http.MethodPost,
				Path:           types.ImageVulnerabilitiesScan(c.EnvID(), imageID),
				Body:           nil,
				FailureMessage: "failed to scan image",
				Render: func(data vulnerability.ScanResult) error {
					output.Header("Vulnerability Scan")
					output.KeyValue("Image", data.ImageName)
					output.KeyValue("Status", string(data.Status))
					if data.ScanPhase != "" {
						output.KeyValue("Phase", string(data.ScanPhase))
					}
					if data.Error != "" {
						output.KeyValue("Error", data.Error)
					}
					if data.ScannerVersion != "" {
						output.KeyValue("Scanner", data.ScannerVersion)
					}
					printSeveritySummary(data.Summary)
					return nil
				},
			})
		},
	}
}

func newImageCmd() *cobra.Command {
	var opts struct {
		limit    int
		start    int
		all      bool
		severity string
		summary  bool
	}
	command := &cobra.Command{
		Use:          "image <image>",
		Short:        "List vulnerabilities for an image",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			imageID, err := resolveImageID(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}

			if opts.summary {
				return cmdutil.Run(cmd, c, cmdutil.Spec[vulnerability.ScanSummary]{
					Method:         http.MethodGet,
					Path:           types.ImageVulnerabilitiesSummary(c.EnvID(), imageID),
					FailureMessage: "failed to get image vulnerability summary",
					Render: func(data vulnerability.ScanSummary) error {
						output.Header("Image Vulnerability Summary")
						output.KeyValue("Image ID", data.ImageID)
						output.KeyValue("Status", string(data.Status))
						output.KeyValue("Scan Time", data.ScanTime.Format(time.RFC3339))
						if data.ScanPhase != "" {
							output.KeyValue("Phase", string(data.ScanPhase))
						}
						if data.Error != "" {
							output.KeyValue("Error", data.Error)
						}
						printSeveritySummary(data.Summary)
						return nil
					},
				})
			}

			query := url.Values{}
			if opts.severity != "" {
				query.Set("severity", strings.ToUpper(opts.severity))
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[vulnerability.Vulnerability]{
				Resource: "vulnerabilities",
				Endpoint: types.ImageVulnerabilitiesList(c.EnvID(), imageID),
				Params:   cmdutil.ListParams{Resource: "vulnerabilities", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Query:    query,
				Headers:  []string{"CVE ID", "SEVERITY", "PACKAGE", "VERSION", "FIXED IN"},
				Row: func(item vulnerability.Vulnerability) []string {
					return []string{
						item.VulnerabilityID,
						string(item.Severity),
						item.PkgName,
						item.InstalledVersion,
						cmp.Or(item.FixedVersion, "-"),
					}
				},
			})
		},
	}
	command.Flags().BoolVar(&opts.summary, "summary", false, "Show the scan summary instead of the detailed list")
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of vulnerabilities to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	command.Flags().StringVar(&opts.severity, "severity", "", "Comma-separated severity filter (critical,high,medium,low,unknown)")
	return command
}

func newIgnoreCmd() *cobra.Command {
	var opts struct {
		image   string
		pkg     string
		version string
		reason  string
	}
	command := &cobra.Command{
		Use:          "ignore <cve-id>",
		Short:        "Ignore a vulnerability for an image",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			imageID, err := resolveImageID(cmd.Context(), c, opts.image)
			if err != nil {
				return err
			}

			payload := vulnerability.IgnorePayload{
				ImageID:          imageID,
				VulnerabilityID:  args[0],
				PkgName:          opts.pkg,
				InstalledVersion: opts.version,
				Reason:           kit.Ternary(cmd.Flags().Changed("reason"), &opts.reason, nil),
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[vulnerability.IgnoredVulnerability]{
				Method:         http.MethodPost,
				Path:           types.VulnerabilitiesIgnore(c.EnvID()),
				Body:           payload,
				FailureMessage: "failed to ignore vulnerability",
				Render: func(data vulnerability.IgnoredVulnerability) error {
					output.Success("Vulnerability %s ignored", data.VulnerabilityID)
					output.KeyValue("Ignore ID", data.ID)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.image, "image", "", "Image containing the vulnerability")
	command.Flags().StringVar(&opts.pkg, "package", "", "Package name containing the vulnerability")
	command.Flags().StringVar(&opts.version, "version", "", "Installed version of the vulnerable package")
	command.Flags().StringVar(&opts.reason, "reason", "", "Reason for ignoring this vulnerability")
	_ = command.MarkFlagRequired("image")
	_ = command.MarkFlagRequired("package")
	return command
}

func newIgnoredCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeRead},
		Use:          "ignored",
		Short:        "List ignored vulnerabilities",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[vulnerability.IgnoredVulnerability]{
				Resource: "ignored vulnerabilities",
				Endpoint: types.VulnerabilitiesIgnored(c.EnvID()),
				Params:   cmdutil.ListParams{Resource: "vulnerabilities", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "CVE ID", "IMAGE ID", "PACKAGE", "REASON", "CREATED"},
				Row: func(item vulnerability.IgnoredVulnerability) []string {
					imageID := strings.TrimPrefix(item.ImageID, "sha256:")
					imageID = imageID[:min(len(imageID), 12)]
					return []string{
						item.ID,
						item.VulnerabilityID,
						imageID,
						item.PkgName,
						cmp.Or(kit.FromPtr(item.Reason), "-"),
						item.CreatedAt.Format(time.RFC3339),
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of ignore records to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newUnignoreCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "unignore <ignore-id>",
		Short:        "Remove a vulnerability ignore record",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to remove ignore record %s?", args[0]),
				Path:           types.VulnerabilityIgnore(c.EnvID(), args[0]),
				FailureMessage: "failed to remove ignore record",
				SuccessMessage: fmt.Sprintf("Ignore record %s removed", args[0]),
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Skip the confirmation prompt")
	cmdutil.SetResource(ignoreRef, command)
	return command
}

func printSeveritySummary(s *vulnerability.SeveritySummary) {
	if s == nil {
		output.Info("No vulnerability data available")
		return
	}
	output.KeyValue("Critical", s.Critical)
	output.KeyValue("High", s.High)
	output.KeyValue("Medium", s.Medium)
	output.KeyValue("Low", s.Low)
	output.KeyValue("Unknown", s.Unknown)
	output.KeyValue("Total", s.Total)
}

// resolveImageID resolves an image name, tag, or (partial) ID to a full image
// ID via the shared images resolver.
func resolveImageID(ctx context.Context, c *client.Client, identifier string) (string, error) {
	resolved, _, err := images.ImageRef.Resolve(ctx, c, identifier, false)
	if err != nil {
		return "", err
	}
	return resolved.ID, nil
}

var ignoreRef = cmdutil.ResourceRef[vulnerability.IgnoredVulnerability, vulnerability.IgnoredVulnerability]{
	EnvironmentScoped: true,
	Singular:          "ignore", Plural: "ignores",
	IDHint: "the identifier", ListCmd: "arcane-cli images vulnerabilities ignored",
	GetPath: nil, ListPath: types.VulnerabilitiesIgnored,
	Matches: func(item vulnerability.IgnoredVulnerability, lower, original string) bool {
		return strings.EqualFold(item.ID, original)
	},
	Label: func(item vulnerability.IgnoredVulnerability) string {
		return item.VulnerabilityID + " (" + item.ID + ")"
	},
	Promote:      func(item vulnerability.IgnoredVulnerability) *vulnerability.IgnoredVulnerability { return &item },
	IdentifierOf: func(item vulnerability.IgnoredVulnerability) string { return item.ID },
}
