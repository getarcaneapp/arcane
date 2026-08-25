package images

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/spf13/cobra"
)

var imagesSearchCmd = &cobra.Command{
	Use:          "search <term>",
	Short:        "Search Docker Hub images",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.ImagesSearch(c.EnvID()) + "?term=" + url.QueryEscape(args[0])

		log.Debugf("Searching images from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to search images")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to search images")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var result struct {
			Success bool                 `json:"success"`
			Data    []image.SearchResult `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"NAME", "DESCRIPTION", "STARS", "OFFICIAL"}
		var rows [][]string
		for _, item := range result.Data {
			official := ""
			if item.Official {
				official = "Yes"
			}
			rows = append(rows, []string{item.Name, truncateCell(item.Description, 60), strconv.Itoa(item.StarCount), official})
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), int64(len(result.Data)), "results")

		return nil
	},
}

var imagesHistoryCmd = &cobra.Command{
	Use:          "history <image-id|name>",
	Short:        "Show image layer history",
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
		path := types.ImageHistory(c.EnvID(), imageID)

		log.Debugf("Getting image history from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get image history")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get image history")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result struct {
			Success bool                `json:"success"`
			Data    []image.HistoryItem `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"CREATED", "CREATED BY", "SIZE", "COMMENT"}
		var rows [][]string
		for _, item := range result.Data {
			created := ""
			if item.Created > 0 {
				created = time.Unix(item.Created, 0).Format(time.RFC3339)
			}
			rows = append(rows, []string{created, truncateCell(item.CreatedBy, 60), output.Bytes(item.Size), truncateCell(item.Comment, 30)})
		}

		output.Table(headers, rows)

		return nil
	},
}

var imagesTagCmd = &cobra.Command{
	Use:          "tag <image-id|name> <new-ref>",
	Short:        "Add a repository tag to an image",
	Args:         cobra.ExactArgs(2),
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

		repository, tag := splitImageRef(args[1])
		if repository == "" {
			return errors.New("target repository is required")
		}

		path := types.ImageTag(c.EnvID(), imageID)

		log.Debugf("Tagging image via: %s", path)

		resp, err := c.Post(cmd.Context(), path, image.TagRequest{Repository: repository, Tag: tag})
		if err != nil {
			return errors.WrapIf(err, "failed to tag image")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to tag image")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result struct {
			Success bool                 `json:"success"`
			Data    base.MessageResponse `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Success("%s", result.Data.Message)

		return nil
	},
}

var imagesExportCmd = &cobra.Command{
	Use:          "export <image-id|name> [output-file]",
	Short:        "Download an image as a tar archive",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		// Exporting large images can take a long time
		c.SetTimeout(30 * time.Minute)

		allowPrompt := prompt.IsInteractive()

		imageID, err := resolveImageID(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		outputFile := exportFileName(args[0])
		if len(args) == 2 {
			outputFile = args[1]
		}

		path := types.ImageExport(c.EnvID(), imageID)

		log.Debugf("Exporting image from: %s", path)

		resp, err := c.RequestRaw(cmd.Context(), http.MethodGet, path, nil, nil)
		if err != nil {
			return errors.WrapIf(err, "failed to export image")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to export image")
		}

		file, err := os.Create(outputFile)
		if err != nil {
			return errors.WrapIf(err, "failed to create output file")
		}

		written, err := io.Copy(file, resp.Body)
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(outputFile)
			return errors.WrapIf(err, "failed to write image archive")
		}

		output.Success("Saved %s (%s)", outputFile, output.Bytes(written))

		return nil
	},
}

var (
	attestationsPlatform      string
	attestationsPredicateType string
	attestationsStatement     bool
)

var imagesAttestationsCmd = &cobra.Command{
	Use:          "attestations <image>",
	Short:        "Show in-toto attestations attached to an image",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		imageName := strings.TrimSpace(args[0])
		if imageName == "" {
			return errors.New("image identifier is required")
		}

		u, err := url.Parse(types.ImageAttestations(c.EnvID(), imageName))
		if err != nil {
			return errors.WrapIf(err, "failed to parse endpoint path")
		}
		q := u.Query()
		if attestationsPlatform != "" {
			q.Set("platform", attestationsPlatform)
		}
		if attestationsPredicateType != "" {
			q.Set("predicateType", attestationsPredicateType)
		}
		if attestationsStatement {
			q.Set("statement", "true")
		}
		u.RawQuery = q.Encode()
		path := u.String()

		log.Debugf("Getting image attestations from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get image attestations")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get image attestations")
		}

		if cmdutil.JSONOutputEnabled(cmd) || attestationsStatement {
			return cmdutil.PrintRawJSON(body)
		}

		var result struct {
			Success bool                  `json:"success"`
			Data    image.AttestationList `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		output.Header("Image Attestations")
		output.KeyValue("Image", result.Data.ImageRef)
		output.KeyValue("Subject Digest", result.Data.SubjectDigest)
		if result.Data.Platform != "" {
			output.KeyValue("Platform", result.Data.Platform)
		}

		if len(result.Data.Attestations) == 0 {
			output.Info("No attestations found")
			return nil
		}

		headers := []string{"PREDICATE TYPE", "PLATFORM", "DIGEST", "SIZE"}
		var rows [][]string
		for _, att := range result.Data.Attestations {
			rows = append(rows, []string{att.PredicateType, att.Platform, truncateCell(att.Digest, 19), output.Bytes(att.Size)})
		}
		fmt.Println()
		output.Table(headers, rows)

		return nil
	},
}

