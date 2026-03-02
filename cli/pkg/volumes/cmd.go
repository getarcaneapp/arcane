package volumes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/volume"
	"github.com/spf13/cobra"
)

var (
	limitFlag      int
	forceFlag      bool
	jsonOutput     bool
	inUseOnlyFlag  bool
	unusedOnlyFlag bool
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
			return fmt.Errorf("--inuse and --unused cannot be used together")
		}
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Volumes(c.EnvID())
		effectiveLimit := cmdutil.EffectiveLimit(cmd, "volumes", "limit", limitFlag, 20)
		if effectiveLimit > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, effectiveLimit)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list volumes: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[volume.Volume]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		result.Data = filterVolumesByUsage(result.Data, inUseOnlyFlag, unusedOnlyFlag)
		result.Pagination.TotalItems = int64(len(result.Data))

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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
		fmt.Printf("\nTotal: %d volumes\n", result.Pagination.TotalItems)
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
				return fmt.Errorf("failed to marshal JSON: %w", err)
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
			output.KeyValue("Size (bytes)", resolved.Size)
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Volume(c.EnvID(), resolved.Name))
		if err != nil {
			return fmt.Errorf("failed to delete volume: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete volume: %w", err)
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.VolumesCounts(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get volume counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[any]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Volume Usage Counts")
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal volume counts: %w", err)
		}
		fmt.Println(string(resultBytes))
		return nil
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
			return fmt.Errorf("failed to prune volumes: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to prune volumes: %w", err)
		}

		var result base.ApiResponse[any]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.VolumesSizes(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get volume sizes: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[any]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Volume Sizes")
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal volume sizes: %w", err)
		}
		fmt.Println(string(resultBytes))
		return nil
	},
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
			return fmt.Errorf("failed to get volume usage: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[any]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Volume Usage: %s", resolved.Name)
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal volume usage: %w", err)
		}
		fmt.Println(string(resultBytes))
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

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of volumes to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&inUseOnlyFlag, "inuse", false, "Only show volumes currently in use")
	listCmd.Flags().BoolVar(&unusedOnlyFlag, "unused", false, "Only show volumes not in use")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Prune command flags
	pruneCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force prune without confirmation")
	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Other command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	sizesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	usageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func resolveVolume(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*volume.Volume, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, fmt.Errorf("volume identifier is required")
	}

	resp, err := c.Get(ctx, types.Endpoints.Volume(c.EnvID(), trimmed))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve volume %q: %w", trimmed, err)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read volume response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[volume.Volume]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse volume response: %w", err)
		}
		return &result.Data, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, fmt.Errorf("failed to resolve volume %q (status %d): %s", trimmed, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Volumes(c.EnvID()), url.QueryEscape(trimmed), 200)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, fmt.Errorf("failed to search volumes: %w", err)
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read volumes response: %w", err)
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to search volumes (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result base.Paginated[volume.Volume]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse volumes response: %w", err)
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
			return nil, fmt.Errorf("multiple volumes match %q; use the volume name or run `arcane volumes list`", trimmed)
		}
		if len(matches) > maxPromptOptions {
			return nil, fmt.Errorf("multiple volumes match %q (%d results); refine your query or use the volume name", trimmed, len(matches))
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

	return nil, fmt.Errorf("volume %q not found; use the volume name or run `arcane volumes list`", trimmed)
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

func filterVolumesByUsage(items []volume.Volume, inUseOnly, unusedOnly bool) []volume.Volume {
	if !inUseOnly && !unusedOnly {
		return items
	}
	filtered := make([]volume.Volume, 0, len(items))
	for _, item := range items {
		if inUseOnly && !item.InUse {
			continue
		}
		if unusedOnly && item.InUse {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
