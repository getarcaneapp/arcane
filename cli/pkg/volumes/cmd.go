package volumes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/spf13/cobra"
)

var (
	limitFlag           int
	startFlag           int
	allFlag             bool
	forceFlag           bool
	jsonOutput          bool
	inUseOnlyFlag       bool
	unusedOnlyFlag      bool
	includeInternalFlag bool
)

var (
	volumeCreateName   string
	volumeCreateDriver string
	volumeCreateOpts   []string
	volumeCreateLabels []string
)

const maxPromptOptions = 20

// VolumesCmd is the parent command for volume operations
var VolumesCmd = &cobra.Command{
	Use:     "volumes",
	Aliases: []string{"volume", "vol", "v"},
	Short:   "Manage volumes",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List volumes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if inUseOnlyFlag && unusedOnlyFlag {
			return errors.New("--inuse and --unused cannot be used together")
		}
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.Endpoints.Volumes(c.EnvID()), cmdutil.ListParams{
			Resource:        "volumes",
			Limit:           limitFlag,
			FallbackDefault: 20,
			Start:           startFlag,
			All:             allFlag,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		// Filter server-side so that pagination and the reported totals apply to
		// the matching set rather than to whichever page happened to be fetched.
		query := url.Values{}
		if inUseOnlyFlag {
			query.Set("inUse", "true")
		}
		if unusedOnlyFlag {
			query.Set("inUse", "false")
		}
		if includeInternalFlag {
			query.Set("includeInternal", "true")
		}
		if len(query) > 0 {
			path = cmdutil.AppendQuery(path, query)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list volumes")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list volumes")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[volume.Volume]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"NAME", "DRIVER", "MOUNTPOINT", "CREATED", "IN USE"}
		rows := make([][]string, len(result.Data))
		for i, vol := range result.Data {
			inUse := "No"
			if vol.InUse {
				inUse = "Yes"
			}
			rows[i] = []string{
				vol.Name,
				vol.Driver,
				vol.Mountpoint,
				vol.CreatedAt,
				inUse,
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "volumes")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <volume-name>",
	Short:        "Get volume details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(resolved, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Volume Details")
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("Driver", resolved.Driver)
		output.KeyValue("Mountpoint", resolved.Mountpoint)
		output.KeyValue("Scope", resolved.Scope)
		output.KeyValue("Created", resolved.CreatedAt)
		output.KeyValue("In Use", resolved.InUse)
		if resolved.Size > 0 {
			output.KeyValue("Size", output.Bytes(resolved.Size))
		}
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <volume-name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a volume",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !forceFlag && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete volume %s?", resolved.Name))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		deletePath := cmdutil.AppendQuery(
			types.Endpoints.Volume(c.EnvID(), resolved.Name),
			url.Values{"force": []string{strconv.FormatBool(forceFlag)}},
		)
		resp, err := c.Delete(cmd.Context(), deletePath)
		if err != nil {
			return errors.WrapIf(err, "failed to delete volume")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete volume")
		}

		output.Success("Volume %s deleted successfully", resolved.Name)
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get volume usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVolumeDataCommand(cmd, volumeDataCommandConfig{
			endpoint:       types.Endpoints.VolumesCounts,
			failureMessage: "failed to get volume counts",
			header:         "Volume Usage Counts",
			marshalMessage: "failed to marshal volume counts",
		})
	},
}

var pruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused volumes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, "Are you sure you want to prune unused volumes?")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.VolumesPrune(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to prune volumes")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to prune volumes")
		}

		var result base.ApiResponse[any]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Volumes pruned successfully")
		return nil
	},
}

var sizesCmd = &cobra.Command{
	Use:          "sizes",
	Short:        "Get volume sizes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVolumeDataCommand(cmd, volumeDataCommandConfig{
			endpoint:       types.Endpoints.VolumesSizes,
			failureMessage: "failed to get volume sizes",
			header:         "Volume Sizes",
			marshalMessage: "failed to marshal volume sizes",
		})
	},
}

type volumeDataCommandConfig struct {
	endpoint       func(string) string
	failureMessage string
	header         string
	marshalMessage string
}

func runVolumeDataCommand(cmd *cobra.Command, cfg volumeDataCommandConfig) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	resp, err := c.Get(cmd.Context(), cfg.endpoint(c.EnvID()))
	if err != nil {
		return errors.WrapIff(err, "%s", cfg.failureMessage)
	}
	defer func() { _ = resp.Body.Close() }()

	var result base.ApiResponse[any]
	if err := cmdutil.DecodeJSON(resp, &result); err != nil {
		return err
	}

	resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
	if err != nil {
		if jsonOutput {
			return errors.WrapIf(err, "failed to marshal JSON")
		}
		return errors.WrapIff(err, "%s", cfg.marshalMessage)
	}

	if jsonOutput {
		fmt.Println(string(resultBytes))
		return nil
	}

	output.Header("%s", cfg.header)
	fmt.Println(string(resultBytes))
	return nil
}

