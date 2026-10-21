package templates

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for template operations
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Aliases: []string{"template", "tpl"},
		Short:   "Manage Docker Compose templates",
	}
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDefaultCmd())
	cmd.AddCommand(newContentCmd())
	cmd.AddCommand(newRegistriesCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newDownloadCmd())
	cmd.AddCommand(newFetchCmd())
	return cmd
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit           int
		start           int
		templateListAll bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List local templates",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			headers := []string{"NAME", "CUSTOM", "REMOTE", "DESCRIPTION"}

			if opts.templateListAll {
				return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.Template]{
					Method:         http.MethodGet,
					Path:           clitypes.TemplatesAll(),
					FailureMessage: "failed to list templates",
					Render: func(data []types.Template) error {
						rows := make([][]string, len(data))
						for i, tpl := range data {
							rows[i] = []string{
								tpl.Name,
								kit.Ternary(tpl.IsCustom, "yes", "no"),
								kit.Ternary(tpl.IsRemote, "yes", "no"),
								tpl.Description,
							}
						}
						if tableErr := output.Table(headers, rows); tableErr != nil {
							return tableErr
						}
						output.Showing(len(data), int64(len(data)), "templates")
						return nil
					},
				})
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Template]{
				Resource: "templates",
				Endpoint: clitypes.Templates(),
				Params:   cmdutil.ListParams{Resource: "templates", Limit: opts.limit, FallbackDefault: 20, Start: opts.start},
				Headers:  headers,
				Row: func(tpl types.Template) []string {
					return []string{
						tpl.Name,
						kit.Ternary(tpl.IsCustom, "yes", "no"),
						kit.Ternary(tpl.IsRemote, "yes", "no"),
						tpl.Description,
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of templates to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, "Offset for pagination")
	cmd.Flags().BoolVarP(&opts.templateListAll, "all", "a", false, "List all templates (including remote)")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if conflictErr := cmdutil.ExclusiveFlags(cmd, "all", "limit"); conflictErr != nil {
			return conflictErr
		}
		return cmdutil.ExclusiveFlags(cmd, "all", "start")
	}
	return cmd
}

func newDefaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "default",
		Short:        "Get default templates",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.DefaultTemplatesResponse]{
				Method:         http.MethodGet,
				Path:           clitypes.TemplatesDefault(),
				FailureMessage: "failed to get default templates",
				Render: func(data types.DefaultTemplatesResponse) error {
					output.Header("Default Templates")
					output.KeyValue("Compose Template", fmt.Sprintf("%d bytes", len(data.ComposeTemplate)))
					output.KeyValue("Env Template", fmt.Sprintf("%d bytes", len(data.EnvTemplate)))
					return nil
				},
			})
		},
	}
	cmd.AddCommand(newDefaultsSaveCmd())
	return cmd
}

func newContentCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "content <template-id>",
		Short:        "Get template content",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.TemplateContent]{
				Method:         http.MethodGet,
				Path:           clitypes.TemplateContent(args[0]),
				FailureMessage: "failed to get template content",
				Render: func(data types.TemplateContent) error {
					output.Header("Template Content")
					output.KeyValue("Name", data.Template.Name)
					output.KeyValue("Description", data.Template.Description)
					output.KeyValue("Services", strconv.Itoa(len(data.Services)))
					output.KeyValue("Env Variables", strconv.Itoa(len(data.EnvVariables)))
					_, _ = fmt.Fprintln(output.Stdout(), "\n--- Compose Content ---")
					_, _ = fmt.Fprintln(output.Stdout(), data.Content)
					if data.EnvContent != "" {
						_, _ = fmt.Fprintln(output.Stdout(), "\n--- Environment Content ---")
						_, _ = fmt.Fprintln(output.Stdout(), data.EnvContent)
					}
					return nil
				},
			})
		},
	}
}

func newRegistriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "registries",
		Aliases:      []string{"reg"},
		Short:        "List template registries",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.TemplateRegistry]{
				Method:         http.MethodGet,
				Path:           clitypes.TemplatesRegistries(),
				FailureMessage: "failed to list registries",
				Render: func(data []types.TemplateRegistry) error {
					headers := []string{"ID", "NAME", "URL", "ENABLED"}
					rows := make([][]string, len(data))
					for i, reg := range data {
						enabled := kit.Ternary(reg.Enabled, "yes", "no")
						rows[i] = []string{
							reg.ID,
							reg.Name,
							reg.URL,
							enabled,
						}
					}

					if tableErr := output.Table(headers, rows); tableErr != nil {
						return tableErr
					}
					_, _ = fmt.Fprintf(output.Stdout(), "\nTotal: %d registries\n", len(data))
					return nil
				},
			})
		},
	}
	cmd.AddCommand(newRegistriesUpdateCmd())
	cmd.AddCommand(newDeleteRegistryCmd())
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <template-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete template",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			label := "template " + args[0]
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete %s?", label),
				Path:           clitypes.Template(args[0]),
				Body:           nil,
				FailureMessage: "failed to delete " + label,
				SuccessMessage: fmt.Sprintf("Deleted %s successfully", label),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}

func newDeleteRegistryCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <registry-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete template registry",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			label := "template registry " + args[0]
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete %s?", label),
				Path:           clitypes.TemplateRegistry(args[0]),
				Body:           nil,
				FailureMessage: "failed to delete " + label,
				SuccessMessage: fmt.Sprintf("Deleted %s successfully", label),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <template>",
		Short:        "Get a template by ID or name",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := templateRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(resolved)
			}

			tpl := *resolved
			output.Header("Template")
			output.KeyValue("ID", tpl.ID)
			output.KeyValue("Name", tpl.Name)
			output.KeyValue("Description", tpl.Description)
			custom := kit.Ternary(tpl.IsCustom, "yes", "no")
			remote := kit.Ternary(tpl.IsRemote, "yes", "no")
			output.KeyValue("Custom", custom)
			output.KeyValue("Remote", remote)
			return nil
		},
	}
}

var templateRef = cmdutil.ResourceRef[types.Template, types.Template]{
	Singular:         "template",
	Plural:           "templates",
	IDHint:           "the template ID",
	ListCmd:          "arcane templates list",
	GetPath:          func(_, identifier string) string { return clitypes.Template(identifier) },
	SearchCandidates: searchTemplateCandidates,
	SelectCandidate:  selectTemplateCandidate,
	Promote:          func(match types.Template) *types.Template { return &match },
}

func searchTemplateCandidates(ctx context.Context, c *client.Client, identifier string) ([]types.Template, error) {
	result, err := c.GetJSON[[]types.Template](ctx, clitypes.TemplatesAll())
	if err != nil {
		return nil, fmt.Errorf("failed to search templates: %w", err)
	}

	identifierLower := strings.ToLower(identifier)
	exactNameMatches := make([]types.Template, 0)
	partialMatches := make([]types.Template, 0)
	for _, candidate := range result.Data {
		if strings.EqualFold(candidate.ID, identifier) {
			return []types.Template{candidate}, nil
		}
		if strings.EqualFold(candidate.Name, identifier) {
			exactNameMatches = append(exactNameMatches, candidate)
			continue
		}
		if strings.Contains(strings.ToLower(candidate.Name), identifierLower) ||
			strings.HasPrefix(strings.ToLower(candidate.ID), identifierLower) {
			partialMatches = append(partialMatches, candidate)
		}
	}

	if len(exactNameMatches) > 0 {
		return exactNameMatches, nil
	}
	if len(partialMatches) == 1 {
		return partialMatches, nil
	}
	candidates := result.Data
	if len(partialMatches) > 0 {
		candidates = partialMatches
	}
	ranked := rankFuzzyTemplateMatches(identifier, candidates)
	if len(ranked) == 0 {
		return nil, nil
	}
	best := ranked[0].score
	if best <= 110 && (len(ranked) == 1 || ranked[1].score-best >= 8) {
		return []types.Template{ranked[0].template}, nil
	}
	limit := min(cmdutil.MaxPromptOptions, len(ranked))
	selected := make([]types.Template, 0, limit)
	for i := range limit {
		selected = append(selected, ranked[i].template)
	}
	return selected, nil
}

