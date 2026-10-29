package version

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"go.getarcane.app/kit/pkg"
)

const (
	repoURL              = "https://github.com/getarcaneapp/arcane"
	commitCompareURLBase = "https://api.github.com/repos/getarcaneapp/arcane/compare/"
)

type commitParser struct {
	group   string
	pattern *regexp.Regexp
}

var (
	commitSHAPattern      = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	conventionalPattern   = regexp.MustCompile(`^\w+(\([^)]*\))?!?: `)
	pullRequestRefPattern = regexp.MustCompile(`\s*\((\w+\s)?#(\d+)\)`)
	commitParsers         = []commitParser{
		{group: "CLI - New features", pattern: regexp.MustCompile(`^feat\(cli\)`)},
		{group: "New features", pattern: regexp.MustCompile(`^feat`)},
		{group: "CLI - Bug fixes", pattern: regexp.MustCompile(`^fix\(cli\)`)},
		{group: "Bug fixes", pattern: regexp.MustCompile(`^fix`)},
		{group: "CLI - Performance improvements", pattern: regexp.MustCompile(`^perf\(cli\)`)},
		{group: "Performance improvements", pattern: regexp.MustCompile(`^perf`)},
		{group: "CLI - Documentation", pattern: regexp.MustCompile(`^docs\(cli\)`)},
		{group: "Documentation", pattern: regexp.MustCompile(`^docs`)},
		{pattern: regexp.MustCompile(`^(chore|release)|update translations via Crowdin`)},
		{group: "Other", pattern: regexp.MustCompile(`.*`)},
	}
	commitGroupOrder = []string{
		"New features",
		"CLI - New features",
		"Bug fixes",
		"CLI - Bug fixes",
		"Performance improvements",
		"CLI - Performance improvements",
		"Documentation",
		"CLI - Documentation",
		"Other",
	}
)

type commitComparison struct {
	TotalCommits int `json:"total_commits"`
	Commits      []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	} `json:"commits"`
}

func isCommitSHA(revision string) bool {
	return commitSHAPattern.MatchString(strings.ToLower(strings.TrimSpace(revision)))
}

func compareURL(base, head string) string {
	return repoURL + "/compare/" + base + "..." + head
}

func (s *VersionService) commitNotes(ctx context.Context, base, head string) string {
	key := base + "..." + head
	notes, found, err := s.commitNotesCache.GetWithLoaders(key, func(_ []string) (map[string]string, error) {
		loadCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()
		comparison, fetchErr := s.fetchCommitComparison(loadCtx, key)
		if fetchErr != nil {
			return nil, fetchErr
		}
		return map[string]string{key: formatCommitNotes(comparison, compareURL(base, head))}, nil
	})
	if err != nil || !found {
		slog.WarnContext(ctx, "Failed to build commit release notes", "range", key, "error", err)
		return ""
	}
	return notes
}

func (s *VersionService) fetchCommitComparison(ctx context.Context, commitRange string) (commitComparison, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, commitCompareURLBase+commitRange, http.NoBody)
	if err != nil {
		return commitComparison{}, fmt.Errorf("create GitHub compare request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return commitComparison{}, fmt.Errorf("compare commits: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return commitComparison{}, fmt.Errorf("GitHub compare API returned status %d", resp.StatusCode)
	}

	var comparison commitComparison
	if unmarshalReadErr := json.UnmarshalRead(resp.Body, &comparison); unmarshalReadErr != nil {
		return commitComparison{}, fmt.Errorf("decode compare payload: %w", unmarshalReadErr)
	}
	return comparison, nil
}

func formatCommitNotes(comparison commitComparison, diffURL string) string {
	grouped := make(map[string][]string, len(commitGroupOrder))
	for _, c := range slices.Backward(comparison.Commits) {
		subject := strings.TrimSpace(strings.SplitN(c.Commit.Message, "\n", 2)[0])
		if !conventionalPattern.MatchString(subject) {
			continue
		}
		group := commitParsers[slices.IndexFunc(commitParsers, func(p commitParser) bool {
			return p.pattern.MatchString(subject)
		})].group
		if group == "" {
			continue
		}
		grouped[group] = append(grouped[group], fmt.Sprintf("- %s (%s%s)",
			pullRequestRefPattern.ReplaceAllString(subject, ""),
			commitLink(pullRequestRefPattern.FindStringSubmatch(subject), c.SHA),
			kit.Ternary(c.Author.Login != "", " by @"+c.Author.Login, "")))
	}

	var notes strings.Builder
	for _, group := range commitGroupOrder {
		if len(grouped[group]) == 0 {
			continue
		}
		fmt.Fprintf(&notes, "### %s\n\n%s\n\n", group, strings.Join(grouped[group], "\n"))
	}
	if notes.Len() > 0 && comparison.TotalCommits > len(comparison.Commits) {
		fmt.Fprintf(&notes, "[And %d more commits](%s)\n", comparison.TotalCommits-len(comparison.Commits), diffURL)
	}
	return strings.TrimSpace(notes.String())
}

func commitLink(pr []string, sha string) string {
	if pr != nil {
		return fmt.Sprintf("[#%s](%s/pull/%s)", pr[2], repoURL, pr[2])
	}
	return fmt.Sprintf("[%s](%s/commit/%s)", sha[:min(7, len(sha))], repoURL, sha)
}
