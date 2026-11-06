package types

import (
	"cmp"
	"strings"
	"time"
)

type (
	VersionInfo struct {
		CurrentVersion   string   `json:"currentVersion"`
		CurrentTag       string   `json:"currentTag,omitempty"`
		CurrentDigest    string   `json:"currentDigest,omitempty"`
		Revision         string   `json:"revision"`
		ShortRevision    string   `json:"shortRevision"`
		GoVersion        string   `json:"goVersion"`
		NodeVersion      string   `json:"nodeVersion"`
		SvelteKitVersion string   `json:"svelteKitVersion"`
		BuildTime        string   `json:"buildTime,omitempty"`
		DisplayVersion   string   `json:"displayVersion"`
		NewestVersion    string   `json:"newestVersion,omitempty"`
		NewestDigest     string   `json:"newestDigest,omitempty"`
		ReleaseURL       string   `json:"releaseUrl,omitempty"`
		ReleaseNotes     string   `json:"releaseNotes,omitempty"`
		ReleasedAt       string   `json:"releasedAt,omitempty"`
		EnabledFeatures  []string `json:"enabledFeatures,omitempty"`
		IsSemverVersion  bool     `json:"isSemverVersion"`
		IsStableVersion  bool     `json:"isStableVersion"`
		UpdateAvailable  bool     `json:"updateAvailable"`
	}

	VersionCheck struct {
		CurrentVersion  string `json:"currentVersion"`
		NewestVersion   string `json:"newestVersion,omitempty"`
		ReleaseURL      string `json:"releaseUrl,omitempty"`
		UpdateAvailable bool   `json:"updateAvailable"`
	}

	VersionStreamEvent struct {
		Timestamp time.Time    `json:"timestamp"`
		Info      *VersionInfo `json:"info,omitempty"`
		Type      string       `json:"type"`
	}
)

func (i *VersionInfo) AlreadyOnNewest() bool {
	if i == nil || i.UpdateAvailable {
		return false
	}

	currentDigest := strings.TrimSpace(i.CurrentDigest)
	newestDigest := strings.TrimSpace(i.NewestDigest)

	// Digests alone catch a mutable tag rebuilt at the same version. Once either side
	// resolves, both must resolve and agree; a matching version tag is not enough.
	switch {
	case cmp.Or(currentDigest, newestDigest) != "":
		return currentDigest == newestDigest
	case i.NewestVersion != "":
		return strings.TrimPrefix(i.NewestVersion, "v") == strings.TrimPrefix(i.CurrentVersion, "v")
	default:
		return false
	}
}