func selectTemplateCandidate(matches []types.Template, identifier string, allowPrompt bool) (*types.Template, error) {
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if !allowPrompt || len(matches) > cmdutil.MaxPromptOptions {
		const previewLimit = 5
		limit := min(len(matches), previewLimit)
		parts := make([]string, 0, limit+1)
		for i := range limit {
			parts = append(parts, fmt.Sprintf("%s (%s)", matches[i].Name, matches[i].ID))
		}
		if len(matches) > limit {
			parts = append(parts, fmt.Sprintf("and %d more", len(matches)-limit))
		}
		return nil, fmt.Errorf("ambiguous template %q: %s", identifier, strings.Join(parts, ", "))
	}

	ordered := slices.Clone(matches)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(ordered[i].Name))
		right := strings.ToLower(strings.TrimSpace(ordered[j].Name))
		if left == right {
			return ordered[i].ID < ordered[j].ID
		}
		return left < right
	})

	options := make([]string, len(ordered))
	for i, candidate := range ordered {
		source := kit.Ternary(candidate.IsRemote, "remote", "local")
		custom := kit.Ternary(candidate.IsCustom, "custom", "builtin")
		options[i] = fmt.Sprintf("%s (id: %s, %s, %s)", candidate.Name, candidate.ID, source, custom)
	}
	choice, err := prompt.Select("template", options)
	if err != nil {
		return nil, err
	}
	return &ordered[choice], nil
}

type rankedTemplateMatch struct {
	template types.Template
	score    int
}

func rankFuzzyTemplateMatches(query string, candidates []types.Template) []rankedTemplateMatch {
	normalizedQuery := normalizeSearchToken(query)
	if normalizedQuery == "" {
		return nil
	}

	ranked := make([]rankedTemplateMatch, 0, len(candidates))
	for _, candidate := range candidates {
		nameScore, nameMatches := fuzzyScore(normalizedQuery, normalizeSearchToken(candidate.Name))
		idScore, idMatches := fuzzyScore(normalizedQuery, normalizeSearchToken(candidate.ID))
		score := 0
		matched := false
		switch {
		case nameMatches && idMatches:
			score = min(nameScore, idScore+10)
			matched = true
		case nameMatches:
			score = nameScore
			matched = true
		case idMatches:
			score = idScore + 10
			matched = true
		}
		if matched {
			ranked = append(ranked, rankedTemplateMatch{template: candidate, score: score})
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score < ranked[j].score
		}
		if ranked[i].template.Name != ranked[j].template.Name {
			return ranked[i].template.Name < ranked[j].template.Name
		}
		return ranked[i].template.ID < ranked[j].template.ID
	})
	return ranked
}

func normalizeSearchToken(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func fuzzyScore(query, target string) (int, bool) {
	if query == "" || target == "" {
		return 0, false
	}
	if query == target {
		return 0, true
	}
	if strings.Contains(target, query) {
		return 10 + absInt(len(target)-len(query)), true
	}
	if gap, ok := subsequenceGapPenalty(query, target); ok {
		return 40 + gap, true
	}

	distance := levenshteinDistance(query, target)
	if distance <= max(2, len(query)/3) {
		return 80 + (distance * 8) + absInt(len(target)-len(query)), true
	}
	return 0, false
}

func subsequenceGapPenalty(query, target string) (int, bool) {
	queryRunes := []rune(query)
	targetRunes := []rune(target)
	if len(queryRunes) == 0 {
		return 0, false
	}

	queryIndex := 0
	start := -1
	for index, r := range targetRunes {
		if r != queryRunes[queryIndex] {
			continue
		}
		if start == -1 {
			start = index
		}
		queryIndex++
		if queryIndex == len(queryRunes) {
			return index - start + 1 - len(queryRunes) + absInt(len(targetRunes)-len(queryRunes)), true
		}
	}
	return 0, false
}

func levenshteinDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}

	previous := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex := 1; leftIndex <= len(leftRunes); leftIndex++ {
		current := make([]int, len(rightRunes)+1)
		current[0] = leftIndex
		for rightIndex := 1; rightIndex <= len(rightRunes); rightIndex++ {
			cost := kit.Ternary(leftRunes[leftIndex-1] != rightRunes[rightIndex-1], 1, 0)
			current[rightIndex] = min(
				current[rightIndex-1]+1,
				previous[rightIndex]+1,
				previous[rightIndex-1]+cost,
			)
		}
		previous = current
	}
	return previous[len(rightRunes)]
}

