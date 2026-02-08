// Package images provides CLI commands for managing Docker images on Arcane servers.
//
// This package implements the "arcane images" command group, which includes
// subcommands for listing, inspecting, pulling, removing, pruning, and uploading
// Docker images.
//
// # Available Commands
//
//   - list: List all images with optional filtering and pagination
//   - get: Get detailed information about a specific image
//   - pull: Pull an image from a container registry
//   - remove: Remove an image from the server
//   - prune: Remove unused images to reclaim disk space
//   - counts: Display image usage statistics
//   - upload: Upload a Docker image from a tar archive
//
// # Example Usage
//
//	# List all images
//	arcane images list
//
//	# Pull an image
//	arcane images pull nginx:latest
//
//	# Get image details
//	arcane images get sha256:abc123...
//
//	# Remove unused images
//	arcane images prune
package images

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/logger"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/image"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"go.withmatt.com/size"
)

var (
	imagesLimit  int
	imagesStart  int
	imagesSort   string
	imagesOrder  string
	imagesSearch string
)

const maxPromptOptions = 20

// ImagesCmd is the parent command for image operations
var ImagesCmd = &cobra.Command{
	Use:     "images",
	Aliases: []string{"image", "i"},
	Short:   "Manage images",
}

var imagesListCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List images",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Images(c.EnvID())

		// Parse the path to handle query params
		u, err := url.Parse(path)
		if err != nil {
			return fmt.Errorf("failed to parse endpoint path: %w", err)
		}
		q := u.Query()

		if imagesLimit > 0 {
			q.Set("limit", fmt.Sprintf("%d", imagesLimit))
		}
		if imagesStart > 0 {
			q.Set("start", fmt.Sprintf("%d", imagesStart))
		}
		if imagesSort != "" {
			q.Set("sort", imagesSort)
		}
		if imagesOrder != "" {
			q.Set("order", imagesOrder)
		}
		if imagesSearch != "" {
			q.Set("search", imagesSearch)
		}

		u.RawQuery = q.Encode()
		path = u.String()

		log.Debugf("Listing images from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list images: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		log.Debugf("Response body: %s", string(body))

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			fmt.Println(string(body))
			return nil
		}

		var result struct {
			Success    bool            `json:"success"`
			Data       []image.Summary `json:"data"`
			Pagination struct {
				TotalItems int64 `json:"totalItems"`
			} `json:"pagination"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		headers := []string{"ID", "REPOSITORY:TAG", "SIZE", "IN USE"}
		var rows [][]string

		for _, img := range result.Data {
			tag := "<none>"
			if len(img.RepoTags) > 0 {
				tag = img.RepoTags[0]
			}
			inUse := color.New(color.FgHiBlack).Sprint("No")
			if img.InUse {
				inUse = color.New(color.FgGreen).Sprint("Yes")
			}
			id := color.New(color.FgHiWhite, color.Bold).Sprint(img.ID)
			rows = append(rows, []string{id, tag, size.Capacity(img.Size).String(), inUse})
		}

		output.Table(headers, rows)
		output.Info("Total: %d images", result.Pagination.TotalItems)

		return nil
	},
}

var imagesGetCmd = &cobra.Command{
	Use:          "get <image-id|name>",
	Short:        "Get image details by ID or name",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		allowPrompt := !jsonOutput && prompt.IsInteractive()

		imageID, err := resolveImageID(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}
		path := types.Endpoints.Image(c.EnvID(), imageID)

		log.Debugf("Getting image details from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to get image: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		log.Debugf("Response body: %s", string(body))

		if jsonOutput {
			fmt.Println(string(body))
			return nil
		}

		var result struct {
			Success bool                `json:"success"`
			Data    image.DetailSummary `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		output.Header("Image Details")
		output.KeyValue("ID", result.Data.ID)
		if len(result.Data.RepoTags) > 0 {
			output.KeyValue("Tags", strings.Join(result.Data.RepoTags, ", "))
		}
		output.KeyValue("Size", size.Capacity(result.Data.Size).String())
		output.KeyValue("Architecture", result.Data.Architecture)
		output.KeyValue("OS", result.Data.Os)
		if result.Data.Created != "" {
			output.KeyValue("Created", result.Data.Created)
		}
		if result.Data.Author != "" {
			output.KeyValue("Author", result.Data.Author)
		}

		if result.Data.Config.WorkingDir != "" {
			output.KeyValue("Working Dir", result.Data.Config.WorkingDir)
		}

		if len(result.Data.Config.Cmd) > 0 {
			output.KeyValue("Cmd", strings.Join(result.Data.Config.Cmd, " "))
		}

		if len(result.Data.Config.Env) > 0 {
			output.Header("Environment Variables")
			for _, env := range result.Data.Config.Env {
				fmt.Println(env)
			}
		}

		if len(result.Data.Config.ExposedPorts) > 0 {
			output.Header("Exposed Ports")
			var ports []string
			for p := range result.Data.Config.ExposedPorts {
				ports = append(ports, p)
			}
			sort.Strings(ports)
			for _, p := range ports {
				fmt.Println(p)
			}
		}

		return nil
	},
}

