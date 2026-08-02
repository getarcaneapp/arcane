package cmdutil

import (
	"fmt"
	"io"
	"strings"

	"emperror.dev/errors"

	"github.com/spf13/cobra"
)

// Confirm prompts the user unless --yes is enabled.
func Confirm(cmd *cobra.Command, prompt string) (bool, error) {
	if AssumeYes(cmd) {
		return true, nil
	}

	fmt.Printf("%s (y/N): ", strings.TrimSpace(prompt))
	var response string
	if _, err := fmt.Scanln(&response); err != nil && !errors.Is(err, io.EOF) {
		// Keep EOF as a default "no" response, but surface other input failures.
		return false, errors.WrapIf(err, "failed to read confirmation input")
	}

	switch strings.ToLower(strings.TrimSpace(response)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