var (
	buildTags       []string
	buildDockerfile string
	buildInlineFile string
	buildArgs       []string
	buildTarget     string
	buildPlatforms  []string
	buildNetwork    string
	buildProvider   string
	buildNoCache    bool
	buildPull       bool
	buildPush       bool
	buildLoad       bool
)

var imagesBuildCmd = &cobra.Command{
	Use:          "build <context-dir|git-url>",
	Short:        "Build an image with BuildKit on the server",
	Long:         "Build a Docker image using BuildKit with streaming progress output.\n\nThe context argument is a build context directory on the Arcane server or a Git URL.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		// Builds can take a long time
		c.SetTimeout(30 * time.Minute)

		requestBody := map[string]any{
			"contextDir": args[0],
		}
		if buildDockerfile != "" {
			requestBody["dockerfile"] = buildDockerfile
		}
		if buildInlineFile != "" {
			content, err := os.ReadFile(buildInlineFile)
			if err != nil {
				return errors.WrapIf(err, "failed to read Dockerfile")
			}
			requestBody["dockerfileInline"] = string(content)
		}
		if len(buildTags) > 0 {
			requestBody["tags"] = buildTags
		}
		if buildTarget != "" {
			requestBody["target"] = buildTarget
		}
		if len(buildArgs) > 0 {
			parsed := make(map[string]string, len(buildArgs))
			for _, arg := range buildArgs {
				key, value, ok := strings.Cut(arg, "=")
				if !ok || key == "" {
					return errors.Errorf("invalid build arg %q; expected KEY=VALUE", arg)
				}
				parsed[key] = value
			}
			requestBody["buildArgs"] = parsed
		}
		if len(buildPlatforms) > 0 {
			requestBody["platforms"] = buildPlatforms
		}
		if buildNetwork != "" {
			requestBody["network"] = buildNetwork
		}
		if buildProvider != "" {
			requestBody["provider"] = buildProvider
		}
		if buildNoCache {
			requestBody["noCache"] = true
		}
		if buildPull {
			requestBody["pull"] = true
		}
		if buildPush {
			requestBody["push"] = true
		}
		if buildLoad {
			requestBody["load"] = true
		}

		path := types.ImagesBuild(c.EnvID())

		log.Debugf("Building image via: %s", path)

		resp, err := c.Post(cmd.Context(), path, requestBody)
		if err != nil {
			return errors.WrapIf(err, "failed to build image")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to build image")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			_, err = io.Copy(cmd.OutOrStdout(), resp.Body)
			if err != nil {
				return errors.WrapIf(err, "failed to read build stream")
			}
			return nil
		}

		if err := streamBuildOutput(resp.Body); err != nil {
			return err
		}

		output.Success("Image built successfully")

		return nil
	},
}

