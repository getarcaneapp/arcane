package images

import (
	"bufio"
	"cmp"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/spf13/cobra"
	buildtypes "go.getarcane.app/builds/types"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeRead},
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]image.SearchResult]{
				Method:         http.MethodGet,
				Path:           path,
				FailureMessage: "failed to search images",
				Render: func(data []image.SearchResult) error {
					headers := []string{"NAME", "DESCRIPTION", "STARS", "OFFICIAL"}
					var rows [][]string
					for _, item := range data {
						rows = append(rows, []string{item.Name, truncateCell(item.Description, 60), strconv.Itoa(item.StarCount), kit.Ternary(item.Official, "Yes", "")})
					}

					return cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: "results", Total: int64(len(data)), Paginated: true})
				},
			})
		},
	}
}

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
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

			allowPrompt := cmdutil.AllowPrompt(cmd)

			resolved, _, err := ImageRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}
			imageID := resolved.ID
			path := types.ImageHistory(c.EnvID(), imageID)

			log.Debugf("Getting image history from: %s", path)

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]image.HistoryItem]{
				Method: http.MethodGet, Path: path, Body: nil, FailureMessage: "failed to get image history",
				Render: func(data []image.HistoryItem) error {
					headers := []string{"CREATED", "CREATED BY", "SIZE", "COMMENT"}
					var rows [][]string
					for _, item := range data {
						created := ""
						if item.Created > 0 {
							created = time.Unix(item.Created, 0).Format(time.RFC3339)
						}
						rows = append(rows, []string{created, truncateCell(item.CreatedBy, 60), output.Bytes(item.Size), truncateCell(item.Comment, 30)})
					}

					return cmdutil.PrintTable(cmd, headers, rows)
				},
			})
		},
	}
}

func newTagCmd() *cobra.Command {
	return &cobra.Command{
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

			allowPrompt := cmdutil.AllowPrompt(cmd)

			resolved, _, err := ImageRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}
			imageID := resolved.ID

			repository, tag := strings.TrimSpace(args[1]), ""
			if index := strings.LastIndex(repository, ":"); index > 0 && !strings.Contains(repository[index+1:], "/") {
				repository, tag = repository[:index], repository[index+1:]
			}
			if repository == "" {
				return errors.New("target repository is required")
			}

			path := types.ImageTag(c.EnvID(), imageID)

			log.Debugf("Tagging image via: %s", path)

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method: http.MethodPost, Path: path, Body: image.TagRequest{Repository: repository, Tag: tag}, FailureMessage: "failed to tag image",
				Render: func(data base.MessageResponse) error {
					output.Success("%s", data.Message)

					return nil
				},
			})
		},
	}
}

func newExportCmd() *cobra.Command {
	return &cobra.Command{
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

			allowPrompt := cmdutil.AllowPrompt(cmd)

			resolved, _, err := ImageRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}
			imageID := resolved.ID

			name := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(strings.TrimSpace(args[0]))
			outputFile := cmp.Or(strings.Trim(name, "._-"), "image") + ".tar"
			if len(args) == 2 {
				outputFile = args[1]
			}

			path := types.ImageExport(c.EnvID(), imageID)

			log.Debugf("Exporting image from: %s", path)

			resp, err := c.RequestRaw(cmd.Context(), http.MethodGet, path, nil, nil)
			if err != nil {
				return fmt.Errorf("failed to export image: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if ensureSuccessStatusErr := cmdutil.EnsureSuccessStatus(resp); ensureSuccessStatusErr != nil {
				return fmt.Errorf("failed to export image: %w", ensureSuccessStatusErr)
			}

			file, err := os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}

			written, err := output.Copy(cmd.Context(), file, resp.Body, resp.ContentLength, "Exporting image", !cmdutil.JSONOutputEnabled(cmd))
			if closeErr := file.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(outputFile)
				return fmt.Errorf("failed to write image archive: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path  string `json:"path"`
					Bytes int64  `json:"bytes"`
				}{Path: outputFile, Bytes: written})
			}
			output.Success("Saved %s (%s)", outputFile, output.Bytes(written))

			return nil
		},
	}
}

