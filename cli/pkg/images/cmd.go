package images

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/upload"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/cli/v2/pkg/images/updates"
)

// Command builds the images command tree.
func Command() *cobra.Command {
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeResource},
		Use:         "images",
		Aliases:     []string{"image", "i"},
		Short:       "Manage images",
	}
	updateCommand := updates.Command()
	for _, child := range updateCommand.Commands() {
		if child.Name() == "check-image" {
			cmdutil.SetResource(ImageRef, child)
		}
	}
	command.AddCommand(
		newListCmd(), newGetCmd(), newRemoveCmd(), newPullCmd(), newPruneCmd(),
		newCountsCmd(), newUploadCmd(), newSearchCmd(), newHistoryCmd(), newTagCmd(),
		newExportCmd(), newAttestationsCmd(), newBuildCmd(), newBuildsCmd(), updateCommand,
	)
	return command
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit         int
		start         int
		sort          string
		order         string
		search        string
		inUseOnly     bool
		unusedOnly    bool
		all           bool
		updatesFilter string
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeMerged},
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List images",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			query := url.Values{}
			if opts.sort != "" {
				query.Set("sort", opts.sort)
			}
			if opts.order != "" {
				query.Set("order", opts.order)
			}
			if opts.search != "" {
				query.Set("search", opts.search)
			}
			// Filter server-side so that pagination and the reported totals apply to
			// the matching set rather than to whichever page happened to be fetched.
			if opts.inUseOnly {
				query.Set("inUse", "true")
			}
			if opts.unusedOnly {
				query.Set("inUse", "false")
			}
			if opts.updatesFilter != "" {
				query.Set("updates", opts.updatesFilter)
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[image.Summary]{
				Resource: "images",
				Endpoint: types.Images(c.EnvID()),
				Params: cmdutil.ListParams{
					Resource:        "images",
					Limit:           opts.limit,
					FallbackDefault: 0,
					Start:           opts.start,
					All:             opts.all,
				},
				Query:   query,
				Headers: []string{"ID", "REPOSITORY:TAG", "SIZE", "IN USE"},
				Row: func(img image.Summary) []string {
					tag := "<none>"
					if len(img.RepoTags) > 0 {
						tag = img.RepoTags[0]
					}
					return []string{img.ID, tag, output.Bytes(img.Size), kit.Ternary(img.InUse, "Yes", "No")}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 0, "Number of images to show (server default 20)")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.EnumVarP(command, command.Flags(), &opts.updatesFilter, "updates", "", "", "Filter by update status", "has_update", "up_to_date", "error", "unknown")
	command.Flags().StringVar(&opts.sort, "sort", "", "Field to sort by")
	cmdutil.EnumVarP(command, command.Flags(), &opts.order, "order", "", "", "Sort order", "asc", "desc")
	command.Flags().StringVar(&opts.search, "search", "", "Search query")
	command.Flags().BoolVar(&opts.inUseOnly, "inuse", false, "Only show images currently in use")
	command.Flags().BoolVar(&opts.unusedOnly, "unused", false, "Only show images not in use")
	command.PreRunE = func(cmd *cobra.Command, _ []string) error { return cmdutil.ExclusiveFlags(cmd, "inuse", "unused") }
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
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

			allowPrompt := cmdutil.AllowPrompt(cmd)

			resolved, _, err := ImageRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}
			path := types.Image(c.EnvID(), resolved.ID)

			log.Debugf("Getting image details from: %s", path)

			return cmdutil.Run(cmd, c, cmdutil.Spec[image.DetailSummary]{
				Method: http.MethodGet, Path: path, Body: nil, FailureMessage: "failed to get image",
				Render: func(data image.DetailSummary) error {
					output.Header("Image Details")
					output.KeyValue("ID", data.ID)
					if len(data.RepoTags) > 0 {
						output.KeyValue("Tags", strings.Join(data.RepoTags, ", "))
					}
					output.KeyValue("Size", output.Bytes(data.Size))
					output.KeyValue("Architecture", data.Architecture)
					output.KeyValue("OS", data.Os)
					if data.Created != "" {
						output.KeyValue("Created", data.Created)
					}
					if data.Author != "" {
						output.KeyValue("Author", data.Author)
					}

					if data.Config.WorkingDir != "" {
						output.KeyValue("Working Dir", data.Config.WorkingDir)
					}

					if len(data.Config.Cmd) > 0 {
						output.KeyValue("Cmd", strings.Join(data.Config.Cmd, " "))
					}

					if len(data.Config.Env) > 0 {
						output.Header("Environment Variables")
						for _, env := range data.Config.Env {
							_, _ = fmt.Fprintln(output.Stdout(), env)
						}
					}

					if len(data.Config.ExposedPorts) > 0 {
						output.Header("Exposed Ports")
						for _, p := range slices.Sorted(maps.Keys(data.Config.ExposedPorts)) {
							_, _ = fmt.Fprintln(output.Stdout(), p)
						}
					}

					return nil
				},
			})
		},
	}
}

func newRemoveCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
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

			if opts.force {
				u, parseErr := url.Parse(path)
				if parseErr != nil {
					return fmt.Errorf("failed to parse path: %w", parseErr)
				}
				q := u.Query()
				q.Set("force", "true")
				u.RawQuery = q.Encode()
				path = u.String()
			}

			log.Debugf("Removing image from: %s", path)

			confirmed, confirmErr := cmdutil.ConfirmOrCancel(cmd, opts.force, fmt.Sprintf("Are you sure you want to remove image %s?", args[0]))
			if confirmErr != nil || !confirmed {
				return confirmErr
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method: http.MethodDelete, Path: path, FailureMessage: "failed to remove image",
				Render: func(data base.MessageResponse) error {
					output.Success("%s", data.Message)
					return nil
				},
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force removal of image")
	return command
}

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeDestination},
		Use:          "pull <image>",
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

			requestBody := image.PullOptions{ImageName: imageName}

			resp, err := c.Post(cmd.Context(), path, requestBody)
			if err != nil {
				return fmt.Errorf("failed to pull image: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if statusErr := cmdutil.EnsureSuccessStatus(resp); statusErr != nil {
				return fmt.Errorf("failed to pull image: %w", statusErr)
			}

			if !cmdutil.JSONOutputEnabled(cmd) {
				output.Info("Pulling image: %s", imageName)
			}
			return readPullStream(cmd, resp.Body)
		},
	}
}

func newPruneCmd() *cobra.Command {
	var opts struct {
		dangling bool
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeFleet},
		Use:          "prune",
		Short:        "Remove unused images",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.GetLogger()
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			path := types.ImagesPrune(c.EnvID())

			log.Debugf("Pruning images from: %s", path)

			requestBody := map[string]any{
				"dangling": opts.dangling,
			}

			confirmed, confirmErr := cmdutil.ConfirmOrCancel(cmd, false, "Are you sure you want to remove unused images?")
			if confirmErr != nil || !confirmed {
				return confirmErr
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[image.PruneReport]{
				Method: http.MethodPost, Path: path, Body: requestBody, FailureMessage: "failed to prune images",
				Render: func(data image.PruneReport) error {
					output.Success("Pruned %d images, reclaimed %s", len(data.ImagesDeleted), output.Bytes(data.SpaceReclaimed))

					return nil
				},
			})
		},
	}
	command.Flags().BoolVar(&opts.dangling, "dangling", false, "Only remove dangling images")
	return command
}

func newCountsCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeMerged},
		Use:          "counts",
		Short:        "Get image usage counts",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.GetLogger()
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			path := types.ImagesCounts(c.EnvID())

			log.Debugf("Getting image counts from: %s", path)

			return cmdutil.Run(cmd, c, cmdutil.Spec[image.UsageCounts]{
				Method: http.MethodGet, Path: path, Body: nil, FailureMessage: "failed to get image counts",
				Render: func(data image.UsageCounts) error {
					output.Header("Image Usage Counts")
					output.KeyValue("In Use", data.Inuse)
					output.KeyValue("Unused", data.Unused)
					output.KeyValue("Total", data.Total)
					output.KeyValue("Total Size", output.Bytes(data.TotalSize))

					return nil
				},
			})
		},
	}
}

func newUploadCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeDestination},
		Use:          "upload <file>",
		Short:        "Upload a Docker image from a tar archive",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			// Uploading large images can take a long time
			c.SetTimeout(30 * time.Minute)

			filePath := args[0]
			output.Info("Uploading image: %s", filePath)

			jsonOutput := cmdutil.JSONOutputEnabled(cmd)
			sessionID, err := cmdutil.UploadFileInChunks(cmd.Context(), c, upload.KindImage, filePath, !jsonOutput)
			if err != nil {
				return err
			}

			uploadErr := cmdutil.Run(cmd, c, cmdutil.Spec[image.LoadResult]{
				Method: http.MethodPost, Path: types.ImagesUpload(c.EnvID()),
				Body: upload.ConsumeRequest{UploadID: sessionID}, FailureMessage: "failed to upload image",
				Render: func(image.LoadResult) error {
					output.Success("Image uploaded successfully")
					return nil
				},
			})
			if uploadErr != nil {
				cmdutil.AbortUploadSession(cmd.Context(), c, upload.KindImage, sessionID)
			}
			return uploadErr
		},
	}
}

