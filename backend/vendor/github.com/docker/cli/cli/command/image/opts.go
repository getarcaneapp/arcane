package image

import (
	"os"

	"github.com/spf13/pflag"
)

// addPlatformFlag adds "--platform" to a set of flags for API version 1.32 and
// later, using the value of "DOCKER_DEFAULT_PLATFORM" (if set) as a default.
//
// It should not be used for new uses, which may have a different API version
// requirement.
func addPlatformFlag(flags *pflag.FlagSet, target *string) {
	flags.StringVar(target, "platform", os.Getenv("DOCKER_DEFAULT_PLATFORM"), "Set platform if server is multi-platform capable")
	_ = flags.SetAnnotation("platform", "version", []string{"1.32"})
}
