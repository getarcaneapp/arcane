package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/utils/cache"
)

const (
	versionTTL            = 3 * time.Hour
	versionCheckURL       = "https://api.github.com/repos/ofkm/arcane/releases/latest"
	defaultRequestTimeout = 5 * time.Second
)

type VersionService struct {
	httpClient *http.Client
	cache      *cache.Cache[string]
	disabled   bool
	version    string
	revision   string
}

func NewVersionService(httpClient *http.Client, disabled bool, version string, revision string) *VersionService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &VersionService{
		httpClient: httpClient,
		cache:      cache.New[string](versionTTL),
		disabled:   disabled,
		version:    version,
		revision:   revision,
	}
}

func (s *VersionService) GetLatestVersion(ctx context.Context) (string, error) {
	version, err := s.cache.GetOrFetch(ctx, func(ctx context.Context) (string, error) {
		reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, versionCheckURL, nil)
		if err != nil {
			return "", fmt.Errorf("create GitHub request: %w", err)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("get latest release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var payload struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return "", fmt.Errorf("decode payload: %w", err)
		}
		if payload.TagName == "" {
			return "", fmt.Errorf("GitHub API returned empty tag name")
		}

		return payload.TagName, nil
	})

	var staleErr *cache.ErrStale
	if errors.As(err, &staleErr) {
		slog.Warn("Failed to fetch latest version, returning stale cache", "error", staleErr.Err)
		return version, nil
	}

	return version, err
}

func (s *VersionService) IsNewer(latest, current string) bool {
	lp := parseSemver(latest)
	cp := parseSemver(current)
	for i := 0; i < 3; i++ {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

func (s *VersionService) ReleaseURL(version string) string {
	if strings.TrimSpace(version) == "" {
		return "https://github.com/ofkm/arcane/releases/latest"
	}
	v := strings.TrimSpace(version)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return "https://github.com/ofkm/arcane/releases/tag/" + v
}

type VersionInformation struct {
	CurrentVersion  string `json:"currentVersion"`
	NewestVersion   string `json:"newestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
}

func (s *VersionService) GetVersionInformation(ctx context.Context, currentVersion string) (*VersionInformation, error) {
	if currentVersion == "" {
		currentVersion = s.version
	}
	cur := strings.TrimSpace(currentVersion)
	if !strings.HasPrefix(cur, "v") && len(cur) > 0 && cur[0] >= '0' && cur[0] <= '9' {
		cur = "v" + cur
	}

	if s.disabled {
		return &VersionInformation{
			CurrentVersion:  cur,
			NewestVersion:   "",
			UpdateAvailable: false,
			ReleaseURL:      s.ReleaseURL(""),
		}, nil
	}

	latest, err := s.GetLatestVersion(ctx)
	if err != nil {
		var staleErr *cache.ErrStale
		if errors.As(err, &staleErr) {
			slog.Warn("Failed to refresh latest version; using stale cache", "error", staleErr.Err)
		} else {
			return &VersionInformation{
				CurrentVersion: cur,
				ReleaseURL:     s.ReleaseURL(""),
			}, err
		}
	}

	return &VersionInformation{
		CurrentVersion:  cur,
		NewestVersion:   latest,
		UpdateAvailable: s.IsNewer(latest, cur),
		ReleaseURL:      s.ReleaseURL(latest),
	}, nil
}

// isSemverVersion checks if a version string is semver-based (e.g., v1.0.0)
func (s *VersionService) isSemverVersion() bool {
	version := strings.TrimPrefix(strings.TrimSpace(s.version), "v")
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		// Extract just the numeric part (ignoring suffixes like -alpha, +build)
		numPart := parts[i]
		if i == len(parts)-1 {
			// Last part might have additional info like "0-alpha" or "0+build"
			numPart = strings.FieldsFunc(numPart, func(r rune) bool {
				return r == '-' || r == '+'
			})[0]
		}
		if numPart == "" {
			return false
		}
		for _, c := range numPart {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// getDisplayVersion formats the version for display purposes
// If version contains "next", it returns "next-<revision>"
// Otherwise returns the version as-is
func (s *VersionService) getDisplayVersion() string {
	version := strings.TrimPrefix(strings.TrimSpace(s.version), "v")
	if strings.Contains(strings.ToLower(version), "next") && s.revision != "" && s.revision != "unknown" {
		return fmt.Sprintf("next-%s", s.revision)
	}
	if s.isSemverVersion() {
		return "v" + version
	}
	return version
}

// GetAppVersionInfo returns application version information including display version
func (s *VersionService) GetAppVersionInfo(ctx context.Context) *dto.VersionInfoDto {
	version := strings.TrimSpace(s.version)
	if s.isSemverVersion() && !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	displayVersion := s.getDisplayVersion()
	isSemver := s.isSemverVersion()

	if s.disabled {
		return &dto.VersionInfoDto{
			CurrentVersion:  version,
			DisplayVersion:  displayVersion,
			Revision:        s.revision,
			IsSemverVersion: isSemver,
			UpdateAvailable: false,
		}
	}

	latest, err := s.GetLatestVersion(ctx)
	if err != nil {
		var staleErr *cache.ErrStale
		if !errors.As(err, &staleErr) {
			return &dto.VersionInfoDto{
				CurrentVersion:  version,
				DisplayVersion:  displayVersion,
				Revision:        s.revision,
				IsSemverVersion: isSemver,
			}
		}
		slog.Warn("Failed to refresh latest version; using stale cache", "error", staleErr.Err)
	}

	return &dto.VersionInfoDto{
		CurrentVersion:  version,
		DisplayVersion:  displayVersion,
		Revision:        s.revision,
		IsSemverVersion: isSemver,
		NewestVersion:   latest,
		UpdateAvailable: s.IsNewer(latest, version) && isSemver,
		ReleaseURL:      s.ReleaseURL(latest),
	}
}

func parseSemver(s string) [3]int {
	var out [3]int
	part := 0
	num := 0
	sign := 1

	flush := func() {
		if part < 3 {
			out[part] = sign * num
			part++
		}
		num = 0
		sign = 1
	}

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '-':
			i = len(s)
		case c == '.':
			flush()
		case c >= '0' && c <= '9':
			num = num*10 + int(c-'0')
		case c == '+' || c == 'v' || c == 'V':
		default:
			i = len(s)
		}
	}
	flush()
	return out
}