var (
	removeForce bool
)

var imagesRemoveCmd = &cobra.Command{
	Use:          "remove <image-id|name>",
	Aliases:      []string{"rm", "delete"},
	Short:        "Remove an image",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		imageID, err := resolveImageID(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}
		path := types.Endpoints.Image(c.EnvID(), imageID)

		if removeForce {
			u, err := url.Parse(path)
			if err != nil {
				return fmt.Errorf("failed to parse path: %w", err)
			}
			q := u.Query()
			q.Set("force", "true")
			u.RawQuery = q.Encode()
			path = u.String()
		}

		log.Debugf("Removing image from: %s", path)

		resp, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to remove image: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		log.Debugf("Response body: %s", string(body))

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			fmt.Println(string(body))
			return nil
		}

		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Message string `json:"message"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		output.Success("%s", result.Data.Message)

		return nil
	},
}

var imagesPullCmd = &cobra.Command{
	Use:          "pull [IMAGE_NAME]",
	Short:        "Pull an image from a registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Pulling large images can take a long time
		c.SetTimeout(30 * time.Minute)

		imageName := args[0]
		path := types.Endpoints.ImagesPull(c.EnvID())

		log.Debugf("Pulling image from: %s", path)

		requestBody := map[string]interface{}{
			"imageName": imageName,
		}

		resp, err := c.Post(cmd.Context(), path, requestBody)
		if err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Stream the response
		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			_, err = io.Copy(cmd.OutOrStdout(), resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read pull stream: %w", err)
			}
			return nil
		}

		output.Info("Pulling image: %s", imageName)

		decoder := json.NewDecoder(resp.Body)
		var bar *progressbar.ProgressBar
		var currentID string

		for {
			var event struct {
				Status         string `json:"status"`
				Error          string `json:"error"`
				ID             string `json:"id"`
				ProgressDetail struct {
					Current int64 `json:"current"`
					Total   int64 `json:"total"`
				} `json:"progressDetail"`
			}

			if err := decoder.Decode(&event); err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("failed to decode stream: %w", err)
			}

			if event.Error != "" {
				return fmt.Errorf("pull error: %s", event.Error)
			}

			if event.Status == "Downloading" && event.ProgressDetail.Total > 0 {
				if bar == nil || currentID != event.ID {
					if bar != nil {
						_ = bar.Finish()
						fmt.Println()
					}
					currentID = event.ID
					bar = progressbar.NewOptions64(
						event.ProgressDetail.Total,
						progressbar.OptionSetDescription(fmt.Sprintf("Downloading %s", event.ID)),
						progressbar.OptionSetWriter(os.Stdout),
						progressbar.OptionShowBytes(true),
						progressbar.OptionSetWidth(15),
						progressbar.OptionThrottle(65*time.Millisecond),
						progressbar.OptionShowCount(),
						progressbar.OptionOnCompletion(func() {
							fmt.Println()
						}),
						progressbar.OptionSpinnerType(14),
						progressbar.OptionFullWidth(),
						progressbar.OptionSetTheme(progressbar.Theme{
							Saucer:        "=",
							SaucerHead:    ">",
							SaucerPadding: " ",
							BarStart:      "[",
							BarEnd:        "]",
						}),
					)
				}
				_ = bar.Set64(event.ProgressDetail.Current)
			} else {
				if bar != nil {
					// If we switch from downloading to something else for the same ID, finish the bar
					if event.ID == currentID && event.Status == "Download complete" {
						_ = bar.Finish()
						fmt.Println()
						bar = nil
						currentID = ""
					}
				}

				// Only print status if it's not a progress update for the current bar
				if event.Status != "Downloading" {
					if event.ID != "" {
						fmt.Printf("%s: %s\n", event.ID, event.Status)
					} else {
						fmt.Printf("%s\n", event.Status)
					}
				}
			}
		}

		output.Success("Image pulled successfully")

		return nil
	},
}

var (
	pruneDangling bool
)

var imagesPruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused images",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ImagesPrune(c.EnvID())

		log.Debugf("Pruning images from: %s", path)

		requestBody := map[string]interface{}{
			"dangling": pruneDangling,
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		var stopSpinner func()

		if !jsonOutput {
			bar := progressbar.NewOptions(-1,
				progressbar.OptionSetDescription("Pruning images..."),
				progressbar.OptionSpinnerType(14),
				progressbar.OptionSetWriter(os.Stdout),
				progressbar.OptionClearOnFinish(),
				progressbar.OptionSetWidth(10),
				progressbar.OptionSetTheme(progressbar.Theme{
					Saucer:        "=",
					SaucerHead:    ">",
					SaucerPadding: " ",
					BarStart:      "[",
					BarEnd:        "]",
				}),
			)

			done := make(chan bool)
			go func() {
				for {
					select {
					case <-done:
						return
					case <-time.After(100 * time.Millisecond):
						_ = bar.Add(1)
					}
				}
			}()

			stopSpinner = func() {
				done <- true
				_ = bar.Finish()
				fmt.Println()
			}
		}

		resp, err := c.Post(cmd.Context(), path, requestBody)

		if stopSpinner != nil {
			stopSpinner()
		}

		if err != nil {
			return fmt.Errorf("failed to prune images: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		log.Debugf("Response body: %s", string(body))

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			fmt.Println(string(body))
			return nil
		}

		var result struct {
			Success bool `json:"success"`
			Data    struct {
				ImagesDeleted  []string `json:"imagesDeleted"`
				SpaceReclaimed int64    `json:"spaceReclaimed"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		output.Success("Pruned %d images, reclaimed %s", len(result.Data.ImagesDeleted), size.Capacity(result.Data.SpaceReclaimed).String())

		return nil
	},
}

var imagesCountsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get image usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ImagesCounts(c.EnvID())

		log.Debugf("Getting image counts from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to get image counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		log.Debugf("Response body: %s", string(body))

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			fmt.Println(string(body))
			return nil
		}

		var result struct {
			Success bool              `json:"success"`
			Data    image.UsageCounts `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		output.Header("Image Usage Counts")
		output.KeyValue("In Use", result.Data.Inuse)
		output.KeyValue("Unused", result.Data.Unused)
		output.KeyValue("Total", result.Data.Total)
		output.KeyValue("Total Size", size.Capacity(result.Data.TotalSize).String())

		return nil
	},
}

var imagesUploadCmd = &cobra.Command{
	Use:          "upload [FILE]",
	Short:        "Upload a Docker image from a tar archive",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Uploading large images can take a long time
		c.SetTimeout(30 * time.Minute)

		filePath := args[0]
		path := types.Endpoints.ImagesUpload(c.EnvID())

		log.Debugf("Uploading image from file: %s to %s", filePath, path)

		// Open the file
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer func() { _ = file.Close() }()

		// Create a multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("file", filepath.Base(filePath))
		if err != nil {
			return fmt.Errorf("failed to create form file: %w", err)
		}

		_, err = io.Copy(part, file)
		if err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}

		err = writer.Close()
		if err != nil {
			return fmt.Errorf("failed to close writer: %w", err)
		}

		output.Info("Uploading image: %s", filePath)

		var requestBody io.Reader = body

		jsonOutput, _ := cmd.Flags().GetBool("json")
		if !jsonOutput {
			bar := progressbar.NewOptions64(
				int64(body.Len()),
				progressbar.OptionSetDescription("Uploading"),
				progressbar.OptionSetWriter(os.Stdout),
				progressbar.OptionShowBytes(true),
				progressbar.OptionSetWidth(15),
				progressbar.OptionThrottle(65*time.Millisecond),
				progressbar.OptionShowCount(),
				progressbar.OptionOnCompletion(func() {
					fmt.Println()
				}),
				progressbar.OptionSpinnerType(14),
				progressbar.OptionFullWidth(),
				progressbar.OptionSetTheme(progressbar.Theme{
					Saucer:        "=",
					SaucerHead:    ">",
					SaucerPadding: " ",
					BarStart:      "[",
					BarEnd:        "]",
				}),
			)
			requestBody = io.TeeReader(body, bar)
		}

		// Use client.RequestRaw to make the multipart request with correct headers
		headers := map[string]string{
			"Content-Type": writer.FormDataContentType(),
		}

		resp, err := c.RequestRaw(cmd.Context(), http.MethodPost, path, requestBody, headers)
		if err != nil {
			return fmt.Errorf("failed to upload image: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		log.Debugf("Response body: %s", string(respBody))

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			fmt.Println(string(respBody))
			return nil
		}

		var result struct {
			Success bool             `json:"success"`
			Data    image.LoadResult `json:"data"`
		}

		if err := json.Unmarshal(respBody, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !result.Success {
			return fmt.Errorf("upload failed: %s", string(respBody))
		}

		output.Success("Image uploaded successfully")

		return nil
	},
}

func init() {
	ImagesCmd.AddCommand(imagesListCmd)
	imagesListCmd.Flags().IntVarP(&imagesLimit, "limit", "n", 0, "Number of images to show (server default 20)")
	imagesListCmd.Flags().IntVar(&imagesStart, "start", 0, "Offset for pagination")
	imagesListCmd.Flags().StringVar(&imagesSort, "sort", "", "Field to sort by")
	imagesListCmd.Flags().StringVar(&imagesOrder, "order", "", "Sort order (asc/desc)")
	imagesListCmd.Flags().StringVar(&imagesSearch, "search", "", "Search query")

	ImagesCmd.AddCommand(imagesGetCmd)

	ImagesCmd.AddCommand(imagesRemoveCmd)
	imagesRemoveCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "Force removal of image")

	ImagesCmd.AddCommand(imagesPullCmd)

	ImagesCmd.AddCommand(imagesPruneCmd)
	imagesPruneCmd.Flags().BoolVar(&pruneDangling, "dangling", false, "Only remove dangling images")

	ImagesCmd.AddCommand(imagesCountsCmd)

	ImagesCmd.AddCommand(imagesUploadCmd)
}

func resolveImageID(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (string, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return "", fmt.Errorf("image identifier is required")
	}

	resolvedID, found, err := resolveImageByID(ctx, c, trimmed)
	if err != nil {
		return "", err
	}
	if found {
		return resolvedID, nil
	}

	terms := buildImageSearchTerms(trimmed)
	seenTerms := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if _, ok := seenTerms[term]; ok {
			continue
		}
		seenTerms[term] = struct{}{}

		matches, err := searchImageMatches(ctx, c, term, trimmed)
		if err != nil {
			return "", err
		}

		selectedID, resolved, err := selectImageMatchID(matches, trimmed, allowPrompt)
		if err != nil {
			return "", err
		}
		if resolved {
			return selectedID, nil
		}
	}

	return "", fmt.Errorf("image %q not found; use the image ID or run `arcane images list`", trimmed)
}

func resolveImageByID(ctx context.Context, c *client.Client, identifier string) (string, bool, error) {
	resp, err := c.Get(ctx, types.Endpoints.Image(c.EnvID(), identifier))
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve image %q: %w", identifier, err)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", false, fmt.Errorf("failed to read image response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var result struct {
			Success bool                `json:"success"`
			Data    image.DetailSummary `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", false, fmt.Errorf("failed to parse image response: %w", err)
		}
		if result.Data.ID == "" {
			return "", false, fmt.Errorf("image lookup for %q returned empty ID", identifier)
		}
		return result.Data.ID, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return "", false, fmt.Errorf("failed to resolve image %q (status %d): %s", identifier, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return "", false, nil
}

