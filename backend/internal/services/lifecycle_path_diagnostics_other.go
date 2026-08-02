//go:build !unix

package services

import "fmt"

func describeLifecyclePathAccessInternal(projectPath, resolvedScriptPath string) string {
	return fmt.Sprintf(
		"Project path=%q, resolved script path=%q. "+
			"Ensure the Arcane process can traverse every parent directory and inspect the script. "+
			"A script being executable is not sufficient when ownership, ACLs, or mount permissions block access.",
		projectPath,
		resolvedScriptPath,
	)
}
