package cmdutil

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

// MaxPromptOptions caps how many search matches the resolver offers
// interactively before asking the user to refine the query instead.
const MaxPromptOptions = 20

// ResourceRef describes how one resource kind is resolved from a name, ID, or
// prefix. D is the detail type returned by the GET-by-identifier endpoint and
// S the summary type returned by the list endpoint.
type ResourceRef[D any, S any] struct {
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

	// IDOf enables the last-resort ID-prefix scan over the full list for
	// resources addressed by opaque IDs (containers, images). Leave nil to
	// skip that fallback.
	IDOf func(S) string

	// Exact optionally reports an exact identifier match. When set and
	// exactly one search match is exact, it wins without prompting even if
	// broader substring matches exist.
	Exact func(item S, identifierLower, original string) bool
}

// Resolve finds one resource by exact identifier or search. The bool result
// reports whether the returned details are complete (fetched from the
// GET-by-identifier endpoint) rather than promoted from a list row.
func (ref ResourceRef[D, S]) Resolve(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*D, bool, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, false, errors.Errorf("%s identifier is required", ref.Singular)
	}

	details, found, err := ref.fetchByIdentifierInternal(ctx, c, trimmed)
	if err != nil {
		return nil, false, err
	}
	if found {
		return details, true, nil
	}

	var matches []S
	if ref.SearchCandidates != nil {
		matches, err = ref.SearchCandidates(ctx, c, trimmed)
	} else {
		matches, err = ref.searchMatchesInternal(ctx, c, trimmed)
	}
	if err != nil {
		return nil, false, err
	}

	if ref.Exact != nil && len(matches) > 1 {
		identifierLower := strings.ToLower(trimmed)
		exact := make([]S, 0, 1)
		for _, match := range matches {
			if ref.Exact(match, identifierLower, trimmed) {
				exact = append(exact, match)
			}
		}
		if len(exact) == 1 {
			matches = exact
		}
	}

	selected, err := ref.selectCandidateInternal(matches, trimmed, allowPrompt)
	if err != nil {
		return nil, false, err
	}
	if selected != nil {
		return ref.Promote(*selected), false, nil
	}

	if ref.IDOf != nil {
		identifierLower := strings.ToLower(trimmed)
		if LooksLikeIDPrefix(identifierLower) {
			fallbackMatches, err := ref.fallbackByIDPrefixInternal(ctx, c, identifierLower)
			if err != nil {
				return nil, false, err
			}
			selected, err := ref.selectCandidateInternal(fallbackMatches, trimmed, allowPrompt)
			if err != nil {
				return nil, false, err
			}
			if selected != nil {
				return ref.Promote(*selected), false, nil
			}
		}
	}

	return nil, false, errors.Errorf("%s %q not found; use %s or run `%s`", ref.Singular, trimmed, ref.IDHint, ref.ListCmd)
}

func (ref ResourceRef[D, S]) fetchByIdentifierInternal(ctx context.Context, c *client.Client, identifier string) (*D, bool, error) {
	resp, err := c.Get(ctx, ref.GetPath(c.EnvID(), identifier))
	if err != nil {
		return nil, false, errors.WrapIff(err, "failed to resolve %s %q", ref.Singular, identifier)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, false, errors.WrapIff(err, "failed to read %s response", ref.Singular)
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[D]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, false, errors.WrapIff(err, "failed to parse %s response", ref.Singular)
		}
		if ref.Validate != nil {
			if err := ref.Validate(result.Data, identifier); err != nil {
				return nil, false, err
			}
		}
		return &result.Data, true, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return nil, false, errors.Errorf("failed to resolve %s %q (status %d): %s", ref.Singular, identifier, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil, false, nil
}

func (ref ResourceRef[D, S]) searchMatchesInternal(ctx context.Context, c *client.Client, identifier string) ([]S, error) {
	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", ref.ListPath(c.EnvID()), url.QueryEscape(identifier), ShowAllLimit)
	items, err := ref.listItemsInternal(ctx, c, searchPath)
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

func (ref ResourceRef[D, S]) selectCandidateInternal(matches []S, identifier string, allowPrompt bool) (*S, error) {
	if ref.SelectCandidate != nil {
		return ref.SelectCandidate(matches, identifier, allowPrompt)
	}
	return ref.selectMatchInternal(matches, identifier, allowPrompt)
}

func (ref ResourceRef[D, S]) selectMatchInternal(matches []S, identifier string, allowPrompt bool) (*S, error) {
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) == 0 {
		return nil, nil
	}

	if !allowPrompt {
		return nil, errors.Errorf("multiple %s match %q; use %s or run `%s`", ref.Plural, identifier, ref.IDHint, ref.ListCmd)
	}
	if len(matches) > MaxPromptOptions {
		return nil, errors.Errorf("multiple %s match %q (%d results); refine your query or use %s", ref.Plural, identifier, len(matches), ref.IDHint)
	}

	options := make([]string, 0, len(matches))
	for _, match := range matches {
		options = append(options, ref.Label(match))
	}
	choice, err := prompt.Select(ref.Singular, options)
	if err != nil {
		return nil, err
	}
	return &matches[choice], nil
}

func (ref ResourceRef[D, S]) fallbackByIDPrefixInternal(ctx context.Context, c *client.Client, identifierLower string) ([]S, error) {
	items, err := ref.listItemsInternal(ctx, c, fmt.Sprintf("%s?limit=%d", ref.ListPath(c.EnvID()), ShowAllLimit))
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

func (ref ResourceRef[D, S]) listItemsInternal(ctx context.Context, c *client.Client, path string) ([]S, error) {
	resp, err := c.Get(ctx, path)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to search %s", ref.Plural)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, errors.WrapIff(err, "failed to read %s response", ref.Plural)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("failed to search %s (status %d): %s", ref.Plural, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result base.Paginated[S]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.WrapIff(err, "failed to parse %s response", ref.Plural)
	}
	return result.Data, nil
}

// LooksLikeIDPrefix reports whether identifier could be a hex ID prefix worth
// scanning the full list for.
func LooksLikeIDPrefix(identifierLower string) bool {
	if len(identifierLower) < 4 {
		return false
	}
	for _, r := range identifierLower {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