func buildImageSearchTerms(trimmed string) []string {
	terms := []string{trimmed}
	identifierLower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "@") {
		if repo, _, ok := strings.Cut(trimmed, "@"); ok && repo != "" {
			terms = append(terms, repo)
		}
		return terms
	}
	if strings.Contains(trimmed, ":") && !strings.HasPrefix(identifierLower, "sha256:") {
		if repo, _, ok := strings.Cut(trimmed, ":"); ok && repo != "" {
			terms = append(terms, repo)
		}
	}
	return terms
}

func searchImageMatches(ctx context.Context, c *client.Client, term, trimmed string) ([]image.Summary, error) {
	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Images(c.EnvID()), url.QueryEscape(term), 200)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, fmt.Errorf("failed to search images: %w", err)
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read images response: %w", err)
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to search images (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result struct {
		Success bool            `json:"success"`
		Data    []image.Summary `json:"data"`
	}
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse images response: %w", err)
	}

	return filterImageMatches(result.Data, trimmed), nil
}

func filterImageMatches(items []image.Summary, trimmed string) []image.Summary {
	identifierLower := strings.ToLower(trimmed)
	hasSeparator := strings.Contains(trimmed, ":") || strings.Contains(trimmed, "@")
	matches := make([]image.Summary, 0)
	for _, item := range items {
		if imageMatches(item, trimmed, identifierLower, hasSeparator) {
			matches = append(matches, item)
		}
	}
	return matches
}

