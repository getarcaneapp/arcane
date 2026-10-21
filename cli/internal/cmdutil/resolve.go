package cmdutil

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
)

// MaxPromptOptions caps how many search matches the resolver offers
// interactively before asking the user to refine the query instead.
const MaxPromptOptions = 20

// ResourceRef resolves a resource from a name, ID, or prefix.
// D is the detail shape and S is the list summary.
type ResourceRef[D any, S any] struct {
	EnvironmentScoped bool
	// Singular and Plural name the resource in prompts and error messages
	// ("container", "containers").
	Singular string
	Plural   string
	// IDHint completes "use <IDHint> or run `<ListCmd>`" guidance
	// ("the container ID", "the volume name").
	IDHint  string
	ListCmd string

	// GetPath builds the GET-by-identifier path; ListPath the list endpoint
	// that Resolve queries with ?search=<identifier>&limit=-1.
	GetPath  func(envID, identifier string) string
	ListPath func(envID string) string
	// SearchCandidates replaces the standard list search for resources with
	// specialized matching rules.
	SearchCandidates func(context.Context, *client.Client, string) ([]S, error)
	// SelectCandidate replaces the standard ambiguity handling while keeping
	// direct lookup and not-found behavior in Resolve.
	SelectCandidate func([]S, string, bool) (*S, error)
	// Validate checks a successful direct lookup before Resolve returns it.
	Validate func(D, string) error

	// Matches reports whether a listed item matches the identifier.
	Matches func(item S, identifierLower, original string) bool
	// Label renders one match as an interactive prompt option.
	Label func(S) string
	// Promote converts a selected summary into the detail shape.
	Promote func(S) *D

	// IDOf enables an ID-prefix scan over the full list. Leave nil for resources without opaque IDs.
	IDOf func(S) string
	// IdentifierOf supplies the canonical identifier for commands accepting raw IDs.
	IdentifierOf func(D) string

	// Exact identifies exact matches so a unique exact search result wins over substring matches.
	Exact func(item S, identifierLower, original string) bool
}

type resourceCandidate[D any] struct {
	details  *D
	complete bool
	exact    bool
	envID    string
	label    string
}

// Fetch resolves an identifier and retrieves full detail when resolution returned only a summary.
func (ref ResourceRef[D, S]) Fetch(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*D, error) {
	resolved, complete, err := ref.Resolve(ctx, c, identifier, allowPrompt)
	if err != nil || complete {
		return resolved, err
	}
	if ref.GetPath == nil || ref.IdentifierOf == nil {
		return nil, fmt.Errorf("%s reference requires GetPath and IdentifierOf to fetch detail", ref.Singular)
	}
	result, fetchErr := c.GetJSON[D](ctx, ref.GetPath(c.EnvID(), ref.IdentifierOf(*resolved)))
	if fetchErr != nil {
		return nil, fetchErr
	}
	if ref.Validate != nil {
		if validateErr := ref.Validate(result.Data, identifier); validateErr != nil {
			return nil, validateErr
		}
	}
	return &result.Data, nil
}

// Resolve finds one resource and pins its environment for subsequent requests.
func (ref ResourceRef[D, S]) Resolve(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*D, bool, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, false, fmt.Errorf("%s identifier is required", ref.Singular)
	}
	app, _ := runtime.From(ctx)
	preview := app.PreviewEnabled() && ref.EnvironmentScoped
	environments := []types.Environment{{ID: c.EnvID()}}
	if preview {
		var discoveryErr error
		environments, discoveryErr = app.Environments(ctx)
		if discoveryErr != nil {
			return nil, false, discoveryErr
		}
		allowPrompt = !app.IsJSON() && prompt.IsInteractive()
	}
	originalEnv := c.EnvID()
	defer func() {
		if preview && app.PreviewEnabled() {
			c.SetEnvironment(originalEnv)
		}
	}()
	var candidates []resourceCandidate[D]
	var failures []error
	for _, env := range environments {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, false, cancelErr
		}
		c.SetEnvironment(env.ID)
		matches, lookupErr := ref.candidates(ctx, c, identifier, allowPrompt, preview)
		switch {
		case lookupErr != nil && !preview:
			return nil, false, lookupErr
		case lookupErr != nil:
			failures = append(failures, fmt.Errorf("%s (%s): %w", cmp.Or(env.Name, env.ID), env.ID, lookupErr))
		}
		for i := range matches {
			matches[i].envID = env.ID
			if preview {
				matches[i].label = fmt.Sprintf("%s — %s (%s)", matches[i].label, cmp.Or(env.Name, env.ID), env.ID)
			}
		}
		candidates = append(candidates, matches...)
	}
	if slices.ContainsFunc(candidates, func(item resourceCandidate[D]) bool { return item.exact }) {
		candidates = slices.DeleteFunc(candidates, func(item resourceCandidate[D]) bool { return !item.exact })
	}
	for _, failure := range failures {
		output.Warning("%v", failure)
	}
	if len(candidates) == 0 {
		return nil, false, errors.Join(append(failures, fmt.Errorf("%s %q not found; use %s or run `%s`", ref.Singular, identifier, ref.IDHint, ref.ListCmd))...)
	}
	choice := 0
	prompted := false
	if len(candidates) > 1 || len(failures) > 0 {
		if !allowPrompt {
			if preview {
				return nil, false, fmt.Errorf("%s %q requires an environment selection; pass --env <id> or run `%s`", ref.Singular, identifier, ref.ListCmd)
			}
			return nil, false, fmt.Errorf("multiple %s match %q; use %s or run `%s`", ref.Plural, identifier, ref.IDHint, ref.ListCmd)
		}
		if len(candidates) > MaxPromptOptions {
			return nil, false, fmt.Errorf("multiple %s match %q (%d results); refine your query or use %s", ref.Plural, identifier, len(candidates), ref.IDHint)
		}
		options := make([]string, len(candidates))
		for i, item := range candidates {
			options[i] = item.label
		}
		var selectErr error
		choice, selectErr = prompt.Select(ref.Singular, options)
		prompted = true
		if selectErr != nil {
			return nil, false, selectErr
		}
	}
	selected := candidates[choice]
	if preview {
		app.SelectEnvironment(selected.envID)
		c.SetEnvironment(selected.envID)
	}
	if preview && !prompted {
		_, _ = fmt.Fprintf(output.Stderr(), "Selected %s: %s\n", ref.Singular, selected.label)
	}
	return selected.details, selected.complete, nil
}

