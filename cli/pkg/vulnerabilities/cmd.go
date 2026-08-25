// Package vulnerabilities provides CLI commands for image vulnerability
// scanning on Arcane servers.
//
// This package implements the "arcane vulnerabilities" command group, which
// includes subcommands for checking scanner status, viewing environment-wide
// summaries, listing vulnerabilities, scanning images, and managing ignore
// records.
package vulnerabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
	"github.com/spf13/cobra"
)

var (
	jsonOutput   bool
	limitFlag    int
	startFlag    int
	allFlag      bool
	forceFlag    bool
	severityFlag string
	imageFlag    string
	summaryFlag  bool

	ignoreImageFlag   string
	ignorePkgFlag     string
	ignoreVersionFlag string
	ignoreReasonFlag  string
)

// VulnerabilitiesCmd is the parent command for vulnerability operations
var VulnerabilitiesCmd = &cobra.Command{
	Use:     "vulnerabilities",
	Aliases: []string{"vulns", "vuln", "cve"},
	Short:   "Manage image vulnerability scans",
}

var statusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show vulnerability scanner status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.VulnerabilitiesScannerStatus(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get scanner status")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to get scanner status")
		}

		var result base.ApiResponse[struct {
			Available bool   `json:"available"`
			Version   string `json:"version,omitempty"`
		}]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Vulnerability Scanner")
		output.KeyValue("Available", result.Data.Available)
		if result.Data.Version != "" {
			output.KeyValue("Version", result.Data.Version)
		}
		return nil
	},
}

var summaryCmd = &cobra.Command{
	Use:          "summary",
	Short:        "Show environment-wide vulnerability summary",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.VulnerabilitiesSummary(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get vulnerability summary")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to get vulnerability summary")
		}

		var result base.ApiResponse[vulnerability.EnvironmentVulnerabilitySummary]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Vulnerability Summary")
		output.KeyValue("Total Images", result.Data.TotalImages)
		output.KeyValue("Scanned Images", result.Data.ScannedImages)
		printSeveritySummary(result.Data.Summary)
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List vulnerabilities across all scanned images",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.VulnerabilitiesAll(c.EnvID()), cmdutil.ListParams{
			Resource:        "vulnerabilities",
			Limit:           limitFlag,
			FallbackDefault: 20,
			Start:           startFlag,
			All:             allFlag,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		query := url.Values{}
		if severityFlag != "" {
			query.Set("severity", strings.ToUpper(severityFlag))
		}
		if imageFlag != "" {
			query.Set("imageName", imageFlag)
		}
		if len(query) > 0 {
			path = cmdutil.AppendQuery(path, query)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list vulnerabilities")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list vulnerabilities")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[vulnerability.VulnerabilityWithImage]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"CVE ID", "SEVERITY", "IMAGE", "PACKAGE", "VERSION", "FIXED IN"}
		rows := make([][]string, len(result.Data))
		for i, item := range result.Data {
			rows[i] = []string{
				item.VulnerabilityID,
				string(item.Severity),
				item.ImageName,
				item.PkgName,
				item.InstalledVersion,
				orDash(item.FixedVersion),
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "vulnerabilities")
		return nil
	},
}

var scanCmd = &cobra.Command{
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

		resp, err := c.Post(cmd.Context(), types.ImageVulnerabilitiesScan(c.EnvID(), imageID), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to scan image")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to scan image")
		}

		var result base.ApiResponse[vulnerability.ScanResult]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Vulnerability Scan")
		output.KeyValue("Image", result.Data.ImageName)
		output.KeyValue("Status", string(result.Data.Status))
		if result.Data.ScanPhase != "" {
			output.KeyValue("Phase", string(result.Data.ScanPhase))
		}
		if result.Data.Error != "" {
			output.KeyValue("Error", result.Data.Error)
		}
		if result.Data.ScannerVersion != "" {
			output.KeyValue("Scanner", result.Data.ScannerVersion)
		}
		printSeveritySummary(result.Data.Summary)
		return nil
	},
}