func newAttestationsCmd() *cobra.Command {
	var opts struct {
		platform      string
		predicateType string
		statement     bool
	}
	command := &cobra.Command{
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
				return fmt.Errorf("failed to parse endpoint path: %w", err)
			}
			q := u.Query()
			if opts.platform != "" {
				q.Set("platform", opts.platform)
			}
			if opts.predicateType != "" {
				q.Set("predicateType", opts.predicateType)
			}
			if opts.statement {
				q.Set("statement", "true")
			}
			u.RawQuery = q.Encode()
			path := u.String()

			log.Debugf("Getting image attestations from: %s", path)

			return cmdutil.Run(cmd, c, cmdutil.Spec[jsontext.Value]{
				Method: http.MethodGet, Path: path, Body: nil, FailureMessage: "failed to get image attestations",
				Render: func(raw jsontext.Value) error {
					if opts.statement {
						return cmdutil.PrintRawJSON(raw)
					}
					var data image.AttestationList
					if decodeErr := json.Unmarshal(raw, &data); decodeErr != nil {
						return fmt.Errorf("failed to parse response: %w", decodeErr)
					}
					output.Header("Image Attestations")
					output.KeyValue("Image", data.ImageRef)
					output.KeyValue("Subject Digest", data.SubjectDigest)
					if data.Platform != "" {
						output.KeyValue("Platform", data.Platform)
					}

					if len(data.Attestations) == 0 {
						output.Info("No attestations found")
						return nil
					}

					headers := []string{"PREDICATE TYPE", "PLATFORM", "DIGEST", "SIZE"}
					var rows [][]string
					for _, att := range data.Attestations {
						rows = append(rows, []string{att.PredicateType, att.Platform, truncateCell(att.Digest, 19), output.Bytes(att.Size)})
					}
					_, _ = fmt.Fprintln(output.Stdout())
					if tableErr := cmdutil.PrintTable(cmd, headers, rows); tableErr != nil {
						return tableErr
					}

					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.platform, "platform", "", "OCI platform selector (e.g. linux/amd64)")
	command.Flags().StringVar(&opts.predicateType, "predicate-type", "", "Exact in-toto predicate type URI to include")
	command.Flags().BoolVar(&opts.statement, "statement", false, "Include verbatim statement JSON bodies (implies JSON output)")
	cmdutil.SetResource(ImageRef, command)
	return command
}

func newBuildCmd() *cobra.Command {
	var opts struct {
		tags       []string
		dockerfile string
		inlineFile string
		args       []string
		target     string
		platforms  []string
		network    string
		provider   string
		noCache    bool
		pull       bool
		push       bool
		load       bool
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeDestination},
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

			requestBody := buildtypes.BuildRequest{
				ContextDir: args[0], Dockerfile: opts.dockerfile,
				Tags: opts.tags, Target: opts.target,
				Platforms: opts.platforms, Network: opts.network,
				Provider: opts.provider, NoCache: opts.noCache,
				Pull: opts.pull, Push: opts.push, Load: opts.load,
			}
			if opts.inlineFile != "" {
				content, readFileErr := os.ReadFile(opts.inlineFile)
				if readFileErr != nil {
					return fmt.Errorf("failed to read Dockerfile: %w", readFileErr)
				}
				requestBody.DockerfileInline = string(content)
			}
			if len(opts.args) > 0 {
				requestBody.BuildArgs = make(map[string]string, len(opts.args))
				for _, arg := range opts.args {
					key, value, valid := strings.Cut(arg, "=")
					if !valid || key == "" {
						return fmt.Errorf("invalid build arg %q; expected KEY=VALUE", arg)
					}
					requestBody.BuildArgs[key] = value
				}
			}
			// The build contract uses legacy omitempty semantics for booleans and numbers.
			payload, marshalErr := jsonv1.Marshal(requestBody)
			if marshalErr != nil {
				return fmt.Errorf("failed to build image: %w", marshalErr)
			}

			path := types.ImagesBuild(c.EnvID())

			log.Debugf("Building image via: %s", path)

			resp, err := c.Post(cmd.Context(), path, jsontext.Value(payload))
			if err != nil {
				return fmt.Errorf("failed to build image: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if ensureSuccessStatusErr := cmdutil.EnsureSuccessStatus(resp); ensureSuccessStatusErr != nil {
				return fmt.Errorf("failed to build image: %w", ensureSuccessStatusErr)
			}

			return readBuildStream(cmd, resp.Body)
		},
	}
	command.Flags().StringArrayVarP(&opts.tags, "tag", "t", nil, "Image tag (repeatable)")
	command.Flags().StringVarP(&opts.dockerfile, "file", "f", "", "Dockerfile path within the build context")
	command.Flags().StringVar(&opts.inlineFile, "dockerfile-inline", "", "Local Dockerfile whose content is sent as the inline Dockerfile")
	command.Flags().StringArrayVar(&opts.args, "build-arg", nil, "Build argument KEY=VALUE (repeatable)")
	command.Flags().StringVar(&opts.target, "target", "", "Target build stage")
	command.Flags().StringArrayVar(&opts.platforms, "platform", nil, "Target platform (repeatable)")
	command.Flags().StringVar(&opts.network, "network", "", "Build network mode")
	cmdutil.EnumVarP(command, command.Flags(), &opts.provider, "provider", "", "", "Build provider override", "local", "depot")
	command.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable build cache")
	command.Flags().BoolVar(&opts.pull, "pull", false, "Always pull referenced base images")
	command.Flags().BoolVar(&opts.push, "push", false, "Push the image after building")
	command.Flags().BoolVar(&opts.load, "load", false, "Load the image into the local Docker daemon")
	return command
}

func newBuildsCmd() *cobra.Command {
	var opts struct {
		limit    int
		start    int
		all      bool
		status   string
		provider string
	}
	command := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeMerged},
		Use:          "builds",
		Short:        "List image build history",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			query := url.Values{}
			if opts.status != "" {
				query.Set("status", opts.status)
			}
			if opts.provider != "" {
				query.Set("provider", opts.provider)
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[image.BuildRecord]{
				Resource: "builds",
				Endpoint: types.ImageBuilds(c.EnvID()),
				Params:   cmdutil.ListParams{Resource: "builds", Limit: opts.limit, FallbackDefault: 0, Start: opts.start, All: opts.all},
				Query:    query,
				Headers:  []string{"ID", "IMAGE", "STATUS", "STARTED", "DURATION"},
				Row: func(record image.BuildRecord) []string {
					label := truncateCell(record.ContextDir, 40)
					if len(record.Tags) > 0 {
						label = record.Tags[0]
					}
					duration := ""
					if record.DurationMs != nil {
						duration = (time.Duration(*record.DurationMs) * time.Millisecond).Round(time.Second).String()
					}
					return []string{
						record.ID,
						label,
						output.TintStatus(record.Status),
						record.CreatedAt.Format(time.RFC3339),
						duration,
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 0, "Number of builds to show (server default 20)")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.EnumVarP(command, command.Flags(), &opts.status, "status", "", "", "Filter by status", "running", "success", "failed")
	cmdutil.EnumVarP(command, command.Flags(), &opts.provider, "provider", "", "", "Filter by provider", "local", "depot")
	command.AddCommand(newBuildsGetCmd())
	return command
}

func newBuildsGetCmd() *cobra.Command {
	command := &cobra.Command{
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[image.BuildRecord]{
				Method: http.MethodGet, Path: path, Body: nil, FailureMessage: "failed to get image build",
				Render: func(data image.BuildRecord) error {
					record := data

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
					duration := ""
					if record.DurationMs != nil {
						duration = (time.Duration(*record.DurationMs) * time.Millisecond).Round(time.Second).String()
					}
					output.KeyValue("Duration", duration)
					if record.Digest != nil && *record.Digest != "" {
						output.KeyValue("Digest", *record.Digest)
					}
					if record.ErrorMessage != nil && *record.ErrorMessage != "" {
						output.KeyValue("Error", *record.ErrorMessage)
					}

					if record.Output != nil && *record.Output != "" {
						output.Header("Output")
						_, _ = fmt.Fprintln(output.Stdout(), *record.Output)
						if record.OutputTruncated {
							output.Warning("Output was truncated")
						}
					}

					return nil
				},
			})
		},
	}
	cmdutil.SetResource(buildRef, command)
	return command
}

func truncateCell(value string, limit int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if limit > 3 && len(value) > limit {
		return value[:limit-3] + "..."
	}
	return value
}

var buildRef = cmdutil.ResourceRef[image.BuildRecord, image.BuildRecord]{
	EnvironmentScoped: true,
	Singular:          "build", Plural: "builds",
	IDHint: "the identifier", ListCmd: "arcane-cli images builds",
	GetPath: types.ImageBuild, ListPath: types.ImageBuilds,
	Matches:      func(item image.BuildRecord, lower, original string) bool { return strings.EqualFold(item.ID, original) },
	Label:        func(item image.BuildRecord) string { return item.ID },
	Promote:      func(item image.BuildRecord) *image.BuildRecord { return &item },
	IdentifierOf: func(item image.BuildRecord) string { return item.ID },
}

func readBuildStream(cmd *cobra.Command, reader io.Reader) error {
	jsonMode := cmdutil.JSONOutputEnabled(cmd)
	frames := make([]jsontext.Value, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	done := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		var frame struct {
			Type  string  `json:"type"`
			Done  bool    `json:"done"`
			Error string  `json:"error"`
			Log   *string `json:"log"`
		}
		framed := strings.HasPrefix(trimmed, "{") && json.Unmarshal([]byte(trimmed), &frame) == nil
		if jsonMode && trimmed != "" {
			// Plain build log lines become JSON strings so stdout stays one array.
			value, _ := json.Marshal(line)
			frames = append(frames, jsontext.Value(kit.Ternary(framed, []byte(trimmed), value)))
		}
		if framed {
			if frame.Error != "" {
				return fmt.Errorf("build error: %s", frame.Error)
			}
			if frame.Done {
				done = true
				continue
			}
			if frame.Type != "" {
				continue
			}
			if frame.Log != nil {
				line = *frame.Log
			}
		}
		if !jsonMode {
			_, _ = fmt.Fprintln(output.Stdout(), line)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("failed to read build stream: %w", scanErr)
	}
	if !done {
		return errors.New("build stream ended without completion frame")
	}
	if jsonMode {
		return cmdutil.PrintJSON(frames)
	}
	output.Success("Image built successfully")
	return nil
}
