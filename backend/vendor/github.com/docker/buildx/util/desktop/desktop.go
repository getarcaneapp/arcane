package desktop

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/containerd/console"
)

var (
	bbEnabledOnce sync.Once
	bbEnabled     bool
)

func BuildBackendEnabled() bool {
	bbEnabledOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		_, err = os.Stat(filepath.Join(home, ".docker", "desktop-build", ".lastaccess"))
		bbEnabled = err == nil
	})
	return bbEnabled
}

func BuildURL(ref string) string {
	return fmt.Sprintf("docker-desktop://dashboard/build/%s", ref)
}

func BuildDetailsOutput(refs map[string]string, term bool) string {
	if len(refs) == 0 {
		return ""
	}
	var out bytes.Buffer
	out.WriteString("View build details: ")
	multiTargets := len(refs) > 1
	for target, ref := range refs {
		if multiTargets {
			out.WriteString(fmt.Sprintf("\n  %s: ", target))
		}
		if term {
			url := BuildURL(ref)
			out.WriteString(ANSIHyperlink(url, url))
		} else {
			out.WriteString(BuildURL(ref))
		}
	}
	return out.String()
}

func PrintBuildDetails(w io.Writer, refs map[string]string, term bool) {
	if out := BuildDetailsOutput(refs, term); out != "" {
		fmt.Fprintf(w, "\n%s\n", out)
	}
}

func ANSIHyperlink(url, text string) string {
	// create an escape sequence using the OSC 8 format: https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}

type ErrorWithBuildRef struct {
	Ref string
	Err error
}

func (e *ErrorWithBuildRef) Error() string {
	return e.Err.Error()
}

func (e *ErrorWithBuildRef) Unwrap() error {
	return e.Err
}

func (e *ErrorWithBuildRef) Print(w io.Writer) error {
	var term bool
	if _, err := console.ConsoleFromFile(os.Stderr); err == nil {
		term = true
	}
	fmt.Fprintf(w, "\n%s\n", BuildDetailsOutput(map[string]string{"default": e.Ref}, term))
	return nil
}
