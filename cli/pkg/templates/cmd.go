package templates

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/template"
	"github.com/spf13/cobra"
)

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
	templateFetchURL          string
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

		headers := []string{"NAME", "CUSTOM", "REMOTE", "DESCRIPTION"}
		row := func(tpl template.Template) []string {
			custom := "no"
			if tpl.IsCustom {
				custom = "yes"
			}
			remote := "no"
			if tpl.IsRemote {
				remote = "yes"
			}
			return []string{
				tpl.Name,
				custom,
				remote,
				tpl.Description,
			}
		}

		if templateListAll {
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("start") {
				return errors.New("--all cannot be combined with explicit pagination flags")
			}

			result, err := c.GetJSON[[]template.Template](cmd.Context(), types.TemplatesAll())
			if err != nil {
				return errors.WrapIf(err, "failed to list templates")
			}

			if jsonOutput {
				return cmdutil.PrintJSON(result.Data)
			}

			rows := make([][]string, len(result.Data))
			for i, tpl := range result.Data {
				rows[i] = row(tpl)
			}
			output.Table(headers, rows)
			output.Showing(len(result.Data), int64(len(result.Data)), "templates")
			return nil
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[template.Template]{
			Resource: "templates",
			Endpoint: types.Templates(),
			Params:   cmdutil.ListParams{Resource: "templates", Limit: limitFlag, FallbackDefault: 20, Start: startFlag},
			JSON:     jsonOutput,
			Headers:  headers,
			Row:      row,
		})
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

		result, err := c.GetJSON[template.DefaultTemplatesResponse](cmd.Context(), types.TemplatesDefault())
		if err != nil {
			return errors.WrapIf(err, "failed to get default templates")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

		result, err := c.GetJSON[template.TemplateContent](cmd.Context(), types.TemplateContent(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to get template content")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Template Content")
		output.KeyValue("Name", result.Data.Template.Name)
		output.KeyValue("Description", result.Data.Template.Description)
		output.KeyValue("Services", strconv.Itoa(len(result.Data.Services)))
		output.KeyValue("Env Variables", strconv.Itoa(len(result.Data.EnvVariables)))
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

		result, err := c.GetJSON[[]template.TemplateRegistry](cmd.Context(), types.TemplatesRegistries())
		if err != nil {
			return errors.WrapIf(err, "failed to list registries")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

func runTemplateDeleteInternal(cmd *cobra.Command, label, path string) error {
	if !forceFlag {
		confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete %s?", label))
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

	result, err := c.DeleteJSON[base.MessageResponse](cmd.Context(), path)
	if err != nil {
		return errors.WrapIff(err, "failed to delete %s", label)
	}
	if jsonOutput {
		return cmdutil.PrintJSON(result.Data)
	}

	output.Success("Deleted %s successfully", label)
	return nil
}

var deleteCmd = &cobra.Command{
	Use:          "delete <template-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete template",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTemplateDeleteInternal(cmd, "template "+args[0], types.Template(args[0]))
	},
}

var deleteRegistryCmd = &cobra.Command{
	Use:          "delete <registry-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete template registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTemplateDeleteInternal(cmd, "template registry "+args[0], types.TemplateRegistry(args[0]))
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

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, _, err := templateRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
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

var templateRef = cmdutil.ResourceRef[template.Template, template.Template]{
	Singular:         "template",
	Plural:           "templates",
	IDHint:           "the template ID",
	ListCmd:          "arcane templates list",
	GetPath:          func(_, identifier string) string { return types.Template(identifier) },
	SearchCandidates: searchTemplateCandidatesInternal,
	SelectCandidate:  selectTemplateCandidateInternal,
	Promote:          func(match template.Template) *template.Template { return &match },
}

func searchTemplateCandidatesInternal(ctx context.Context, c *client.Client, identifier string) ([]template.Template, error) {
	result, err := c.GetJSON[[]template.Template](ctx, types.TemplatesAll())
	if err != nil {
		return nil, errors.WrapIf(err, "failed to search templates")
	}

	identifierLower := strings.ToLower(identifier)
	exactNameMatches := make([]template.Template, 0)
	partialMatches := make([]template.Template, 0)
	for _, candidate := range result.Data {
		if strings.EqualFold(candidate.ID, identifier) {
			return []template.Template{candidate}, nil
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
	if len(partialMatches) > 1 {
		ranked := rankFuzzyTemplateMatchesInternal(identifier, partialMatches)
		if isConfidentBestFuzzyMatchInternal(ranked) {
			return []template.Template{ranked[0].template}, nil
		}
		return topTemplatesFromRankedMatchesInternal(ranked, cmdutil.MaxPromptOptions), nil
	}

	ranked := rankFuzzyTemplateMatchesInternal(identifier, result.Data)
	if isConfidentBestFuzzyMatchInternal(ranked) {
		return []template.Template{ranked[0].template}, nil
	}
	return topTemplatesFromRankedMatchesInternal(ranked, cmdutil.MaxPromptOptions), nil
}

func selectTemplateCandidateInternal(matches []template.Template, identifier string, allowPrompt bool) (*template.Template, error) {
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if !allowPrompt || len(matches) > cmdutil.MaxPromptOptions {
		return nil, errors.Errorf("ambiguous template %q: %s", identifier, formatTemplateCandidatesInternal(matches))
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
		options[i] = templateOptionLabelInternal(candidate)
	}
	choice, err := prompt.Select("template", options)
	if err != nil {
		return nil, err
	}
	return &ordered[choice], nil
}

func templateOptionLabelInternal(candidate template.Template) string {
	source := "local"
	if candidate.IsRemote {
		source = "remote"
	}
	custom := "builtin"
	if candidate.IsCustom {
		custom = "custom"
	}
	return fmt.Sprintf("%s (id: %s, %s, %s)", candidate.Name, candidate.ID, source, custom)
}

type rankedTemplateMatchInternal struct {
	template template.Template
	score    int
}

func rankFuzzyTemplateMatchesInternal(query string, candidates []template.Template) []rankedTemplateMatchInternal {
	normalizedQuery := normalizeSearchTokenInternal(query)
	if normalizedQuery == "" {
		return nil
	}

	ranked := make([]rankedTemplateMatchInternal, 0, len(candidates))
	for _, candidate := range candidates {
		nameScore, nameMatches := fuzzyScoreInternal(normalizedQuery, normalizeSearchTokenInternal(candidate.Name))
		idScore, idMatches := fuzzyScoreInternal(normalizedQuery, normalizeSearchTokenInternal(candidate.ID))
		score := 0
		matched := false
		switch {
		case nameMatches && idMatches:
			score = nameScore
			if idScore < nameScore {
				score = idScore + 10
			}
			matched = true
		case nameMatches:
			score = nameScore
			matched = true
		case idMatches:
			score = idScore + 10
			matched = true
		}
		if matched {
			ranked = append(ranked, rankedTemplateMatchInternal{template: candidate, score: score})
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

func normalizeSearchTokenInternal(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func fuzzyScoreInternal(query, target string) (int, bool) {
	if query == "" || target == "" {
		return 0, false
	}
	if query == target {
		return 0, true
	}
	if strings.Contains(target, query) {
		return 10 + absIntInternal(len(target)-len(query)), true
	}
	if gap, ok := subsequenceGapPenaltyInternal(query, target); ok {
		return 40 + gap, true
	}

	distance := levenshteinDistanceInternal(query, target)
	if distance <= max(2, len(query)/3) {
		return 80 + (distance * 8) + absIntInternal(len(target)-len(query)), true
	}
	return 0, false
}

func subsequenceGapPenaltyInternal(query, target string) (int, bool) {
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
			span := (index - start) + 1
			gaps := span - len(queryRunes)
			return gaps + absIntInternal(len(targetRunes)-len(queryRunes)), true
		}
	}
	return 0, false
}

func levenshteinDistanceInternal(left, right string) int {
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
			cost := 0
			if leftRunes[leftIndex-1] != rightRunes[rightIndex-1] {
				cost = 1
			}
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

func isConfidentBestFuzzyMatchInternal(matches []rankedTemplateMatchInternal) bool {
	if len(matches) == 0 {
		return false
	}
	best := matches[0].score
	if len(matches) == 1 {
		return best <= 110
	}
	return best <= 110 && matches[1].score-best >= 8
}

func topTemplatesFromRankedMatchesInternal(matches []rankedTemplateMatchInternal, limit int) []template.Template {
	if limit <= 0 || len(matches) == 0 {
		return nil
	}
	limit = min(limit, len(matches))
	result := make([]template.Template, 0, limit)
	for i := range limit {
		result = append(result, matches[i].template)
	}
	return result
}

func absIntInternal(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func formatTemplateCandidatesInternal(matches []template.Template) string {
	if len(matches) == 0 {
		return "no matches"
	}
	const previewLimit = 5
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
			return errors.WrapIff(err, "failed to read file %s", templateCreateFile)
		}

		req := template.CreateRequest{
			Name:        templateCreateName,
			Description: templateCreateDescription,
			Content:     string(content),
		}

		if templateCreateEnvFile != "" {
			envContent, err := os.ReadFile(templateCreateEnvFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read env file %s", templateCreateEnvFile)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.PostJSON[template.Template](cmd.Context(), types.Templates(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create template")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// UpdateRequest has no omitempty fields and the handler replaces the whole
		// record, so an unset --file would otherwise wipe the compose content.
		// Start from the stored template and overlay only what was passed.
		current, err := fetchTemplateInternal(cmd, c, args[0])
		if err != nil {
			return err
		}

		req := template.UpdateRequest{
			Name:        current.Name,
			Description: current.Description,
			Content:     current.Content,
		}
		if current.EnvContent != nil {
			req.EnvContent = *current.EnvContent
		}
		if cmd.Flags().Changed("name") {
			req.Name = templateUpdateName
		}
		if cmd.Flags().Changed("description") {
			req.Description = templateUpdateDescription
		}

		if templateUpdateFile != "" {
			content, err := os.ReadFile(templateUpdateFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read file %s", templateUpdateFile)
			}
			req.Content = string(content)
		}

		if templateUpdateEnvFile != "" {
			envContent, err := os.ReadFile(templateUpdateEnvFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read env file %s", templateUpdateEnvFile)
			}
			req.EnvContent = string(envContent)
		}

		result, err := c.PutJSON[template.Template](cmd.Context(), types.Template(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update template")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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

		result, err := c.PostJSON[template.Template](cmd.Context(), types.TemplateDownload(args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to download template")
		}

		if templateDownloadOutput != "" {
			dir := filepath.Dir(templateDownloadOutput)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return errors.WrapIff(err, "failed to create directory %s", dir)
			}
			if err := os.WriteFile(templateDownloadOutput, []byte(result.Data.Content), 0o600); err != nil {
				return errors.WrapIff(err, "failed to write file %s", templateDownloadOutput)
			}
			output.Success("Template downloaded to %s", templateDownloadOutput)
			return nil
		}

		fmt.Print(result.Data.Content)
		return nil
	},
}

var defaultsSaveCmd = &cobra.Command{
	Use:          "save",
	Short:        "Save default templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(templateDefaultsSaveFile)
		if err != nil {
			return errors.WrapIff(err, "failed to read file %s", templateDefaultsSaveFile)
		}

		req := template.SaveDefaultTemplatesRequest{
			ComposeContent: string(content),
		}

		if templateDefaultsEnvFile != "" {
			envContent, err := os.ReadFile(templateDefaultsEnvFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read env file %s", templateDefaultsEnvFile)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.TemplatesDefault(),
			Body:           req,
			FailureMessage: "failed to save default templates",
			SuccessMessage: "Default templates saved successfully",
			JSON:           jsonOutput,
		})
	},
}

var registriesUpdateCmd = &cobra.Command{
	Use:          "update <registry-id>",
	Short:        "Update a template registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if templateRegUpdateEnabled && templateRegUpdateDisabled {
			return errors.New("--enabled and --disabled cannot be used together")
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Every field on UpdateRegistryRequest is required and the handler does a
		// full replace, so start from the current registry and overlay only the
		// flags that were actually passed.
		current, err := fetchTemplateRegistryInternal(cmd, c, args[0])
		if err != nil {
			return err
		}

		req := template.UpdateRegistryRequest{
			Name:        current.Name,
			URL:         current.URL,
			Description: current.Description,
			Enabled:     current.Enabled,
		}
		if cmd.Flags().Changed("name") {
			req.Name = templateRegUpdateName
		}
		if cmd.Flags().Changed("url") {
			req.URL = templateRegUpdateURL
		}
		if cmd.Flags().Changed("description") {
			req.Description = templateRegUpdateDesc
		}
		if templateRegUpdateEnabled {
			req.Enabled = true
		}
		if templateRegUpdateDisabled {
			req.Enabled = false
		}

		// The handler answers with a plain message, not the updated registry.
		result, err := c.PutJSON[base.MessageResponse](cmd.Context(), types.TemplateRegistry(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update registry")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Template registry updated successfully")
		output.KeyValue("ID", current.ID)
		output.KeyValue("Name", req.Name)
		return nil
	},
}

// fetchTemplateInternal loads a single template, including its content.
func fetchTemplateInternal(cmd *cobra.Command, c *client.Client, id string) (template.Template, error) {
	result, err := c.GetJSON[template.Template](cmd.Context(), types.Template(id))
	if err != nil {
		return template.Template{}, errors.WrapIf(err, "failed to load template")
	}
	return result.Data, nil
}

// fetchTemplateRegistryInternal looks a registry up by ID. The API exposes no
// get-by-id route for template registries, so this filters the list.
func fetchTemplateRegistryInternal(cmd *cobra.Command, c *client.Client, id string) (template.TemplateRegistry, error) {
	listed, err := c.GetJSON[[]template.TemplateRegistry](cmd.Context(), types.TemplatesRegistries())
	if err != nil {
		return template.TemplateRegistry{}, errors.WrapIf(err, "failed to load registry")
	}

	for _, registry := range listed.Data {
		if registry.ID == id {
			return registry, nil
		}
	}
	return template.TemplateRegistry{}, errors.Errorf("template registry %q not found", id)
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

		path := cmdutil.AppendQuery(types.TemplateFetch(), url.Values{"url": []string{templateFetchURL}})
		result, err := c.GetJSON[template.RemoteRegistry](cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to fetch templates")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
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
	TemplatesCmd.AddCommand(deleteCmd)
	TemplatesCmd.AddCommand(getCmd)
	TemplatesCmd.AddCommand(createCmd)
	TemplatesCmd.AddCommand(updateCmd)
	TemplatesCmd.AddCommand(downloadCmd)
	TemplatesCmd.AddCommand(fetchCmd)
	defaultCmd.AddCommand(defaultsSaveCmd)
	registriesCmd.AddCommand(registriesUpdateCmd)
	registriesCmd.AddCommand(deleteRegistryCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of templates to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, "Offset for pagination")
	listCmd.Flags().BoolVarP(&templateListAll, "all", "a", false, "List all templates (including remote)")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	defaultCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	contentCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	registriesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

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

	// registries update command flags
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateName, "name", "", "Registry name")
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateURL, "url", "", "Registry URL")
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateDesc, "description", "", "Registry description")
	registriesUpdateCmd.Flags().BoolVar(&templateRegUpdateEnabled, "enabled", false, "Enable registry")
	registriesUpdateCmd.Flags().BoolVar(&templateRegUpdateDisabled, "disabled", false, "Disable registry")
	registriesUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// fetch command flags
	fetchCmd.Flags().StringVar(&templateFetchURL, "url", "", "Registry URL to fetch templates from (required)")
	_ = fetchCmd.MarkFlagRequired("url")
	fetchCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