var ImageRef = cmdutil.ResourceRef[image.DetailSummary, image.Summary]{
	EnvironmentScoped: true,
	Singular:          "image",
	Plural:            "images",
	IDHint:            "the image ID",
	ListCmd:           "arcane images list",
	GetPath:           types.Image,
	ListPath:          types.Images,
	SearchCandidates:  searchImageCandidates,
	Matches:           imageMatches,
	Label: func(match image.Summary) string {
		label := match.ID
		switch {
		case len(match.RepoTags) > 0:
			label = match.RepoTags[0]
		case match.Repo != "" && match.Tag != "":
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
	IDOf:         func(match image.Summary) string { return match.ID },
	IdentifierOf: func(details image.DetailSummary) string { return details.ID },
	Validate: func(details image.DetailSummary, identifier string) error {
		if details.ID == "" {
			return fmt.Errorf("image lookup for %q returned empty ID", identifier)
		}
		return nil
	},
}

func searchImageCandidates(ctx context.Context, c *client.Client, identifier string) ([]image.Summary, error) {
	terms := []string{identifier}
	separator := kit.Ternary(strings.Contains(identifier, "@"), "@", ":")
	if separator == "@" || !strings.HasPrefix(strings.ToLower(identifier), "sha256:") {
		if repository, _, ok := strings.Cut(identifier, separator); ok && repository != "" {
			terms = append(terms, repository)
		}
	}
	for _, term := range kit.Unique(kit.TrimNonEmpty(terms)) {
		searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Images(c.EnvID()), url.QueryEscape(term), cmdutil.ShowAllLimit)
		result, err := c.GetJSON[[]image.Summary](ctx, searchPath)
		if err != nil {
			return nil, fmt.Errorf("failed to search images: %w", err)
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

func imageMatches(item image.Summary, identifierLower, original string) bool {
	hasSeparator := strings.Contains(original, ":") || strings.Contains(original, "@")
	idLower := strings.ToLower(item.ID)
	// Qualified refs match exactly or by registry/namespace suffix, so repo:1 never matches repo:10.
	refMatches := func(ref string) bool {
		refLower := strings.ToLower(ref)
		return refLower == identifierLower || (hasSeparator && strings.HasSuffix(refLower, "/"+identifierLower)) ||
			(!hasSeparator && strings.Contains(refLower, identifierLower))
	}
	return idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) ||
		(!hasSeparator && strings.Contains(strings.ToLower(item.Repo), identifierLower)) ||
		slices.ContainsFunc(item.RepoTags, refMatches) ||
		(item.Repo != "" && item.Tag != "" && refMatches(item.Repo+":"+item.Tag)) ||
		slices.ContainsFunc(item.RepoDigests, func(digest string) bool { return strings.Contains(strings.ToLower(digest), identifierLower) })
}

func readPullStream(cmd *cobra.Command, reader io.Reader) error {
	jsonMode := cmdutil.JSONOutputEnabled(cmd)
	frames := make([]jsontext.Value, 0)
	decoder := jsontext.NewDecoder(reader)
	loading := output.StartTracker(cmd.Context(), cmd.ErrOrStderr(), !jsonMode)
	defer loading.Stop()
	for {
		frame, readErr := decoder.ReadValue()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return fmt.Errorf("failed to decode stream: %w", readErr)
		}
		var event struct {
			Type           string `json:"type"`
			Status         string `json:"status"`
			Error          string `json:"error"`
			ID             string `json:"id"`
			Log            string `json:"log"`
			Done           bool   `json:"done"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if decodeErr := json.Unmarshal(frame, &event); decodeErr != nil {
			return fmt.Errorf("failed to decode stream: %w", decodeErr)
		}
		if jsonMode {
			frames = append(frames, frame.Clone())
		}
		if event.Error != "" {
			pullErr := fmt.Errorf("pull error: %s", event.Error)
			loading.Finish(event.ID, pullErr)
			return pullErr
		}
		if event.Done {
			break
		}
		// Servers render Docker progress as "<layer>: <status>" log lines.
		if id, status, ok := strings.Cut(event.Log, ": "); ok && len(id) == 12 && strings.Trim(id, "0123456789abcdef") == "" {
			event.ID, event.Status = id, status
		} else if event.Log != "" {
			event.Status = event.Log
		}
		if jsonMode || event.Status == "" {
			continue
		}
		if event.ID != "" {
			loading.SetProgress(event.ID, event.Status+" "+event.ID, event.ProgressDetail.Current, event.ProgressDetail.Total, true)
			if slices.Contains([]string{"Download complete", "Pull complete", "Already exists"}, event.Status) {
				loading.Finish(event.ID, nil)
			}
		}
		if (!output.IsTerminal(cmd.ErrOrStderr()) || !output.IsTerminal(cmd.OutOrStdout()) || event.ID == "") && event.Status != "Downloading" && event.Status != "Extracting" {
			resume := output.SuspendProgress()
			if event.ID != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", event.ID, event.Status)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", event.Status)
			}
			resume()
		}
	}
	loading.Stop()
	if jsonMode {
		return cmdutil.PrintJSON(frames)
	}
	output.Success("Image pulled successfully")
	return nil
}