func imageMatches(item image.Summary, trimmed, identifierLower string, hasSeparator bool) bool {
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}

	if !hasSeparator && strings.Contains(strings.ToLower(item.Repo), identifierLower) {
		return true
	}

	for _, tag := range item.RepoTags {
		tagLower := strings.ToLower(tag)
		if (!hasSeparator && strings.Contains(tagLower, identifierLower)) || strings.EqualFold(tag, trimmed) {
			return true
		}
	}

	if item.Repo != "" && item.Tag != "" {
		combined := item.Repo + ":" + item.Tag
		combinedLower := strings.ToLower(combined)
		if strings.EqualFold(combined, trimmed) || (hasSeparator && strings.Contains(combinedLower, identifierLower)) {
			return true
		}
	}

	for _, digest := range item.RepoDigests {
		digestLower := strings.ToLower(digest)
		if strings.EqualFold(digest, trimmed) || strings.Contains(digestLower, identifierLower) {
			return true
		}
	}

	return false
}

func selectImageMatchID(matches []image.Summary, trimmed string, allowPrompt bool) (string, bool, error) {
	if len(matches) == 1 {
		return matches[0].ID, true, nil
	}
	if len(matches) == 0 {
		return "", false, nil
	}

	if !allowPrompt {
		return "", false, fmt.Errorf("multiple images match %q; use the image ID or run `arcane images list`", trimmed)
	}
	if len(matches) > maxPromptOptions {
		return "", false, fmt.Errorf("multiple images match %q (%d results); refine your query or use the image ID", trimmed, len(matches))
	}

	options := make([]string, 0, len(matches))
	for _, match := range matches {
		options = append(options, formatImageMatchOption(match))
	}
	choice, err := prompt.Select("image", options)
	if err != nil {
		return "", false, err
	}
	return matches[choice].ID, true, nil
}

func formatImageMatchOption(match image.Summary) string {
	label := match.ID
	if len(match.RepoTags) > 0 {
		label = match.RepoTags[0]
	} else if match.Repo != "" && match.Tag != "" {
		label = match.Repo + ":" + match.Tag
	}
	return fmt.Sprintf("%s (%s)", label, match.ID)
}
