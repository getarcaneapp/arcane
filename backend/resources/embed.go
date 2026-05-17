package resources

import "embed"

// Embedded file systems for the project

//go:embed migrations images email-templates fonts migration_versions.json
var FS embed.FS
