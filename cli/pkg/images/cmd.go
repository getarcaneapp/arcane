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
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		if imagesInUseOnly && imagesUnusedOnly {
			return errors.New("--inuse and --unused cannot be used together")
		}

		query := url.Values{}
		if imagesSort != "" {
			query.Set("sort", imagesSort)
		}
		if imagesOrder != "" {
			query.Set("order", imagesOrder)
		}
		if imagesSearch != "" {
			query.Set("search", imagesSearch)
		}
		// Filter server-side so that pagination and the reported totals apply to
		// the matching set rather than to whichever page happened to be fetched.
		if imagesInUseOnly {
			query.Set("inUse", "true")
		}
		if imagesUnusedOnly {
			query.Set("inUse", "false")
		}
		if imagesUpdatesFilter != "" {
			query.Set("updates", imagesUpdatesFilter)
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[image.Summary]{
			Resource: "images",
			Endpoint: types.Images(c.EnvID()),
			Params: cmdutil.ListParams{
				Resource:        "images",
				Limit:           imagesLimit,
				FallbackDefault: 0,
				Start:           imagesStart,
				All:             imagesAll,
			},
			Query:   query,
			JSON:    cmdutil.JSONOutputEnabled(cmd),
			Headers: []string{"ID", "REPOSITORY:TAG", "SIZE", "IN USE"},
			Row: func(img image.Summary) []string {
				tag := "<none>"
				if len(img.RepoTags) > 0 {
					tag = img.RepoTags[0]
				}
				inUse := "No"
				if img.InUse {
					inUse = "Yes"
				}
				return []string{img.ID, tag, output.Bytes(img.Size), inUse}
			},
		})
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

		resolved, _, err := ImageRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}
		path := types.Image(c.EnvID(), resolved.ID)

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

		resolved, _, err := ImageRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}
		path := types.Image(c.EnvID(), resolved.ID)

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
		path := types.ImagesPull(c.EnvID())

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

		decoder := jsontext.NewDecoder(resp.Body)
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

			if err := json.UnmarshalDecode(decoder, &event); err != nil {
				if errors.Is(err, io.EOF) {
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

		path := types.ImagesPrune(c.EnvID())

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

		path := types.ImagesCounts(c.EnvID())

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
		output.Info("Uploading image: %s", filePath)

		jsonOutput := cmdutil.JSONOutputEnabled(cmd)
		sessionID, err := cmdutil.UploadFileInChunks(cmd.Context(), c, uploadtypes.KindImage, filePath, !jsonOutput)
		if err != nil {
			return err
		}

		respBody, err := c.DoRaw(cmd.Context(), http.MethodPost, types.ImagesUpload(c.EnvID()), uploadtypes.ConsumeRequest{UploadID: sessionID})
		if err != nil {
			cmdutil.AbortUploadSession(cmd.Context(), c, uploadtypes.KindImage, sessionID)
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

var ImageRef = cmdutil.ResourceRef[image.DetailSummary, image.Summary]{
	Singular:         "image",
	Plural:           "images",
	IDHint:           "the image ID",
	ListCmd:          "arcane images list",
	GetPath:          types.Image,
	ListPath:         types.Images,
	SearchCandidates: searchImageCandidatesInternal,
	Matches:          imageMatches,
	Label: func(match image.Summary) string {
		label := match.ID
		if len(match.RepoTags) > 0 {
			label = match.RepoTags[0]
		} else if match.Repo != "" && match.Tag != "" {
			label = match.Repo + ":" + match.Tag
		}
		return fmt.Sprintf("%s (%s)", label, match.ID)
	},
	Promote: func(match image.Summary) *image.DetailSummary {
		return &image.DetailSummary{
			ID:               match.ID,
			RepoTags:         match.RepoTags,
			RepoDigests:      match.RepoDigests,
			PinnedReferences: match.PinnedReferences,
			Size:             match.Size,
		}
	},
	IDOf: func(match image.Summary) string { return match.ID },
	Validate: func(details image.DetailSummary, identifier string) error {
		if details.ID == "" {
			return errors.Errorf("image lookup for %q returned empty ID", identifier)
		}
		return nil
	},
}

func searchImageCandidatesInternal(ctx context.Context, c *client.Client, identifier string) ([]image.Summary, error) {
	terms := imageSearchTermsInternal(identifier)
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

		searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Images(c.EnvID()), url.QueryEscape(term), cmdutil.ShowAllLimit)
		result, err := c.GetJSON[[]image.Summary](ctx, searchPath)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to search images")
		}

		identifierLower := strings.ToLower(identifier)
		matches := make([]image.Summary, 0)
		for _, item := range result.Data {
			if imageMatches(item, identifierLower, identifier) {
				matches = append(matches, item)
			}
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}

	return nil, nil
}

func imageSearchTermsInternal(identifier string) []string {
	terms := []string{identifier}
	identifierLower := strings.ToLower(identifier)
	if strings.Contains(identifier, "@") {
		if repository, _, ok := strings.Cut(identifier, "@"); ok && repository != "" {
			terms = append(terms, repository)
		}
		return terms
	}
	if strings.Contains(identifier, ":") && !strings.HasPrefix(identifierLower, "sha256:") {
		if repository, _, ok := strings.Cut(identifier, ":"); ok && repository != "" {
			terms = append(terms, repository)
		}
	}
	return terms
}

func imageMatches(item image.Summary, identifierLower, original string) bool {
	hasSeparator := strings.Contains(original, ":") || strings.Contains(original, "@")

	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}

	if !hasSeparator && strings.Contains(strings.ToLower(item.Repo), identifierLower) {
		return true
	}

	for _, tag := range item.RepoTags {
		tagLower := strings.ToLower(tag)
		if (!hasSeparator && strings.Contains(tagLower, identifierLower)) || strings.EqualFold(tag, original) {
			return true
		}
	}

	if item.Repo != "" && item.Tag != "" {
		combined := item.Repo + ":" + item.Tag
		combinedLower := strings.ToLower(combined)
		if strings.EqualFold(combined, original) || (hasSeparator && strings.Contains(combinedLower, identifierLower)) {
			return true
		}
	}

	for _, digest := range item.RepoDigests {
		digestLower := strings.ToLower(digest)
		if strings.EqualFold(digest, original) || strings.Contains(digestLower, identifierLower) {
			return true
		}
	}

	return false
}
