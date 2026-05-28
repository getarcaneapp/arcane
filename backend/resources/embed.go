package resources

import "embed"

// Embedded file systems for the project

//go:embed migrations images email-templates
var FS embed.FS
