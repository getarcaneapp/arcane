package resources

import "embed"

// Embedded file systems for the project

//go:embed migrations images scalar aaguids.json
var FS embed.FS