func absInt(value int) int {
	return kit.Ternary(value < 0, -value, value)
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name        string
		file        string
		description string
		envFile     string
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new template",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(opts.file)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.file, err)
			}

			var envContent string
			if opts.envFile != "" {
				envData, readFileErr := os.ReadFile(opts.envFile)
				if readFileErr != nil {
					return fmt.Errorf("failed to read env file %s: %w", opts.envFile, readFileErr)
				}
				envContent = string(envData)
			}

			req := types.Template{
				BaseTemplate: types.BaseTemplate{
					Name:        opts.name,
					Description: opts.description,
				},
				Content:    string(content),
				EnvContent: &envContent,
			}

			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Template]{
				Method:         http.MethodPost,
				Path:           clitypes.Templates(),
				Body:           req,
				FailureMessage: "failed to create template",
				Render: func(data types.Template) error {
					output.Success("Template created successfully")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "Template name")
	cmd.Flags().StringVar(&opts.file, "file", "", "Path to Docker Compose file")
	cmd.Flags().StringVar(&opts.description, "description", "", "Template description")
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Path to environment file")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name        string
		file        string
		description string
		envFile     string
	}
	cmd := &cobra.Command{
		Use:          "update <template-id>",
		Short:        "Update a template",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			req := map[string]any{}
			if cmd.Flags().Changed("name") {
				req["name"] = opts.name
			}
			if cmd.Flags().Changed("description") {
				req["description"] = opts.description
			}
			if opts.file != "" {
				contentData, readFileErr := os.ReadFile(opts.file)
				if readFileErr != nil {
					return fmt.Errorf("failed to read file %s: %w", opts.file, readFileErr)
				}
				req["content"] = string(contentData)
			}
			if opts.envFile != "" {
				envData, readEnvFileErr := os.ReadFile(opts.envFile)
				if readEnvFileErr != nil {
					return fmt.Errorf("failed to read env file %s: %w", opts.envFile, readEnvFileErr)
				}
				req["envContent"] = string(envData)
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Template]{
				Method:         http.MethodPut,
				Path:           clitypes.Template(args[0]),
				Body:           req,
				FailureMessage: "failed to update template",
				Render: func(data types.Template) error {
					output.Success("Template updated successfully")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "Template name")
	cmd.Flags().StringVar(&opts.file, "file", "", "Path to Docker Compose file")
	cmd.Flags().StringVar(&opts.description, "description", "", "Template description")
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Path to environment file")
	return cmd
}

func newDownloadCmd() *cobra.Command {
	var opts struct{ outputFile string }
	cmd := &cobra.Command{
		Use:          "download <template-id>",
		Short:        "Download template compose file",
		Example:      "  arcane templates download <template-id> --output-file compose.yaml\n  arcane templates download <template-id> -o compose.yaml",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}
			result, err := c.PostJSON[jsontext.Value](cmd.Context(), clitypes.TemplateDownload(args[0]), nil)
			if err != nil {
				return fmt.Errorf("failed to download template: %w", err)
			}
			jsonMode := cmdutil.JSONOutputEnabled(cmd)
			if jsonMode && opts.outputFile == "" {
				if len(result.Data) == 0 {
					return cmdutil.PrintJSON(nil)
				}
				return cmdutil.PrintRawJSON(result.Data)
			}
			var data types.Template
			if decodeErr := json.Unmarshal(result.Data, &data); decodeErr != nil {
				return fmt.Errorf("failed to download template: failed to decode response: %w", decodeErr)
			}
			if opts.outputFile != "" {
				dir := filepath.Dir(opts.outputFile)
				if mkdirErr := os.MkdirAll(dir, 0o755); mkdirErr != nil {
					return fmt.Errorf("failed to create directory %s: %w", dir, mkdirErr)
				}
				if writeErr := os.WriteFile(opts.outputFile, []byte(data.Content), 0o600); writeErr != nil {
					return fmt.Errorf("failed to write file %s: %w", opts.outputFile, writeErr)
				}
				if jsonMode {
					return cmdutil.PrintJSON(struct {
						Path  string `json:"path"`
						Bytes int    `json:"bytes"`
					}{Path: opts.outputFile, Bytes: len(data.Content)})
				}
				output.Success("Template downloaded to %s", opts.outputFile)
				return nil
			}
			_, writeErr := fmt.Fprint(output.Stdout(), data.Content)
			return writeErr
		},
	}
	cmd.Flags().StringVarP(&opts.outputFile, "output-file", "o", "", "Output file path")
	return cmd
}