var imageCmd = &cobra.Command{
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

		if summaryFlag {
			return runImageSummary(cmd, c, imageID)
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.ImageVulnerabilitiesList(c.EnvID(), imageID), cmdutil.ListParams{
			Resource:        "vulnerabilities",
			Limit:           limitFlag,
			FallbackDefault: 20,
			Start:           startFlag,
			All:             allFlag,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		if severityFlag != "" {
			path = cmdutil.AppendQuery(path, url.Values{"severity": []string{strings.ToUpper(severityFlag)}})
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list image vulnerabilities")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list image vulnerabilities")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[vulnerability.Vulnerability]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"CVE ID", "SEVERITY", "PACKAGE", "VERSION", "FIXED IN"}
		rows := make([][]string, len(result.Data))
		for i, item := range result.Data {
			rows[i] = []string{
				item.VulnerabilityID,
				string(item.Severity),
				item.PkgName,
				item.InstalledVersion,
				orDash(item.FixedVersion),
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "vulnerabilities")
		return nil
	},
}

func runImageSummary(cmd *cobra.Command, c *client.Client, imageID string) error {
	resp, err := c.Get(cmd.Context(), types.ImageVulnerabilitiesSummary(c.EnvID(), imageID))
	if err != nil {
		return errors.WrapIf(err, "failed to get image vulnerability summary")
	}
	defer func() { _ = resp.Body.Close() }()
	if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
		return errors.WrapIf(err, "failed to get image vulnerability summary")
	}

	var result base.ApiResponse[vulnerability.ScanSummary]
	if err := cmdutil.DecodeJSON(resp, &result); err != nil {
		return err
	}

	if jsonOutput {
		return cmdutil.PrintJSON(result.Data)
	}

	output.Header("Image Vulnerability Summary")
	output.KeyValue("Image ID", result.Data.ImageID)
	output.KeyValue("Status", string(result.Data.Status))
	output.KeyValue("Scan Time", result.Data.ScanTime.Format(time.RFC3339))
	if result.Data.ScanPhase != "" {
		output.KeyValue("Phase", string(result.Data.ScanPhase))
	}
	if result.Data.Error != "" {
		output.KeyValue("Error", result.Data.Error)
	}
	printSeveritySummary(result.Data.Summary)
	return nil
}

var ignoreCmd = &cobra.Command{
	Use:          "ignore <cve-id>",
	Short:        "Ignore a vulnerability for an image",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		imageID, err := resolveImageID(cmd.Context(), c, ignoreImageFlag)
		if err != nil {
			return err
		}

		payload := vulnerability.IgnorePayload{
			ImageID:          imageID,
			VulnerabilityID:  args[0],
			PkgName:          ignorePkgFlag,
			InstalledVersion: ignoreVersionFlag,
		}
		if ignoreReasonFlag != "" {
			payload.Reason = &ignoreReasonFlag
		}

		resp, err := c.Post(cmd.Context(), types.VulnerabilitiesIgnore(c.EnvID()), payload)
		if err != nil {
			return errors.WrapIf(err, "failed to ignore vulnerability")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to ignore vulnerability")
		}

		var result base.ApiResponse[vulnerability.IgnoredVulnerability]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Vulnerability %s ignored", result.Data.VulnerabilityID)
		output.KeyValue("Ignore ID", result.Data.ID)
		return nil
	},
}

var ignoredCmd = &cobra.Command{
	Use:          "ignored",
	Short:        "List ignored vulnerabilities",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.VulnerabilitiesIgnored(c.EnvID()), cmdutil.ListParams{
			Resource:        "vulnerabilities",
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
			return errors.WrapIf(err, "failed to list ignored vulnerabilities")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list ignored vulnerabilities")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[vulnerability.IgnoredVulnerability]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"ID", "CVE ID", "IMAGE ID", "PACKAGE", "REASON", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, item := range result.Data {
			reason := ""
			if item.Reason != nil {
				reason = *item.Reason
			}
			rows[i] = []string{
				item.ID,
				item.VulnerabilityID,
				shortImageID(item.ImageID),
				item.PkgName,
				orDash(reason),
				item.CreatedAt.Format(time.RFC3339),
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "ignored vulnerabilities")
		return nil
	},
}

var unignoreCmd = &cobra.Command{
	Use:          "unignore <ignore-id>",
	Short:        "Remove a vulnerability ignore record",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to remove ignore record %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.VulnerabilityIgnore(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to remove ignore record")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to remove ignore record")
		}

		output.Success("Ignore record %s removed", args[0])
		return nil
	},
}

