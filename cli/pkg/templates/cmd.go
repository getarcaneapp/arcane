package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/env"
	"github.com/getarcaneapp/arcane/types/template"
	"github.com/spf13/cobra"
)

const maxTemplatePromptOptions = 20

var (
	limitFlag       int
	startFlag       int
	templateListAll bool
	forceFlag       bool
	jsonOutput      bool

	templateUpdateName        string
	templateUpdateFile        string
	templateUpdateDescription string
	templateUpdateEnvFile     string
	templateCreateName        string
	templateCreateFile        string
	templateCreateDescription string
	templateCreateEnvFile     string
	templateDownloadOutput    string
	templateDefaultsSaveFile  string
	templateDefaultsEnvFile   string
	templateVarsUpdateFile    string
	templateRegUpdateName     string
	templateRegUpdateURL      string
	templateRegUpdateDesc     string
	templateRegUpdateEnabled  bool
	templateRegUpdateDisabled bool
)

// TemplatesCmd is the parent command for template operations
var TemplatesCmd = &cobra.Command{
	Use:     "templates",
	Aliases: []string{"template", "tpl"},
	Short:   "Manage Docker Compose templates",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List local templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		var (
			templates   []template.Template
			totalItems  int64
			jsonPayload any
			path        string
		)

		if templateListAll {
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("start") {
				return fmt.Errorf("--all cannot be combined with explicit pagination flags")
			}
			path = types.Endpoints.TemplatesAll()
			resp, err := c.Get(cmd.Context(), path)
			if err != nil {
				return fmt.Errorf("failed to list templates: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result base.ApiResponse[[]template.Template]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			templates = result.Data
			totalItems = int64(len(result.Data))
			jsonPayload = result.Data
		} else {
			path = types.Endpoints.Templates()
			path, err = cmdutil.ApplyPaginationParams(cmd, path, "templates", "limit", limitFlag, 20, "start", startFlag)
			if err != nil {
				return fmt.Errorf("failed to build pagination query: %w", err)
			}

			resp, err := c.Get(cmd.Context(), path)
			if err != nil {
				return fmt.Errorf("failed to list templates: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result base.Paginated[template.Template]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			templates = result.Data
			totalItems = result.Pagination.TotalItems
			jsonPayload = result
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(jsonPayload, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"NAME", "CUSTOM", "REMOTE", "DESCRIPTION"}
		rows := make([][]string, len(templates))
		for i, tpl := range templates {
			custom := "no"
			if tpl.IsCustom {
				custom = "yes"
			}
			remote := "no"
			if tpl.IsRemote {
				remote = "yes"
			}
			rows[i] = []string{
				tpl.Name,
				custom,
				remote,
				tpl.Description,
			}
		}

		output.Table(headers, rows)
		output.Showing(len(templates), totalItems, "templates")
		return nil
	},
}

var defaultCmd = &cobra.Command{
	Use:          "default",
	Short:        "Get default templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesDefault())
		if err != nil {
			return fmt.Errorf("failed to get default templates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.DefaultTemplatesResponse]
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

		output.Header("Default Templates")
		output.KeyValue("Compose Template", fmt.Sprintf("%d bytes", len(result.Data.ComposeTemplate)))
		output.KeyValue("Env Template", fmt.Sprintf("%d bytes", len(result.Data.EnvTemplate)))
		return nil
	},
}

var contentCmd = &cobra.Command{
	Use:          "content <template-id>",
	Short:        "Get template content",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplateContent(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get template content: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.TemplateContent]
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

		output.Header("Template Content")
		output.KeyValue("Name", result.Data.Template.Name)
		output.KeyValue("Description", result.Data.Template.Description)
		output.KeyValue("Services", fmt.Sprintf("%d", len(result.Data.Services)))
		output.KeyValue("Env Variables", fmt.Sprintf("%d", len(result.Data.EnvVariables)))
		fmt.Println("\n--- Compose Content ---")
		fmt.Println(result.Data.Content)
		if result.Data.EnvContent != "" {
			fmt.Println("\n--- Environment Content ---")
			fmt.Println(result.Data.EnvContent)
		}
		return nil
	},
}

var registriesCmd = &cobra.Command{
	Use:          "registries",
	Aliases:      []string{"reg"},
	Short:        "List template registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesRegistries())
		if err != nil {
			return fmt.Errorf("failed to list registries: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]template.TemplateRegistry]
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

		headers := []string{"ID", "NAME", "URL", "ENABLED"}
		rows := make([][]string, len(result.Data))
		for i, reg := range result.Data {
			enabled := "no"
			if reg.Enabled {
				enabled = "yes"
			}
			rows[i] = []string{
				reg.ID,
				reg.Name,
				reg.URL,
				enabled,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d registries\n", len(result.Data))
		return nil
	},
}

var variablesCmd = &cobra.Command{
	Use:          "variables",
	Aliases:      []string{"vars"},
	Short:        "List template variables",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesVariables())
		if err != nil {
			return fmt.Errorf("failed to list variables: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]env.Variable]
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

		headers := []string{"KEY", "VALUE"}
		rows := make([][]string, len(result.Data))
		for i, v := range result.Data {
			rows[i] = []string{
				v.Key,
				v.Value,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d variables\n", len(result.Data))
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <template-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete template",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete template %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Template(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete template: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete template: %w", err)
		}

		output.Success("Template deleted successfully")
		return nil
	},
}

var deleteRegistryCmd = &cobra.Command{
	Use:          "delete-registry <registry-id>",
	Short:        "Delete template registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete template registry %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.TemplateRegistry(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete registry: %w", err)
		}

		output.Success("Template registry deleted successfully")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <template>",
	Short:        "Get a template by ID or name",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, err := resolveTemplate(cmd.Context(), c, args[0])
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

		tpl := *resolved
		output.Header("Template")
		output.KeyValue("ID", tpl.ID)
		output.KeyValue("Name", tpl.Name)
		output.KeyValue("Description", tpl.Description)
		custom := "no"
		if tpl.IsCustom {
			custom = "yes"
		}
		remote := "no"
		if tpl.IsRemote {
			remote = "yes"
		}
		output.KeyValue("Custom", custom)
		output.KeyValue("Remote", remote)
		return nil
	},
}

// resolveTemplate attempts to resolve a template by ID or name.
// It first tries direct GET by ID and falls back to searching all templates by name/ID.
func resolveTemplate(ctx context.Context, c *client.Client, identifier string) (*template.Template, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, fmt.Errorf("template identifier is required")
	}

	resp, err := c.Get(ctx, types.Endpoints.Template(trimmed))
	if err != nil {
		return nil, fmt.Errorf("failed to get template: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[template.Template]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		return &result.Data, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("failed to get template: request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	listResp, err := c.Get(ctx, types.Endpoints.TemplatesAll())
	if err != nil {
		return nil, fmt.Errorf("failed to search templates: %w", err)
	}
	defer func() { _ = listResp.Body.Close() }()

	if listResp.StatusCode < http.StatusOK || listResp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(listResp.Body, 4096))
		return nil, fmt.Errorf("failed to search templates: request failed with status %d: %s", listResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var listResult base.ApiResponse[[]template.Template]
	if err := json.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		return nil, fmt.Errorf("failed to parse template list: %w", err)
	}

	lowerIdentifier := strings.ToLower(trimmed)
	exactNameMatches := make([]template.Template, 0)
	partialMatches := make([]template.Template, 0)

	for _, tpl := range listResult.Data {
		if strings.EqualFold(tpl.ID, trimmed) {
			return &tpl, nil
		}
		if strings.EqualFold(tpl.Name, trimmed) {
			exactNameMatches = append(exactNameMatches, tpl)
			continue
		}

		if strings.Contains(strings.ToLower(tpl.Name), lowerIdentifier) || strings.HasPrefix(strings.ToLower(tpl.ID), lowerIdentifier) {
			partialMatches = append(partialMatches, tpl)
		}
	}

	if len(exactNameMatches) == 1 {
		return &exactNameMatches[0], nil
	}
	if len(exactNameMatches) > 1 {
		return selectTemplateMatch(trimmed, exactNameMatches)
	}

	if len(partialMatches) == 1 {
		return &partialMatches[0], nil
	}
	if len(partialMatches) > 1 {
		ranked := rankFuzzyTemplateMatches(trimmed, partialMatches)
		if isConfidentBestFuzzyMatch(ranked) {
			return &ranked[0].template, nil
		}
		return selectTemplateMatch(trimmed, topTemplatesFromRankedMatches(ranked, maxTemplatePromptOptions))
	}

	ranked := rankFuzzyTemplateMatches(trimmed, listResult.Data)
	if isConfidentBestFuzzyMatch(ranked) {
		return &ranked[0].template, nil
	}
	if len(ranked) > 0 {
		return selectTemplateMatch(trimmed, topTemplatesFromRankedMatches(ranked, maxTemplatePromptOptions))
	}

	return nil, fmt.Errorf("template %q not found", trimmed)
}

func selectTemplateMatch(identifier string, matches []template.Template) (*template.Template, error) {
	if len(matches) == 0 {
		return nil, fmt.Errorf("template %q not found", identifier)
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}

	if !prompt.IsInteractive() || len(matches) > maxTemplatePromptOptions {
		return nil, fmt.Errorf("ambiguous template %q: %s", identifier, formatTemplateCandidates(matches))
	}

	ordered := make([]template.Template, len(matches))
	copy(ordered, matches)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(ordered[i].Name))
		right := strings.ToLower(strings.TrimSpace(ordered[j].Name))
		if left == right {
			return ordered[i].ID < ordered[j].ID
		}
		return left < right
	})

	options := make([]string, len(ordered))
	for i, tpl := range ordered {
		source := "local"
		if tpl.IsRemote {
			source = "remote"
		}
		custom := "builtin"
		if tpl.IsCustom {
			custom = "custom"
		}
		options[i] = fmt.Sprintf("%s (id: %s, %s, %s)", tpl.Name, tpl.ID, source, custom)
	}

	choice, err := prompt.Select("template", options)
	if err != nil {
		return nil, err
	}

	return new(ordered[choice]), nil
}

type rankedTemplateMatch struct {
	template template.Template
	score    int
}

func rankFuzzyTemplateMatches(query string, candidates []template.Template) []rankedTemplateMatch {
	normalizedQuery := normalizeSearchToken(query)
	if normalizedQuery == "" {
		return nil
	}

	ranked := make([]rankedTemplateMatch, 0, len(candidates))
	for _, candidate := range candidates {
		nameScore, nameOk := fuzzyScore(normalizedQuery, normalizeSearchToken(candidate.Name))
		idScore, idOk := fuzzyScore(normalizedQuery, normalizeSearchToken(candidate.ID))

		score := 0
		matched := false
		switch {
		case nameOk && idOk:
			if nameScore <= idScore {
				score = nameScore
			} else {
				score = idScore + 10
			}
			matched = true
		case nameOk:
			score = nameScore
			matched = true
		case idOk:
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
	maxDistance := maxInt(2, len(query)/3)
	if distance <= maxDistance {
		return 80 + (distance * 8) + absInt(len(target)-len(query)), true
	}

	return 0, false
}

func subsequenceGapPenalty(query, target string) (int, bool) {
	q := []rune(query)
	t := []rune(target)
	if len(q) == 0 {
		return 0, false
	}

	qIdx := 0
	start := -1
	for idx, r := range t {
		if r != q[qIdx] {
			continue
		}
		if start == -1 {
			start = idx
		}
		qIdx++
		if qIdx == len(q) {
			span := (idx - start) + 1
			gaps := span - len(q)
			return gaps + absInt(len(t)-len(q)), true
		}
	}

	return 0, false
}

func levenshteinDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)

	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	for j := 0; j <= len(rb); j++ {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		curr := make([]int, len(rb)+1)
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			curr[j] = minInt(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev = curr
	}

	return prev[len(rb)]
}

func isConfidentBestFuzzyMatch(matches []rankedTemplateMatch) bool {
	if len(matches) == 0 {
		return false
	}

	best := matches[0].score
	if len(matches) == 1 {
		return best <= 110
	}

	second := matches[1].score
	return best <= 110 && (second-best) >= 8
}

func topTemplatesFromRankedMatches(matches []rankedTemplateMatch, limit int) []template.Template {
	if limit <= 0 || len(matches) == 0 {
		return nil
	}
	if len(matches) < limit {
		limit = len(matches)
	}

	result := make([]template.Template, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, matches[i].template)
	}
	return result
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
	}
	return min
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func formatTemplateCandidates(matches []template.Template) string {
	const previewLimit = 5

	if len(matches) == 0 {
		return "no matches"
	}

	limit := min(len(matches), previewLimit)

	parts := make([]string, 0, limit+1)
	for i := range limit {
		parts = append(parts, fmt.Sprintf("%s (%s)", matches[i].Name, matches[i].ID))
	}

	if len(matches) > limit {
		parts = append(parts, fmt.Sprintf("and %d more", len(matches)-limit))
	}

	return strings.Join(parts, ", ")
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a new template",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(templateCreateFile)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", templateCreateFile, err)
		}

		req := template.CreateRequest{
			Name:        templateCreateName,
			Description: templateCreateDescription,
			Content:     string(content),
		}

		if templateCreateEnvFile != "" {
			envContent, err := os.ReadFile(templateCreateEnvFile)
			if err != nil {
				return fmt.Errorf("failed to read env file %s: %w", templateCreateEnvFile, err)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.Templates(), req)
		if err != nil {
			return fmt.Errorf("failed to create template: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.Template]
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

		output.Success("Template created successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <template-id>",
	Short:        "Update a template",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		req := template.UpdateRequest{
			Name:        templateUpdateName,
			Description: templateUpdateDescription,
		}

		if templateUpdateFile != "" {
			content, err := os.ReadFile(templateUpdateFile)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", templateUpdateFile, err)
			}
			req.Content = string(content)
		}

		if templateUpdateEnvFile != "" {
			envContent, err := os.ReadFile(templateUpdateEnvFile)
			if err != nil {
				return fmt.Errorf("failed to read env file %s: %w", templateUpdateEnvFile, err)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.Template(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update template: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.Template]
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

		output.Success("Template updated successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		return nil
	},
}

var downloadCmd = &cobra.Command{
	Use:          "download <template-id>",
	Short:        "Download template compose file",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.TemplateDownload(args[0]), nil)
		if err != nil {
			return fmt.Errorf("failed to download template: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to download template: %w", err)
		}

		var result base.ApiResponse[template.TemplateContent]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if templateDownloadOutput != "" {
			dir := filepath.Dir(templateDownloadOutput)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
			if err := os.WriteFile(templateDownloadOutput, []byte(result.Data.Content), 0o600); err != nil {
				return fmt.Errorf("failed to write file %s: %w", templateDownloadOutput, err)
			}
			output.Success("Template downloaded to %s", templateDownloadOutput)
			return nil
		}

		fmt.Print(result.Data.Content)
		return nil
	},
}

var defaultsSaveCmd = &cobra.Command{
	Use:          "defaults-save",
	Short:        "Save default templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(templateDefaultsSaveFile)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", templateDefaultsSaveFile, err)
		}

		req := template.SaveDefaultTemplatesRequest{
			ComposeContent: string(content),
		}

		if templateDefaultsEnvFile != "" {
			envContent, err := os.ReadFile(templateDefaultsEnvFile)
			if err != nil {
				return fmt.Errorf("failed to read env file %s: %w", templateDefaultsEnvFile, err)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.TemplatesDefault(), req)
		if err != nil {
			return fmt.Errorf("failed to save default templates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to save default templates: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[template.DefaultTemplatesResponse]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Default templates saved successfully")
		return nil
	},
}

var variablesUpdateCmd = &cobra.Command{
	Use:          "variables-update",
	Aliases:      []string{"vars-update"},
	Short:        "Update template variables",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(templateVarsUpdateFile)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", templateVarsUpdateFile, err)
		}

		var variables []env.Variable
		if err := json.Unmarshal(content, &variables); err != nil {
			return fmt.Errorf("failed to parse variables JSON: %w", err)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.TemplatesVariables(), variables)
		if err != nil {
			return fmt.Errorf("failed to update variables: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update variables: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[[]env.Variable]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Template variables updated successfully")
		return nil
	},
}

