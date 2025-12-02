package gitutil

import (
	"os/exec"
)

func gitPath(_ string) (string, error) {
	return exec.LookPath("git.exe")
}
