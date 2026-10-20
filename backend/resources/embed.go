package resources

import "embed"

// Embedded file systems for the project

//go:embed migrations images aaguids.json
var FS embed.FS