var usageCmd = &cobra.Command{
	Use:          "usage <volume-name>",
	Short:        "Get specific volume usage",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, err := resolveVolume(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.VolumeUsage(c.EnvID(), resolved.Name))
		if err != nil {
			return errors.WrapIf(err, "failed to get volume usage")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[any]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Volume Usage: %s", resolved.Name)
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return errors.WrapIf(err, "failed to marshal volume usage")
		}
		fmt.Println(string(resultBytes))
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a volume",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		req := volume.Create{
			Name:   volumeCreateName,
			Driver: volumeCreateDriver,
		}

		if len(volumeCreateOpts) > 0 {
			req.DriverOpts = make(map[string]string)
			for _, opt := range volumeCreateOpts {
				parts := strings.SplitN(opt, "=", 2)
				if len(parts) == 2 {
					req.DriverOpts[parts[0]] = parts[1]
				}
			}
		}

		if len(volumeCreateLabels) > 0 {
			req.Labels = make(map[string]string)
			for _, lbl := range volumeCreateLabels {
				parts := strings.SplitN(lbl, "=", 2)
				if len(parts) == 2 {
					req.Labels[parts[0]] = parts[1]
				}
			}
		}

		path := types.Endpoints.Volumes(c.EnvID())
		resp, err := c.Post(cmd.Context(), path, req)
		if err != nil {
			return errors.WrapIf(err, "failed to create volume")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to create volume")
		}

		var result base.ApiResponse[volume.Volume]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Volume %s created successfully", result.Data.Name)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Driver", result.Data.Driver)
		output.KeyValue("Mountpoint", result.Data.Mountpoint)
		return nil
	},
}

func init() {
	VolumesCmd.AddCommand(listCmd)
	VolumesCmd.AddCommand(getCmd)
	VolumesCmd.AddCommand(deleteCmd)
	VolumesCmd.AddCommand(countsCmd)
	VolumesCmd.AddCommand(pruneCmd)
	VolumesCmd.AddCommand(sizesCmd)
	VolumesCmd.AddCommand(usageCmd)
	VolumesCmd.AddCommand(createCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of volumes to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&inUseOnlyFlag, "inuse", false, "Only show volumes currently in use")
	listCmd.Flags().BoolVar(&unusedOnlyFlag, "unused", false, "Only show volumes not in use")
	listCmd.Flags().BoolVar(&includeInternalFlag, "include-internal", false, "Include Arcane-internal volumes")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force removal of a volume that is in use and skip the confirmation prompt")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Prune command flags
	pruneCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force prune without confirmation")
	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Other command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	sizesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	usageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringVar(&volumeCreateName, "name", "", "Volume name")
	createCmd.Flags().StringVar(&volumeCreateDriver, "driver", "", "Volume driver")
	createCmd.Flags().StringArrayVar(&volumeCreateOpts, "opt", nil, "Driver option (KEY=VALUE)")
	createCmd.Flags().StringArrayVar(&volumeCreateLabels, "label", nil, "Label (KEY=VALUE)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
}

func resolveVolume(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*volume.Volume, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, errors.New("volume identifier is required")
	}

	resp, err := c.Get(ctx, types.Endpoints.Volume(c.EnvID(), trimmed))
	if err != nil {
		return nil, errors.WrapIff(err, "failed to resolve volume %q", trimmed)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read volume response")
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[volume.Volume]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, errors.WrapIf(err, "failed to parse volume response")
		}
		return &result.Data, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, errors.Errorf("failed to resolve volume %q (status %d): %s", trimmed, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Volumes(c.EnvID()), url.QueryEscape(trimmed), cmdutil.ShowAllLimit)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to search volumes")
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read volumes response")
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, errors.Errorf("failed to search volumes (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result base.Paginated[volume.Volume]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, errors.WrapIf(err, "failed to parse volumes response")
	}

	identifierLower := strings.ToLower(trimmed)
	matches := make([]volume.Volume, 0)
	for _, item := range result.Data {
		if volumeMatches(item, identifierLower, trimmed) {
			matches = append(matches, item)
		}
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}

	if len(matches) > 1 {
		if !allowPrompt {
			return nil, errors.Errorf("multiple volumes match %q; use the volume name or run `arcane volumes list`", trimmed)
		}
		if len(matches) > maxPromptOptions {
			return nil, errors.Errorf("multiple volumes match %q (%d results); refine your query or use the volume name", trimmed, len(matches))
		}

		options := make([]string, 0, len(matches))
		for _, match := range matches {
			label := match.Name
			if match.Driver != "" {
				label = fmt.Sprintf("%s (%s)", match.Name, match.Driver)
			}
			options = append(options, label)
		}
		choice, err := prompt.Select("volume", options)
		if err != nil {
			return nil, err
		}
		return &matches[choice], nil
	}

	return nil, errors.Errorf("volume %q not found; use the volume name or run `arcane volumes list`", trimmed)
}

func volumeMatches(item volume.Volume, identifierLower, original string) bool {
	if strings.EqualFold(item.Name, original) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), identifierLower) {
		return true
	}
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	return false
}
