package libbuild

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

type GitBuildContextSource struct {
	Raw           string
	RepositoryURL string
	Ref           string
	Subdir        string
}

func ParseGitBuildContextSource(raw string) (*GitBuildContextSource, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false, nil
	}

	repositoryURL, fragment, hasFragment := strings.Cut(trimmed, "#")
	repositoryURL = strings.TrimSpace(repositoryURL)
	if !IsSupportedGitRepositoryURL(repositoryURL) {
		return nil, false, nil
	}

	source := &GitBuildContextSource{
		Raw:           trimmed,
		RepositoryURL: strings.TrimRight(repositoryURL, "/"),
	}

	if !hasFragment {
		return source, true, nil
	}

	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return nil, true, fmt.Errorf("git build context fragment cannot be empty")
	}

	ref, subdir, hasSubdir := strings.Cut(fragment, ":")
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, true, fmt.Errorf("git build context ref cannot be empty")
	}
	source.Ref = ref

	if !hasSubdir {
		return source, true, nil
	}

	subdir = strings.TrimSpace(subdir)
	if subdir == "" {
		return nil, true, fmt.Errorf("git build context subdir cannot be empty")
	}
	if strings.HasPrefix(subdir, "/") {
		return nil, true, fmt.Errorf("git build context subdir must be relative")
	}

	cleanSubdir := path.Clean(subdir)
	if cleanSubdir == "." || cleanSubdir == ".." || strings.HasPrefix(cleanSubdir, "../") {
		return nil, true, fmt.Errorf("git build context subdir must stay within the repository")
	}

	source.Subdir = cleanSubdir
	return source, true, nil
}

func NormalizeGitBuildContextSourceForMatch(raw string) string {
	source, ok, err := ParseGitBuildContextSource(raw)
	if err != nil || !ok || source == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(source.RepositoryURL), "/")
}

func IsPotentialRemoteBuildContextSource(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}

	if strings.HasPrefix(trimmed, "git@") {
		return true
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ssh", "git":
		return parsed.Host != ""
	default:
		return false
	}
}

func IsSupportedGitRepositoryURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}

	if strings.HasPrefix(trimmed, "git@") {
		return true
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}

	switch strings.ToLower(parsed.Scheme) {
	case "git", "ssh":
		return true
	case "http", "https":
		return strings.HasSuffix(strings.ToLower(parsed.Path), ".git")
	default:
		return false
	}
}
