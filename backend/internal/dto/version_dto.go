package dto

type VersionInfoDto struct {
	CurrentVersion  string `json:"currentVersion"`
	CurrentTag      string `json:"currentTag,omitempty"`
	CurrentDigest   string `json:"currentDigest,omitempty"`
	Revision        string `json:"revision"`
	DisplayVersion  string `json:"displayVersion"`
	IsSemverVersion bool   `json:"isSemverVersion"`
	NewestVersion   string `json:"newestVersion,omitempty"`
	NewestDigest    string `json:"newestDigest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
}
