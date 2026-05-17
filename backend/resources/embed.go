package resources

import "embed"

// Embedded file systems for the project

//go:embed migrations images email-templates fonts
var FS embed.FS
