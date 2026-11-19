//go:build exclude_frontend

package resources

import "embed"

//go:embed images aaguids.json secretsources
var FS embed.FS
