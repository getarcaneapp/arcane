package manager

import (
	"os/exec"

	"github.com/docker/cli/cli-plugins/metadata"
)

type candidate struct {
	path string
}

func (c *candidate) Path() string {
	return c.path
}

func (c *candidate) Metadata() ([]byte, error) {
	return exec.Command(c.path, metadata.MetadataSubcommandName).Output() // #nosec G204 -- ignore "Subprocess launched with a potential tainted input or cmd arguments"
}
