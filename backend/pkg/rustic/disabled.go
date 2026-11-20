package rustic

import (
	"errors"
	"os"
	"strings"
)

// EnvDisabled switches the Rustic-backed backup engine off. Config loads it into
// Config.BackupsDisabled, and every backup and recovery entry point returns ErrDisabled instead of
// inspecting, pulling or running DefaultImage, so a host that never backs anything up does not pay
// for the engine: no image pull, no helper container, no repository volume.
const EnvDisabled = "ARCANE_BACKUPS_DISABLED"

// ErrDisabled is returned by every Rustic entry point while the engine is disabled.
var ErrDisabled = errors.New("backup engine is disabled: " + EnvDisabled + " is set")

// DisabledFromEnv reports whether the environment switches the engine off. It is the one place the
// variable is parsed, so config-driven callers and env-only callers (the recovery helper) agree on
// which values disable the engine.
func DisabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvDisabled))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