func (ref ResourceRef[D, S]) candidates(ctx context.Context, c *client.Client, identifier string, allowPrompt, collect bool) ([]resourceCandidate[D], error) {
	details, found, err := ref.fetchByIdentifier(ctx, c, identifier)
	if err != nil {
		return nil, err
	}
	if found {
		return []resourceCandidate[D]{{details: details, complete: true, exact: true, label: identifier}}, nil
	}
	var matches []S
	if ref.SearchCandidates != nil {
		matches, err = ref.SearchCandidates(ctx, c, identifier)
	} else {
		matches, err = ref.searchMatches(ctx, c, identifier)
	}
	if err != nil && len(matches) == 0 {
		return nil, err
	}
	identifierLower := strings.ToLower(identifier)
	if ref.Exact != nil && !collect {
		exact := slices.Clone(matches)
		exact = slices.DeleteFunc(exact, func(item S) bool { return !ref.Exact(item, identifierLower, identifier) })
		if len(exact) == 1 {
			matches = exact
		}
	}
	if ref.SelectCandidate != nil && !collect {
		selected, selectErr := ref.SelectCandidate(matches, identifier, allowPrompt)
		if selectErr != nil {
			return nil, selectErr
		}
		matches = nil
		if selected != nil {
			matches = []S{*selected}
		}
	}
	if len(matches) == 0 && ref.IDOf != nil && LooksLikeIDPrefix(identifierLower) {
		matches, err = ref.fallbackByIDPrefix(ctx, c, identifierLower)
		if err != nil {
			return nil, err
		}
	}
	result := make([]resourceCandidate[D], len(matches))
	for i, item := range matches {
		result[i] = resourceCandidate[D]{details: ref.Promote(item), label: ref.Label(item), exact: collect && ref.Exact != nil && ref.Exact(item, identifierLower, identifier)}
	}
	return result, err
}

func (ref ResourceRef[D, S]) fetchByIdentifier(ctx context.Context, c *client.Client, identifier string) (*D, bool, error) {
	if ref.GetPath == nil {
		return nil, false, nil
	}
	resp, err := c.Get(ctx, ref.GetPath(c.EnvID(), identifier))
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve %s %q: %w", ref.Singular, identifier, err)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %s response: %w", ref.Singular, err)
	}

	if resp.StatusCode == http.StatusOK {
		var result struct {
			types.ApiResponse[D]
			types.ErrorResponse
		}
		if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
			return nil, false, fmt.Errorf("failed to parse %s response: %w", ref.Singular, unmarshalErr)
		}
		if !result.Success {
			return nil, false, fmt.Errorf("API error: %s", cmp.Or(result.Error, "request was not successful"))
		}
		if ref.Validate != nil {
			if validateErr := ref.Validate(result.Data, identifier); validateErr != nil {
				return nil, false, validateErr
			}
		}
		return &result.Data, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, false, fmt.Errorf("failed to resolve %s %q (status %d): %s", ref.Singular, identifier, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil, false, nil
}

func (ref ResourceRef[D, S]) searchMatches(ctx context.Context, c *client.Client, identifier string) ([]S, error) {
	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", ref.ListPath(c.EnvID()), url.QueryEscape(identifier), ShowAllLimit)
	items, err := ref.listItems(ctx, c, searchPath)
	if err != nil {
		return nil, err
	}

	identifierLower := strings.ToLower(identifier)
	matches := make([]S, 0)
	for _, item := range items {
		if ref.Matches(item, identifierLower, identifier) {
			matches = append(matches, item)
		}
	}
	return matches, nil
}

func (ref ResourceRef[D, S]) fallbackByIDPrefix(ctx context.Context, c *client.Client, identifierLower string) ([]S, error) {
	items, err := ref.listItems(ctx, c, fmt.Sprintf("%s?limit=%d", ref.ListPath(c.EnvID()), ShowAllLimit))
	if err != nil {
		return nil, err
	}
	matches := make([]S, 0)
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(ref.IDOf(item)), identifierLower) {
			matches = append(matches, item)
		}
	}
	return matches, nil
}

func (ref ResourceRef[D, S]) listItems(ctx context.Context, c *client.Client, path string) ([]S, error) {
	resp, err := c.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to search %s: %w", ref.Plural, err)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read %s response: %w", ref.Plural, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to search %s (status %d): %s", ref.Plural, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		types.ApiResponse[[]S]
		types.ErrorResponse
	}
	if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", ref.Plural, unmarshalErr)
	}
	if !result.Success {
		return nil, fmt.Errorf("API error: %s", cmp.Or(result.Error, "request was not successful"))
	}
	return result.Data, nil
}

// LooksLikeIDPrefix reports whether identifier could be a hex ID prefix worth
// scanning the full list for.
func LooksLikeIDPrefix(identifierLower string) bool {
	return len(identifierLower) >= 4 && strings.IndexFunc(identifierLower, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	}) == -1
}
