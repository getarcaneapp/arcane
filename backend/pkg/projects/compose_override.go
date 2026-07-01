package projects

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
)

// composeOverrideFileCandidates lists supported compose override filenames in
// detection order, sourced from compose-go.
var composeOverrideFileCandidates = slices.Clone(cli.DefaultOverrideFileNames)

// DefaultComposeOverrideFileName is the override filename Arcane writes when a
// project gains an override but has none on disk. It is deliberately
// composeOverrideFileCandidates[1] ("compose.override.yaml"), NOT [0]
// ("compose.override.yml"), so the default override extension matches Arcane's
// ".yaml" base default (DefaultComposeFileName = "compose.yaml"). Do not "fix"
// this to [0]: `docker compose` has no preference between the two, and keeping
// ".yaml" avoids a mismatched base/override extension pair.
const DefaultComposeOverrideFileName = "compose.override.yaml"

// ComposeOverrideFileCandidates returns the supported compose override filenames
// in detection order. A copy is returned so callers can't mutate package state.
func ComposeOverrideFileCandidates() []string {
	return slices.Clone(composeOverrideFileCandidates)
}

// findComposeOverrideCandidatesInternal returns every supported override
// candidate name for which exists reports true, in compose-go preference order.
// It is the single primitive behind both filesystem discovery
// (DetectComposeOverrideFile) and non-filesystem discovery
// (ResolveComposeOverride), so the two can never drift in preference order.
func findComposeOverrideCandidatesInternal(exists func(name string) bool) []string {
	found := make([]string, 0, 1)
	for _, name := range composeOverrideFileCandidates {
		if exists(name) {
			found = append(found, name)
		}
	}
	return found
}

// warnOnMultipleComposeOverridesInternal mirrors compose-go: when more than one
// override candidate is present it logs which candidates were found and which
// highest-preference match will be used.
func warnOnMultipleComposeOverridesInternal(found []string) {
	if len(found) > 1 {
		slog.Warn("multiple compose override files found; using highest-preference match",
			"using", found[0], "found", found)
	}
}

// DetectComposeOverrideFile returns the path to the highest-preference compose
// override file present in dir, following compose-go's preference order, or ""
// when none exists. When multiple override files are present it returns the
// highest-preference match and logs a warning, mirroring compose-go behavior.
func DetectComposeOverrideFile(dir string) string {
	found := findComposeOverrideCandidatesInternal(func(name string) bool {
		info, err := os.Stat(filepath.Join(dir, name))
		return err == nil && !info.IsDir()
	})
	if len(found) == 0 {
		return ""
	}
	warnOnMultipleComposeOverridesInternal(found)
	return filepath.Join(dir, found[0])
}

// ReadComposeOverrideContent returns the content of the highest-preference
// compose override file present in dir, or "" when none exists or it cannot be
// read. It is a best-effort read intended for change detection.
func ReadComposeOverrideContent(dir string) string {
	overridePath := DetectComposeOverrideFile(dir)
	if overridePath == "" {
		return ""
	}
	content, err := os.ReadFile(overridePath)
	if err != nil {
		return ""
	}
	return string(content)
}

// ResolveComposeOverride finds the highest-preference compose override file among
// the supported candidates using the exists probe, then loads it via read. It
// centralizes override discovery so non-filesystem sources (e.g. a Git working
// tree accessed through a validated client) reuse the same preference order.
// When multiple candidates exist it warns and uses the highest-preference match,
// mirroring DetectComposeOverrideFile. found is false (with empty name/content)
// when no candidate exists.
func ResolveComposeOverride(exists func(name string) bool, read func(name string) (string, error)) (fileName string, content string, found bool, err error) {
	matches := findComposeOverrideCandidatesInternal(exists)
	if len(matches) == 0 {
		return "", "", false, nil
	}
	warnOnMultipleComposeOverridesInternal(matches)
	name := matches[0]
	c, readErr := read(name)
	if readErr != nil {
		return "", "", false, readErr
	}
	return name, c, true, nil
}

// WriteComposeOverrideFile writes content as the override file named fileName in
// dir when content is non-nil, and removes every other override candidate so a
// renamed or deleted override never leaves a stale copy behind. When content is
// nil, all supported override files are removed. projectsRoot bounds writes to
// prevent path traversal.
func WriteComposeOverrideFile(projectsRoot, dir string, content *string, fileName string) error {
	keep := ""
	if content != nil {
		keep = strings.TrimSpace(fileName)
		if keep == "" {
			return errors.New("missing override file name")
		}
		if err := WriteProjectFile(projectsRoot, dir, keep, *content); err != nil {
			return err
		}
	}

	// Remove stale override files: deleted from the source, or renamed to a
	// different candidate name than the one currently written.
	for _, candidate := range composeOverrideFileCandidates {
		if candidate == keep {
			continue
		}
		if err := RemoveProjectFile(projectsRoot, dir, candidate); err != nil {
			return err
		}
	}

	return nil
}
