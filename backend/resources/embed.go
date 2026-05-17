package resources

import "embed"

// Embedded file systems for the project

//go:embed migrations images email-templates fonts manual_updates.json
var FS embed.FS
