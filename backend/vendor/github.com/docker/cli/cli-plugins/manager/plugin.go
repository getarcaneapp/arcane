package manager

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/internal/lazyregexp"
	"github.com/spf13/cobra"
)

var pluginNameRe = lazyregexp.New("^[a-z][a-z0-9]*$")

// Plugin represents a potential plugin with all it's metadata.
type Plugin struct {
	metadata.Metadata

	Name string `json:",omitempty"`
	Path string `json:",omitempty"`

	// Err is non-nil if the plugin failed one of the candidate tests.
	Err error `json:",omitempty"`

	// ShadowedPaths contains the paths of any other plugins which this plugin takes precedence over.
	ShadowedPaths []string `json:",omitempty"`
}

// MarshalJSON implements [json.Marshaler] to handle marshaling the
// [Plugin.Err] field (Go doesn't marshal errors by default).
func (p *Plugin) MarshalJSON() ([]byte, error) {
	type Alias Plugin // avoid recursion

	cp := *p // shallow copy to avoid mutating original

	if cp.Err != nil {
		if _, ok := cp.Err.(encoding.TextMarshaler); !ok {
			cp.Err = &pluginError{cp.Err}
		}
	}

	return json.Marshal((*Alias)(&cp))
}

// pluginCandidate represents a possible plugin candidate, for mocking purposes.
type pluginCandidate interface {
	Path() string
	Metadata() ([]byte, error)
}

// newPlugin determines if the given candidate is valid and returns a
// Plugin.  If the candidate fails one of the tests then `Plugin.Err`
// is set, and is always a `pluginError`, but the `Plugin` is still
// returned with no error. An error is only returned due to a
// non-recoverable error.
func newPlugin(c pluginCandidate, cmds []*cobra.Command) (Plugin, error) {
	path := c.Path()
	if path == "" {
		return Plugin{}, errors.New("plugin candidate path cannot be empty")
	}

	// The candidate listing process should have skipped anything
	// which would fail here, so there are all real errors.
	fullname := filepath.Base(path)
	if fullname == "." {
		return Plugin{}, fmt.Errorf("unable to determine basename of plugin candidate %q", path)
	}
	var err error
	if fullname, err = trimExeSuffix(fullname); err != nil {
		return Plugin{}, fmt.Errorf("plugin candidate %q: %w", path, err)
	}
	if !strings.HasPrefix(fullname, metadata.NamePrefix) {
		return Plugin{}, fmt.Errorf("plugin candidate %q: does not have %q prefix", path, metadata.NamePrefix)
	}

	p := Plugin{
		Name: strings.TrimPrefix(fullname, metadata.NamePrefix),
		Path: path,
	}

	// Now apply the candidate tests, so these update p.Err.
	if !pluginNameRe.MatchString(p.Name) {
		p.Err = newPluginError("plugin candidate %q did not match %q", p.Name, pluginNameRe.String())
		return p, nil
	}

	for _, cmd := range cmds {
		// Ignore conflicts with commands which are
		// just plugin stubs (i.e. from a previous
		// call to AddPluginCommandStubs).
		if IsPluginCommand(cmd) {
			continue
		}
		if cmd.Name() == p.Name {
			p.Err = newPluginError("plugin %q duplicates builtin command", p.Name)
			return p, nil
		}
		if cmd.HasAlias(p.Name) {
			p.Err = newPluginError("plugin %q duplicates an alias of builtin command %q", p.Name, cmd.Name())
			return p, nil
		}
	}

	// We are supposed to check for relevant execute permissions here. Instead we rely on an attempt to execute.
	meta, err := c.Metadata()
	if err != nil {
		p.Err = wrapAsPluginError(err, "failed to fetch metadata")
		return p, nil
	}

	if err := json.Unmarshal(meta, &p.Metadata); err != nil {
		p.Err = wrapAsPluginError(err, "invalid metadata")
		return p, nil
	}
	if err := validateSchemaVersion(p.Metadata.SchemaVersion); err != nil {
		p.Err = &pluginError{cause: err}
		return p, nil
	}
	if p.Metadata.Vendor == "" {
		p.Err = newPluginError("plugin metadata does not define a vendor")
		return p, nil
	}
	return p, nil
}

// validateSchemaVersion validates if the plugin's schemaVersion is supported.
//
// The current schema-version is "0.1.0", but we don't want to break compatibility
// until v2.0.0 of the schema version. Check for the major version to be < 2.0.0.
//
// Note that CLI versions before 28.4.1 may not support these versions as they were
// hard-coded to only accept "0.1.0".
func validateSchemaVersion(version string) error {
	if version == "0.1.0" {
		return nil
	}
	if version == "" {
		return errors.New("plugin SchemaVersion version cannot be empty")
	}
	major, _, ok := strings.Cut(version, ".")
	majorVersion, err := strconv.Atoi(major)
	if !ok || err != nil {
		return fmt.Errorf("plugin SchemaVersion %q has wrong format: must be <major>.<minor>.<patch>", version)
	}
	if majorVersion > 1 {
		return fmt.Errorf("plugin SchemaVersion %q is not supported: must be lower than 2.0.0", version)
	}
	return nil
}

// RunHook executes the plugin's hooks command
// and returns its unprocessed output.
func (p *Plugin) RunHook(ctx context.Context, hookData HookPluginData) ([]byte, error) {
	hDataBytes, err := json.Marshal(hookData)
	if err != nil {
		return nil, wrapAsPluginError(err, "failed to marshall hook data")
	}

	pCmd := exec.CommandContext(ctx, p.Path, p.Name, metadata.HookSubcommandName, string(hDataBytes)) // #nosec G204 -- ignore "Subprocess launched with a potential tainted input or cmd arguments"
	pCmd.Env = os.Environ()
	pCmd.Env = append(pCmd.Env, metadata.ReexecEnvvar+"="+os.Args[0])
	hookCmdOutput, err := pCmd.Output()
	if err != nil {
		return nil, wrapAsPluginError(err, "failed to execute plugin hook subcommand")
	}

	return hookCmdOutput, nil
}
