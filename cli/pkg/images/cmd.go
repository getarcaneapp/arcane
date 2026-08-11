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
//   - updates: Check for image updates
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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/images/updates"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	"github.com/spf13/cobra"
)

var (
	imagesLimit      int
	imagesStart      int
	imagesSort       string
	imagesOrder      string
	imagesSearch     string
	imagesInUseOnly  bool
	imagesUnusedOnly bool
	imagesAll        bool

	imagesUpdatesFilter string
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
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		if imagesInUseOnly && imagesUnusedOnly {
			return errors.New("--inuse and --unused cannot be used together")
		}

		path := types.Endpoints.Images(c.EnvID())

		// Parse the path to handle query params
		u, err := url.Parse(path)
		if err != nil {
			return errors.WrapIf(err, "failed to parse endpoint path")
		}
		q := u.Query()

		if imagesSort != "" {
			q.Set("sort", imagesSort)
		}
		if imagesOrder != "" {
			q.Set("order", imagesOrder)
		}
		if imagesSearch != "" {
			q.Set("search", imagesSearch)
		}
		// Filter server-side so that pagination and the reported totals apply to
		// the matching set rather than to whichever page happened to be fetched.
		if imagesInUseOnly {
			q.Set("inUse", "true")
		}
		if imagesUnusedOnly {
			q.Set("inUse", "false")
		}
		if imagesUpdatesFilter != "" {
			q.Set("updates", imagesUpdatesFilter)
		}

		u.RawQuery = q.Encode()
		path, err = cmdutil.ApplyPaginationParams(cmd, u.String(), cmdutil.ListParams{
			Resource:        "images",
			Limit:           imagesLimit,
			FallbackDefault: 0,
			Start:           imagesStart,
			All:             imagesAll,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		log.Debugf("Listing images from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list images")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list images")
		}

		log.Debugf("Response body: %s", string(body))

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[image.Summary]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"ID", "REPOSITORY:TAG", "SIZE", "IN USE"}
		var rows [][]string

		for _, img := range result.Data {
			tag := "<none>"
			if len(img.RepoTags) > 0 {
				tag = img.RepoTags[0]
			}
			inUse := "No"
			if img.InUse {
				inUse = "Yes"
			}
			rows = append(rows, []string{img.ID, tag, output.Bytes(img.Size), inUse})
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "images")

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
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		jsonOutput := cmdutil.JSONOutputEnabled(cmd)
		allowPrompt := !jsonOutput && prompt.IsInteractive()

		imageID, err := resolveImageID(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}
		path := types.Endpoints.Image(c.EnvID(), imageID)

		log.Debugf("Getting image details from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get image")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errors.WrapIf(err, "failed to read response")
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
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Header("Image Details")
		output.KeyValue("ID", result.Data.ID)
		if len(result.Data.RepoTags) > 0 {
			output.KeyValue("Tags", strings.Join(result.Data.RepoTags, ", "))
		}
		output.KeyValue("Size", output.Bytes(result.Data.Size))
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

var removeForce bool

var imagesRemoveCmd = &cobra.Command{
	Use:          "remove <image-id|name>",
	Aliases:      []string{"rm", "delete"},
	Short:        "Remove an image",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
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
				return errors.WrapIf(err, "failed to parse path")
			}
			q := u.Query()
			q.Set("force", "true")
			u.RawQuery = q.Encode()
			path = u.String()
		}

		log.Debugf("Removing image from: %s", path)

		resp, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to remove image")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return errors.Errorf("failed to remove image (status %d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errors.WrapIf(err, "failed to read response")
		}

		log.Debugf("Response body: %s", string(body))

		if cmdutil.JSONOutputEnabled(cmd) {
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
			return errors.WrapIf(err, "failed to parse response")
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
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		// Pulling large images can take a long time
		c.SetTimeout(30 * time.Minute)

		imageName := args[0]
		path := types.Endpoints.ImagesPull(c.EnvID())

		log.Debugf("Pulling image from: %s", path)

		requestBody := map[string]any{
			"imageName": imageName,
		}

		resp, err := c.Post(cmd.Context(), path, requestBody)
		if err != nil {
			return errors.WrapIf(err, "failed to pull image")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return errors.Errorf("failed to pull image (status %d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}

		// Stream the response
		if cmdutil.JSONOutputEnabled(cmd) {
			_, err = io.Copy(cmd.OutOrStdout(), resp.Body)
			if err != nil {
				return errors.WrapIf(err, "failed to read pull stream")
			}
			return nil
		}

		output.Info("Pulling image: %s", imageName)

		decoder := json.NewDecoder(resp.Body)
		var progressUI *output.Progress
		var currentID string

		for {
			var event struct {
				Type           string `json:"type"`
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
				return errors.WrapIf(err, "failed to decode stream")
			}

			// The stream opens with an activity frame that carries no Docker
			// status; printing it would emit a blank line per pull.
			if event.Type != "" && event.Status == "" && event.Error == "" {
				continue
			}

			if event.Error != "" {
				if progressUI != nil {
					progressUI.Stop()
				}
				return errors.Errorf("pull error: %s", event.Error)
			}

			if event.Status == "Downloading" && event.ProgressDetail.Total > 0 {
				if progressUI == nil {
					progressUI = output.StartProgress("", event.ProgressDetail.Total)
				}
				if currentID != event.ID {
					currentID = event.ID
					progressUI.SetLabel("Downloading " + event.ID)
					progressUI.SetTotal(event.ProgressDetail.Total)
				}
				progressUI.SetCurrent(event.ProgressDetail.Current)
			} else {
				if progressUI != nil {
					// Stop the progress bar when the current layer completes.
					if event.ID == currentID && event.Status == "Download complete" {
						progressUI.SetCurrent(event.ProgressDetail.Total)
						progressUI.SetLabel("Download complete")
						progressUI.Stop()
						progressUI = nil
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

		if progressUI != nil {
			progressUI.Stop()
		}

		output.Success("Image pulled successfully")

		return nil
	},
}

var pruneDangling bool

var imagesPruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused images",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.Endpoints.ImagesPrune(c.EnvID())

		log.Debugf("Pruning images from: %s", path)

		requestBody := map[string]any{
			"dangling": pruneDangling,
		}

		jsonOutput := cmdutil.JSONOutputEnabled(cmd)
		var spinner *output.Spinner

		if !jsonOutput {
			spinner = output.StartSpinner("Pruning images...")
		}

		resp, err := c.Post(cmd.Context(), path, requestBody)

		if spinner != nil {
			spinner.Stop()
		}

		if err != nil {
			return errors.WrapIf(err, "failed to prune images")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return errors.Errorf("failed to prune images (status %d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errors.WrapIf(err, "failed to read response")
		}

		log.Debugf("Response body: %s", string(body))

		if cmdutil.JSONOutputEnabled(cmd) {
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
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Success("Pruned %d images, reclaimed %s", len(result.Data.ImagesDeleted), output.Bytes(result.Data.SpaceReclaimed))

		return nil
	},
}

var imagesCountsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get image usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.Endpoints.ImagesCounts(c.EnvID())

		log.Debugf("Getting image counts from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get image counts")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errors.WrapIf(err, "failed to read response")
		}

		log.Debugf("Response body: %s", string(body))

		if cmdutil.JSONOutputEnabled(cmd) {
			fmt.Println(string(body))
			return nil
		}

		var result struct {
			Success bool              `json:"success"`
			Data    image.UsageCounts `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Header("Image Usage Counts")
		output.KeyValue("In Use", result.Data.Inuse)
		output.KeyValue("Unused", result.Data.Unused)
		output.KeyValue("Total", result.Data.Total)
		output.KeyValue("Total Size", output.Bytes(result.Data.TotalSize))

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
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		// Uploading large images can take a long time
		c.SetTimeout(30 * time.Minute)

		filePath := args[0]

		// Open the file
		file, err := os.Open(filePath)
		if err != nil {
			return errors.WrapIf(err, "failed to open file")
		}
		defer func() { _ = file.Close() }()
		fileInfo, err := file.Stat()
		if err != nil {
			return errors.WrapIf(err, "failed to stat file")
		}

		// Create the chunked upload session; the file is sent as independently
		// retried chunks so reverse-proxy body limits never see the full size.
		var created struct {
			Success bool                `json:"success"`
			Data    uploadtypes.Session `json:"data"`
		}
		createPath := types.Endpoints.UploadSessions(c.EnvID(), uploadtypes.KindImage)
		if err := c.DoJSON(cmd.Context(), http.MethodPost, createPath, uploadtypes.CreateSessionRequest{
			Filename: filepath.Base(filePath),
			Size:     fileInfo.Size(),
		}, &created); err != nil {
			return errors.WrapIf(err, "failed to create upload session")
		}
		session := created.Data

		log.Debugf("Uploading image %s as %d chunks of %d bytes (session %s)", filePath, session.TotalChunks, session.ChunkSize, session.ID)
		output.Info("Uploading image: %s", filePath)

		jsonOutput := cmdutil.JSONOutputEnabled(cmd)
		var progressUI *output.Progress
		if !jsonOutput {
			progressUI = output.StartProgress("Uploading", fileInfo.Size())
			defer progressUI.Stop()
		}

		deleteSession := func() {
			if resp, deleteErr := c.Delete(context.WithoutCancel(cmd.Context()), types.Endpoints.UploadSession(c.EnvID(), uploadtypes.KindImage, session.ID)); deleteErr == nil {
				_ = resp.Body.Close()
			}
		}

		buf := make([]byte, session.ChunkSize)
		for index := range session.TotalChunks {
			expected := session.ChunkSize
			if index == session.TotalChunks-1 {
				expected = session.Size - int64(index)*session.ChunkSize
			}
			if _, err := io.ReadFull(file, buf[:expected]); err != nil {
				deleteSession()
				return errors.WrapIf(err, "failed to read file")
			}

			chunkPath := types.Endpoints.UploadSessionChunk(c.EnvID(), uploadtypes.KindImage, session.ID, index)
			for attempt := 1; ; attempt++ {
				resp, chunkErr := c.RequestRaw(cmd.Context(), http.MethodPut, chunkPath, bytes.NewReader(buf[:expected]), map[string]string{"Content-Type": "application/octet-stream"})
				if chunkErr == nil {
					ok := resp.StatusCode >= 200 && resp.StatusCode < 300
					if !ok {
						errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
						chunkErr = errors.Errorf("chunk %d failed (status %d): %s", index, resp.StatusCode, strings.TrimSpace(string(errorBody)))
					}
					_ = resp.Body.Close()
					if ok {
						break
					}
				}
				if attempt >= 3 {
					deleteSession()
					return errors.WrapIf(chunkErr, "failed to upload image")
				}
				log.Debugf("Retrying chunk %d after error: %v", index, chunkErr)
			}
			if progressUI != nil {
				progressUI.Add(expected)
			}
		}

		respBody, err := c.DoRaw(cmd.Context(), http.MethodPost, types.Endpoints.ImagesUpload(c.EnvID()), uploadtypes.ConsumeRequest{UploadID: session.ID})
		if err != nil {
			deleteSession()
			return errors.WrapIf(err, "failed to upload image")
		}

		log.Debugf("Response body: %s", string(respBody))

		if jsonOutput {
			fmt.Println(string(respBody))
			return nil
		}

		var result struct {
			Success bool             `json:"success"`
			Data    image.LoadResult `json:"data"`
		}

		if err := json.Unmarshal(respBody, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		if !result.Success {
			return errors.Errorf("upload failed: %s", string(respBody))
		}

		output.Success("Image uploaded successfully")

		return nil
	},
}

func init() {
	ImagesCmd.AddCommand(imagesListCmd)
	imagesListCmd.Flags().IntVarP(&imagesLimit, "limit", "n", 0, "Number of images to show (server default 20)")
	imagesListCmd.Flags().IntVar(&imagesStart, "start", 0, cmdutil.StartFlagUsage)
	imagesListCmd.Flags().BoolVarP(&imagesAll, "all", "a", false, cmdutil.AllFlagUsage)
	imagesListCmd.Flags().StringVar(&imagesUpdatesFilter, "updates", "", "Filter by update status (has_update, up_to_date, error, unknown)")
	imagesListCmd.Flags().StringVar(&imagesSort, "sort", "", "Field to sort by")
	imagesListCmd.Flags().StringVar(&imagesOrder, "order", "", "Sort order (asc/desc)")
	imagesListCmd.Flags().StringVar(&imagesSearch, "search", "", "Search query")
	imagesListCmd.Flags().BoolVar(&imagesInUseOnly, "inuse", false, "Only show images currently in use")
	imagesListCmd.Flags().BoolVar(&imagesUnusedOnly, "unused", false, "Only show images not in use")

	ImagesCmd.AddCommand(imagesGetCmd)

	ImagesCmd.AddCommand(imagesRemoveCmd)
	imagesRemoveCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "Force removal of image")

	ImagesCmd.AddCommand(imagesPullCmd)

	ImagesCmd.AddCommand(imagesPruneCmd)
	imagesPruneCmd.Flags().BoolVar(&pruneDangling, "dangling", false, "Only remove dangling images")

	ImagesCmd.AddCommand(imagesCountsCmd)
	ImagesCmd.AddCommand(updates.UpdatesCmd)

	ImagesCmd.AddCommand(imagesUploadCmd)
}

func resolveImageID(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (string, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return "", errors.New("image identifier is required")
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

	return "", errors.Errorf("image %q not found; use the image ID or run `arcane images list`", trimmed)
}

func resolveImageByID(ctx context.Context, c *client.Client, identifier string) (string, bool, error) {
	resp, err := c.Get(ctx, types.Endpoints.Image(c.EnvID(), identifier))
	if err != nil {
		return "", false, errors.WrapIff(err, "failed to resolve image %q", identifier)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", false, errors.WrapIf(err, "failed to read image response")
	}

	if resp.StatusCode == http.StatusOK {
		var result struct {
			Success bool                `json:"success"`
			Data    image.DetailSummary `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", false, errors.WrapIf(err, "failed to parse image response")
		}
		if result.Data.ID == "" {
			return "", false, errors.Errorf("image lookup for %q returned empty ID", identifier)
		}
		return result.Data.ID, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return "", false, errors.Errorf("failed to resolve image %q (status %d): %s", identifier, resp.StatusCode, strings.TrimSpace(string(body)))
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
	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Images(c.EnvID()), url.QueryEscape(term), cmdutil.ShowAllLimit)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to search images")
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read images response")
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return nil, errors.Errorf("failed to search images (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result struct {
		Success bool            `json:"success"`
		Data    []image.Summary `json:"data"`
	}
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return nil, errors.WrapIf(err, "failed to parse images response")
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
		return "", false, errors.Errorf("multiple images match %q; use the image ID or run `arcane images list`", trimmed)
	}
	if len(matches) > maxPromptOptions {
		return "", false, errors.Errorf("multiple images match %q (%d results); refine your query or use the image ID", trimmed, len(matches))
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