var registriesUpdateCmd = &cobra.Command{
	Use:          "registries-update <registry-id>",
	Short:        "Update a template registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		req := template.UpdateRegistryRequest{
			Name:        templateRegUpdateName,
			URL:         templateRegUpdateURL,
			Description: templateRegUpdateDesc,
			Enabled:     templateRegUpdateEnabled && !templateRegUpdateDisabled,
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.TemplateRegistry(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.TemplateRegistry]
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

		output.Success("Template registry updated successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		return nil
	},
}

var fetchCmd = &cobra.Command{
	Use:          "fetch",
	Short:        "Fetch remote template registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplateFetch())
		if err != nil {
			return fmt.Errorf("failed to fetch templates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to fetch templates: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Remote templates fetched successfully")
		return nil
	},
}

func init() {
	TemplatesCmd.AddCommand(listCmd)
	TemplatesCmd.AddCommand(defaultCmd)
	TemplatesCmd.AddCommand(contentCmd)
	TemplatesCmd.AddCommand(registriesCmd)
	TemplatesCmd.AddCommand(variablesCmd)
	TemplatesCmd.AddCommand(deleteCmd)
	TemplatesCmd.AddCommand(deleteRegistryCmd)
	TemplatesCmd.AddCommand(getCmd)
	TemplatesCmd.AddCommand(createCmd)
	TemplatesCmd.AddCommand(updateCmd)
	TemplatesCmd.AddCommand(downloadCmd)
	TemplatesCmd.AddCommand(defaultsSaveCmd)
	TemplatesCmd.AddCommand(variablesUpdateCmd)
	TemplatesCmd.AddCommand(registriesUpdateCmd)
	TemplatesCmd.AddCommand(fetchCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of templates to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, "Offset for pagination")
	listCmd.Flags().BoolVarP(&templateListAll, "all", "a", false, "List all templates (including remote)")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	defaultCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	contentCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	registriesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	variablesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteRegistryCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteRegistryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// create command flags
	createCmd.Flags().StringVar(&templateCreateName, "name", "", "Template name")
	createCmd.Flags().StringVar(&templateCreateFile, "file", "", "Path to Docker Compose file")
	createCmd.Flags().StringVar(&templateCreateDescription, "description", "", "Template description")
	createCmd.Flags().StringVar(&templateCreateEnvFile, "env-file", "", "Path to environment file")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("file")

	// update command flags
	updateCmd.Flags().StringVar(&templateUpdateName, "name", "", "Template name")
	updateCmd.Flags().StringVar(&templateUpdateFile, "file", "", "Path to Docker Compose file")
	updateCmd.Flags().StringVar(&templateUpdateDescription, "description", "", "Template description")
	updateCmd.Flags().StringVar(&templateUpdateEnvFile, "env-file", "", "Path to environment file")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// download command flags
	downloadCmd.Flags().StringVarP(&templateDownloadOutput, "output", "o", "", "Output file path")
	downloadCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// defaults-save command flags
	defaultsSaveCmd.Flags().StringVar(&templateDefaultsSaveFile, "file", "", "Path to compose content file")
	defaultsSaveCmd.Flags().StringVar(&templateDefaultsEnvFile, "env-file", "", "Path to environment content file")
	defaultsSaveCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = defaultsSaveCmd.MarkFlagRequired("file")

	// variables-update command flags
	variablesUpdateCmd.Flags().StringVar(&templateVarsUpdateFile, "file", "", "Path to variables JSON file")
	variablesUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = variablesUpdateCmd.MarkFlagRequired("file")

	// registries-update command flags
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateName, "name", "", "Registry name")
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateURL, "url", "", "Registry URL")
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateDesc, "description", "", "Registry description")
	registriesUpdateCmd.Flags().BoolVar(&templateRegUpdateEnabled, "enabled", false, "Enable registry")
	registriesUpdateCmd.Flags().BoolVar(&templateRegUpdateDisabled, "disabled", false, "Disable registry")
	registriesUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// fetch command flags
	fetchCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