func newDefaultsSaveCmd() *cobra.Command {
	var opts struct {
		saveFile string
		envFile  string
	}
	cmd := &cobra.Command{
		Use:          "save",
		Short:        "Save default templates",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(opts.saveFile)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.saveFile, err)
			}

			var envContent string
			if opts.envFile != "" {
				envData, readFileErr := os.ReadFile(opts.envFile)
				if readFileErr != nil {
					return fmt.Errorf("failed to read env file %s: %w", opts.envFile, readFileErr)
				}
				envContent = string(envData)
			}

			req := types.SaveDefaultTemplatesRequest{
				ComposeContent: string(content),
				EnvContent:     envContent,
			}

			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.TemplatesDefault(),
				Body:           req,
				FailureMessage: "failed to save default templates",
				Render: func(types.MessageResponse) error {
					output.Success("%s", "Default templates saved successfully")
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.saveFile, "file", "", "Path to compose content file")
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Path to environment content file")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newRegistriesUpdateCmd() *cobra.Command {
	var opts struct {
		name    string
		url     string
		desc    string
		enabled bool
	}
	cmd := &cobra.Command{
		Use:          "update <registry-id>",
		Short:        "Update a template registry",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			req := map[string]any{}
			for _, field := range []struct {
				changed bool
				key     string
				value   any
			}{
				{changed: cmd.Flags().Changed("name"), key: "name", value: opts.name},
				{changed: cmd.Flags().Changed("url"), key: "url", value: opts.url},
				{changed: cmd.Flags().Changed("description"), key: "description", value: opts.desc},
				{changed: cmd.Flags().Changed("enabled"), key: "enabled", value: opts.enabled},
			} {
				if field.changed {
					req[field.key] = field.value
				}
			}

			// The handler answers with a plain message, not the updated registry.
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPut,
				Path:           clitypes.TemplateRegistry(args[0]),
				Body:           req,
				FailureMessage: "failed to update registry",
				Render: func(types.MessageResponse) error {
					output.Success("Template registry updated successfully")
					output.KeyValue("ID", args[0])
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "Registry name")
	cmd.Flags().StringVar(&opts.url, "url", "", "Registry URL")
	cmd.Flags().StringVar(&opts.desc, "description", "", "Registry description")
	cmd.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable registry")
	return cmd
}

func newFetchCmd() *cobra.Command {
	var opts struct {
		url string
	}
	cmd := &cobra.Command{
		Use:          "fetch",
		Short:        "Fetch remote template registries",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			path := cmdutil.AppendQuery(clitypes.TemplateFetch(), url.Values{"url": []string{opts.url}})
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.TemplateRemoteRegistry]{
				Method:         http.MethodGet,
				Path:           path,
				FailureMessage: "failed to fetch templates",
				Render: func(data types.TemplateRemoteRegistry) error {
					output.Success("Remote templates fetched successfully")
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.url, "url", "", "Registry URL to fetch templates from (required)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}
