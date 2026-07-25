package swarm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"emperror.dev/errors"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
)

const (
	resolveImageAlways      = "always"
	resolveImageChanged     = "changed"
	resolveImageNever       = "never"
	resourceTypeLabel       = "app.getarcane.swarm.resource.type"
	resourceNameLabel       = "app.getarcane.swarm.resource.name"
	resourceHashLabel       = "app.getarcane.swarm.resource.hash"
	legacyResourceTypeLabel = "io.getarcane.swarm.resource.type"
	resourceNameMaxHash     = 12
)

var stackNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ErrInvalidStack identifies invalid stack source or conversion input.
var ErrInvalidStack = errors.Sentinel("invalid swarm stack")

type namespace struct {
	name string
}

func (n namespace) Scope(key string) string {
	return n.name + "_" + key
}

func (n namespace) Resolve(key, configuredName string) string {
	if name := strings.TrimSpace(configuredName); name != "" {
		return name
	}
	return n.Scope(key)
}

func mergeLabelsInternal(primary map[string]string, secondary map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, primary)
	maps.Copy(out, secondary)
	return out
}

func validateStackNameInternal(stackName string) error {
	switch {
	case stackName == "":
		return errors.New("stack name is required")
	case !stackNamePattern.MatchString(stackName):
		return errors.Errorf("invalid stack name %q: use lowercase letters, numbers, hyphens, and underscores, starting with a letter or number", stackName)
	default:
		return nil
	}
}

func invalidStackErrorInternal(err error) error {
	if err == nil || errors.Is(err, ErrInvalidStack) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrInvalidStack, err)
}

func hashManagedResourceInternal(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashManagedFileResourceInternal(config composegotypes.FileObjectConfig, data []byte) string {
	if config.Driver == "" && config.TemplateDriver == "" && len(config.DriverOpts) == 0 {
		return hashManagedResourceInternal(data)
	}

	hasher := sha256.New()
	_, _ = hasher.Write(data)
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(config.Driver))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(config.TemplateDriver))
	for _, key := range slices.Sorted(maps.Keys(config.DriverOpts)) {
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(key))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(config.DriverOpts[key]))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func managedResourceNameInternal(logicalName, hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) > resourceNameMaxHash {
		hash = hash[:resourceNameMaxHash]
	}
	if hash == "" {
		return logicalName
	}
	return fmt.Sprintf("%s_%s", logicalName, hash)
}