// streamBuildOutput prints the build NDJSON stream: control frames (activity,
// done, error) are interpreted, everything else is raw build output.
func streamBuildOutput(body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	done := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{") {
			var frame struct {
				Type  string `json:"type"`
				Done  bool   `json:"done"`
				Error string `json:"error"`
			}
			if json.Unmarshal([]byte(trimmed), &frame) == nil {
				if frame.Error != "" {
					return errors.Errorf("build error: %s", frame.Error)
				}
				if frame.Done {
					done = true
					continue
				}
				if frame.Type != "" {
					continue
				}
			}
		}
		fmt.Println(line)
	}
	if err := scanner.Err(); err != nil {
		return errors.WrapIf(err, "failed to read build stream")
	}
	if !done {
		return errors.New("build stream ended without completion frame")
	}
	return nil
}

var (
	buildsLimit    int
	buildsStart    int
	buildsAll      bool
	buildsStatus   string
	buildsProvider string
)

var imagesBuildsCmd = &cobra.Command{
	Use:          "builds",
	Short:        "List image build history",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		u, err := url.Parse(types.ImageBuilds(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to parse endpoint path")
		}
		q := u.Query()
		if buildsStatus != "" {
			q.Set("status", buildsStatus)
		}
		if buildsProvider != "" {
			q.Set("provider", buildsProvider)
		}
		u.RawQuery = q.Encode()

		path, err := cmdutil.ApplyPaginationParams(cmd, u.String(), cmdutil.ListParams{
			Resource:        "builds",
			Limit:           buildsLimit,
			FallbackDefault: 0,
			Start:           buildsStart,
			All:             buildsAll,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		log.Debugf("Listing image builds from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list image builds")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list image builds")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[image.BuildRecord]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"ID", "IMAGE", "STATUS", "STARTED", "DURATION"}
		var rows [][]string
		for _, record := range result.Data {
			rows = append(rows, []string{
				record.ID,
				buildImageLabel(record),
				output.TintStatus(record.Status),
				record.CreatedAt.Format(time.RFC3339),
				buildDurationLabel(record.DurationMs),
			})
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "builds")

		return nil
	},
}

var imagesBuildsGetCmd = &cobra.Command{
	Use:          "get <build-id>",
	Short:        "Get an image build history entry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := logger.GetLogger()
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.ImageBuild(c.EnvID(), args[0])

		log.Debugf("Getting image build from: %s", path)

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get image build")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get image build")
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintRawJSON(body)
		}

		var result struct {
			Success bool              `json:"success"`
			Data    image.BuildRecord `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		record := result.Data

		output.Header("Build Details")
		output.KeyValue("ID", record.ID)
		output.KeyValue("Status", output.TintStatus(record.Status))
		if record.Provider != "" {
			output.KeyValue("Provider", record.Provider)
		}
		output.KeyValue("Context", record.ContextDir)
		if record.Dockerfile != "" {
			output.KeyValue("Dockerfile", record.Dockerfile)
		}
		if len(record.Tags) > 0 {
			output.KeyValue("Tags", strings.Join(record.Tags, ", "))
		}
		if len(record.Platforms) > 0 {
			output.KeyValue("Platforms", strings.Join(record.Platforms, ", "))
		}
		if record.Username != nil && *record.Username != "" {
			output.KeyValue("User", *record.Username)
		}
		output.KeyValue("Started", record.CreatedAt.Format(time.RFC3339))
		if record.CompletedAt != nil {
			output.KeyValue("Completed", record.CompletedAt.Format(time.RFC3339))
		}
		output.KeyValue("Duration", buildDurationLabel(record.DurationMs))
		if record.Digest != nil && *record.Digest != "" {
			output.KeyValue("Digest", *record.Digest)
		}
		if record.ErrorMessage != nil && *record.ErrorMessage != "" {
			output.KeyValue("Error", *record.ErrorMessage)
		}

		if record.Output != nil && *record.Output != "" {
			output.Header("Output")
			fmt.Println(*record.Output)
			if record.OutputTruncated {
				output.Warning("Output was truncated")
			}
		}

		return nil
	},
}

func buildImageLabel(record image.BuildRecord) string {
	if len(record.Tags) > 0 {
		return record.Tags[0]
	}
	return truncateCell(record.ContextDir, 40)
}

func buildDurationLabel(durationMs *int64) string {
	if durationMs == nil {
		return ""
	}
	return (time.Duration(*durationMs) * time.Millisecond).Round(time.Second).String()
}

// splitImageRef splits an image reference into repository and tag. The tag is
// only split off when the text after the final colon contains no slash, so
// registry ports (registry:5000/repo) stay part of the repository.
func splitImageRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if idx := strings.LastIndex(ref, ":"); idx > 0 && !strings.Contains(ref[idx+1:], "/") {
		return ref[:idx], ref[idx+1:]
	}
	return ref, ""
}

// exportFileName mirrors the server's default export filename for an image
// reference, replacing separators with underscores.
func exportFileName(imageName string) string {
	name := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(strings.TrimSpace(imageName))
	name = strings.Trim(name, "._-")
	if name == "" {
		name = "image"
	}
	return name + ".tar"
}

func truncateCell(value string, limit int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if limit > 3 && len(value) > limit {
		return value[:limit-3] + "..."
	}
	return value
}

func init() {
	ImagesCmd.AddCommand(imagesSearchCmd)

	ImagesCmd.AddCommand(imagesHistoryCmd)

	ImagesCmd.AddCommand(imagesTagCmd)

	ImagesCmd.AddCommand(imagesExportCmd)

	ImagesCmd.AddCommand(imagesAttestationsCmd)
	imagesAttestationsCmd.Flags().StringVar(&attestationsPlatform, "platform", "", "OCI platform selector (e.g. linux/amd64)")
	imagesAttestationsCmd.Flags().StringVar(&attestationsPredicateType, "predicate-type", "", "Exact in-toto predicate type URI to include")
	imagesAttestationsCmd.Flags().BoolVar(&attestationsStatement, "statement", false, "Include verbatim statement JSON bodies (implies JSON output)")

	ImagesCmd.AddCommand(imagesBuildCmd)
	imagesBuildCmd.Flags().StringArrayVarP(&buildTags, "tag", "t", nil, "Image tag (repeatable)")
	imagesBuildCmd.Flags().StringVarP(&buildDockerfile, "file", "f", "", "Dockerfile path within the build context")
	imagesBuildCmd.Flags().StringVar(&buildInlineFile, "dockerfile-inline", "", "Local Dockerfile whose content is sent as the inline Dockerfile")
	imagesBuildCmd.Flags().StringArrayVar(&buildArgs, "build-arg", nil, "Build argument KEY=VALUE (repeatable)")
	imagesBuildCmd.Flags().StringVar(&buildTarget, "target", "", "Target build stage")
	imagesBuildCmd.Flags().StringArrayVar(&buildPlatforms, "platform", nil, "Target platform (repeatable)")
	imagesBuildCmd.Flags().StringVar(&buildNetwork, "network", "", "Build network mode")
	imagesBuildCmd.Flags().StringVar(&buildProvider, "provider", "", "Build provider override")
	imagesBuildCmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "Disable build cache")
	imagesBuildCmd.Flags().BoolVar(&buildPull, "pull", false, "Always pull referenced base images")
	imagesBuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push the image after building")
	imagesBuildCmd.Flags().BoolVar(&buildLoad, "load", false, "Load the image into the local Docker daemon")

	ImagesCmd.AddCommand(imagesBuildsCmd)
	imagesBuildsCmd.Flags().IntVarP(&buildsLimit, "limit", "n", 0, "Number of builds to show (server default 20)")
	imagesBuildsCmd.Flags().IntVar(&buildsStart, "start", 0, cmdutil.StartFlagUsage)
	imagesBuildsCmd.Flags().BoolVarP(&buildsAll, "all", "a", false, cmdutil.AllFlagUsage)
	imagesBuildsCmd.Flags().StringVar(&buildsStatus, "status", "", "Filter by status")
	imagesBuildsCmd.Flags().StringVar(&buildsProvider, "provider", "", "Filter by provider")

	imagesBuildsCmd.AddCommand(imagesBuildsGetCmd)
}