func init() {
	VulnerabilitiesCmd.AddCommand(statusCmd)
	VulnerabilitiesCmd.AddCommand(summaryCmd)
	VulnerabilitiesCmd.AddCommand(listCmd)
	VulnerabilitiesCmd.AddCommand(scanCmd)
	VulnerabilitiesCmd.AddCommand(imageCmd)
	VulnerabilitiesCmd.AddCommand(ignoreCmd)
	VulnerabilitiesCmd.AddCommand(ignoredCmd)
	VulnerabilitiesCmd.AddCommand(unignoreCmd)

	statusCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	summaryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of vulnerabilities to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().StringVar(&severityFlag, "severity", "", "Comma-separated severity filter (critical,high,medium,low,unknown)")
	listCmd.Flags().StringVar(&imageFlag, "image", "", "Filter by image name (substring)")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	scanCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	imageCmd.Flags().BoolVar(&summaryFlag, "summary", false, "Show the scan summary instead of the detailed list")
	imageCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of vulnerabilities to show")
	imageCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	imageCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	imageCmd.Flags().StringVar(&severityFlag, "severity", "", "Comma-separated severity filter (critical,high,medium,low,unknown)")
	imageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	ignoreCmd.Flags().StringVar(&ignoreImageFlag, "image", "", "Image containing the vulnerability")
	ignoreCmd.Flags().StringVar(&ignorePkgFlag, "package", "", "Package name containing the vulnerability")
	ignoreCmd.Flags().StringVar(&ignoreVersionFlag, "version", "", "Installed version of the vulnerable package")
	ignoreCmd.Flags().StringVar(&ignoreReasonFlag, "reason", "", "Reason for ignoring this vulnerability")
	ignoreCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = ignoreCmd.MarkFlagRequired("image")
	_ = ignoreCmd.MarkFlagRequired("package")

	ignoredCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of ignore records to show")
	ignoredCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	ignoredCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	ignoredCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	unignoreCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip the confirmation prompt")
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

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func shortImageID(id string) string {
	trimmed := strings.TrimPrefix(id, "sha256:")
	if len(trimmed) > 12 {
		return trimmed[:12]
	}
	return trimmed
}

// resolveImageID resolves an image name, tag, or (partial) ID to a full image
// ID. It first tries a direct lookup and falls back to a server-side search.
func resolveImageID(ctx context.Context, c *client.Client, identifier string) (string, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return "", errors.New("image identifier is required")
	}

	resp, err := c.Get(ctx, types.Image(c.EnvID(), trimmed))
	if err != nil {
		return "", errors.WrapIff(err, "failed to resolve image %q", trimmed)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", errors.WrapIf(err, "failed to read image response")
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[image.DetailSummary]
		if err := json.Unmarshal(body, &result); err != nil {
			return "", errors.WrapIf(err, "failed to parse image response")
		}
		if result.Data.ID == "" {
			return "", errors.Errorf("image lookup for %q returned empty ID", trimmed)
		}
		return result.Data.ID, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return "", errors.Errorf("failed to resolve image %q (status %d): %s", trimmed, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Images(c.EnvID()), url.QueryEscape(trimmed), cmdutil.ShowAllLimit)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return "", errors.WrapIf(err, "failed to search images")
	}

	searchBody, err := cmdutil.ReadJSONBody(searchResp)
	_ = searchResp.Body.Close()
	if err != nil {
		return "", errors.WrapIf(err, "failed to search images")
	}

	var result base.Paginated[image.Summary]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return "", errors.WrapIf(err, "failed to parse images response")
	}

	identifierLower := strings.ToLower(trimmed)
	matches := make([]image.Summary, 0)
	for _, item := range result.Data {
		if imageMatches(item, trimmed, identifierLower) {
			matches = append(matches, item)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0].ID, nil
	case 0:
		return "", errors.Errorf("image %q not found; use the image ID or run `arcane images list`", trimmed)
	default:
		return "", errors.Errorf("multiple images match %q; use the image ID or run `arcane images list`", trimmed)
	}
}

func imageMatches(item image.Summary, trimmed, identifierLower string) bool {
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	for _, tag := range item.RepoTags {
		if strings.EqualFold(tag, trimmed) || strings.Contains(strings.ToLower(tag), identifierLower) {
			return true
		}
	}
	for _, digest := range item.RepoDigests {
		if strings.EqualFold(digest, trimmed) || strings.Contains(strings.ToLower(digest), identifierLower) {
			return true
		}
	}
	return false
}
