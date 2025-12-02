// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package formatter

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	defaultContainerTableFormat = "table {{.ID}}\t{{.Image}}\t{{.Command}}\t{{.RunningFor}}\t{{.Status}}\t{{.Ports}}\t{{.Names}}"

	namesHeader      = "NAMES"
	commandHeader    = "COMMAND"
	runningForHeader = "CREATED"
	mountsHeader     = "MOUNTS"
	localVolumes     = "LOCAL VOLUMES"
	networksHeader   = "NETWORKS"
	platformHeader   = "PLATFORM"
)

// Platform wraps a [ocispec.Platform] to implement the stringer interface.
type Platform struct {
	ocispec.Platform
}

func (p Platform) String() string {
	return platforms.FormatAll(p.Platform)
}

// NewContainerFormat returns a Format for rendering using a Context
func NewContainerFormat(source string, quiet bool, size bool) Format {
	switch source {
	case TableFormatKey, "": // table formatting is the default if none is set.
		if quiet {
			return DefaultQuietFormat
		}
		format := defaultContainerTableFormat
		if size {
			format += `\t{{.Size}}`
		}
		return Format(format)
	case RawFormatKey:
		if quiet {
			return `container_id: {{.ID}}`
		}
		format := `container_id: {{.ID}}
image: {{.Image}}
command: {{.Command}}
created_at: {{.CreatedAt}}
state: {{- pad .State 1 0}}
status: {{- pad .Status 1 0}}
names: {{.Names}}
labels: {{- pad .Labels 1 0}}
ports: {{- pad .Ports 1 0}}
`
		if size {
			format += `size: {{.Size}}\n`
		}
		return Format(format)
	default: // custom format
		if quiet {
			return DefaultQuietFormat
		}
		return Format(source)
	}
}

// ContainerWrite renders the context for a list of containers
func ContainerWrite(ctx Context, containers []container.Summary) error {
	return ctx.Write(NewContainerContext(), func(format func(subContext SubContext) error) error {
		for _, ctr := range containers {
			if err := format(&ContainerContext{trunc: ctx.Trunc, c: ctr}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ContainerContext is a struct used for rendering a list of containers in a Go template.
type ContainerContext struct {
	HeaderContext
	trunc bool
	c     container.Summary

	// FieldsUsed is used in the pre-processing step to detect which fields are
	// used in the template. It's currently only used to detect use of the .Size
	// field which (if used) automatically sets the '--size' option when making
	// the API call.
	FieldsUsed map[string]any
}

// NewContainerContext creates a new context for rendering containers
func NewContainerContext() *ContainerContext {
	containerCtx := ContainerContext{}
	containerCtx.Header = SubHeaderContext{
		"ID":           ContainerIDHeader,
		"Names":        namesHeader,
		"Image":        ImageHeader,
		"Command":      commandHeader,
		"CreatedAt":    CreatedAtHeader,
		"RunningFor":   runningForHeader,
		"Ports":        PortsHeader,
		"State":        StateHeader,
		"Status":       StatusHeader,
		"Size":         SizeHeader,
		"Labels":       LabelsHeader,
		"Mounts":       mountsHeader,
		"LocalVolumes": localVolumes,
		"Networks":     networksHeader,
		"Platform":     platformHeader,
	}
	return &containerCtx
}

// MarshalJSON makes ContainerContext implement json.Marshaler
func (c *ContainerContext) MarshalJSON() ([]byte, error) {
	return MarshalJSON(c)
}

// ID returns the container's ID as a string. Depending on the `--no-trunc`
// option being set, the full or truncated ID is returned.
func (c *ContainerContext) ID() string {
	if c.trunc {
		return TruncateID(c.c.ID)
	}
	return c.c.ID
}

// Names returns a comma-separated string of the container's names, with their
// slash (/) prefix stripped. Additional names for the container (related to the
// legacy `--link` feature) are omitted.
func (c *ContainerContext) Names() string {
	names := StripNamePrefix(c.c.Names)
	if c.trunc {
		for _, name := range names {
			if len(strings.Split(name, "/")) == 1 {
				names = []string{name}
				break
			}
		}
	}
	return strings.Join(names, ",")
}

// StripNamePrefix removes prefix from string, typically container names as returned by `ContainersList` API
func StripNamePrefix(ss []string) []string {
	sss := make([]string, len(ss))
	for i, s := range ss {
		sss[i] = s[1:]
	}
	return sss
}

// Image returns the container's image reference. If the trunc option is set,
// the image's registry digest can be included.
func (c *ContainerContext) Image() string {
	if c.c.Image == "" {
		return "<no image>"
	}
	if c.trunc {
		if trunc := TruncateID(c.c.ImageID); trunc == TruncateID(c.c.Image) {
			return trunc
		}
		// truncate digest if no-trunc option was not selected
		ref, err := reference.ParseNormalizedNamed(c.c.Image)
		if err == nil {
			if nt, ok := ref.(reference.NamedTagged); ok {
				// case for when a tag is provided
				if namedTagged, err := reference.WithTag(reference.TrimNamed(nt), nt.Tag()); err == nil {
					return reference.FamiliarString(namedTagged)
				}
			} else {
				// case for when a tag is not provided
				named := reference.TrimNamed(ref)
				return reference.FamiliarString(named)
			}
		}
	}

	return c.c.Image
}

// Command returns's the container's command. If the trunc option is set, the
// returned command is truncated (ellipsized).
func (c *ContainerContext) Command() string {
	command := c.c.Command
	if c.trunc {
		command = Ellipsis(command, 20)
	}
	return strconv.Quote(command)
}

// CreatedAt returns the formatted string representing the container's creation date/time.
// The format may include nanoseconds if present.
// e.g. "2006-01-02 15:04:05.999999999 -0700 MST" or "2006-01-02 15:04:05 -0700 MST"
func (c *ContainerContext) CreatedAt() string {
	return time.Unix(c.c.Created, 0).String()
}

// RunningFor returns a human-readable representation of the duration for which
// the container has been running.
//
// Note that this duration is calculated on the client, and as such is influenced
// by clock skew between the client and the daemon.
func (c *ContainerContext) RunningFor() string {
	createdAt := time.Unix(c.c.Created, 0)
	return units.HumanDuration(time.Now().UTC().Sub(createdAt)) + " ago"
}

// Platform returns a human-readable representation of the container's
// platform if it is available.
func (c *ContainerContext) Platform() *Platform {
	p := c.c.ImageManifestDescriptor
	if p == nil || p.Platform == nil {
		return nil
	}
	return &Platform{*p.Platform}
}

// Ports returns a comma-separated string representing open ports of the container
// e.g. "0.0.0.0:80->9090/tcp, 9988/tcp"
// it's used by command 'docker ps'
// Both published and exposed ports are included.
func (c *ContainerContext) Ports() string {
	return DisplayablePorts(c.c.Ports)
}

// State returns the container's current state (e.g. "running" or "paused").
// Refer to [container.ContainerState] for possible states.
func (c *ContainerContext) State() string {
	return c.c.State
}

// Status returns the container's status in a human readable form (for example,
// "Up 24 hours" or "Exited (0) 8 days ago")
func (c *ContainerContext) Status() string {
	return c.c.Status
}

// Size returns the container's size and virtual size (e.g. "2B (virtual 21.5MB)")
func (c *ContainerContext) Size() string {
	if c.FieldsUsed == nil {
		c.FieldsUsed = map[string]any{}
	}
	c.FieldsUsed["Size"] = struct{}{}
	srw := units.HumanSizeWithPrecision(float64(c.c.SizeRw), 3)
	sv := units.HumanSizeWithPrecision(float64(c.c.SizeRootFs), 3)

	sf := srw
	if c.c.SizeRootFs > 0 {
		sf = fmt.Sprintf("%s (virtual %s)", srw, sv)
	}
	return sf
}

// Labels returns a comma-separated string of labels present on the container.
func (c *ContainerContext) Labels() string {
	if c.c.Labels == nil {
		return ""
	}

	joinLabels := make([]string, 0, len(c.c.Labels))
	for k, v := range c.c.Labels {
		joinLabels = append(joinLabels, k+"="+v)
	}
	sort.Strings(joinLabels)
	return strings.Join(joinLabels, ",")
}

// Label returns the value of the label with the given name or an empty string
// if the given label does not exist.
func (c *ContainerContext) Label(name string) string {
	if c.c.Labels == nil {
		return ""
	}
	return c.c.Labels[name]
}

// Mounts returns a comma-separated string of mount names present on the container.
// If the trunc option is set, names can be truncated (ellipsized).
func (c *ContainerContext) Mounts() string {
	var name string
	mounts := make([]string, 0, len(c.c.Mounts))
	for _, m := range c.c.Mounts {
		if m.Name == "" {
			name = m.Source
		} else {
			name = m.Name
		}
		if c.trunc {
			name = Ellipsis(name, 15)
		}
		mounts = append(mounts, name)
	}
	return strings.Join(mounts, ",")
}

// LocalVolumes returns the number of volumes using the "local" volume driver.
func (c *ContainerContext) LocalVolumes() string {
	count := 0
	for _, m := range c.c.Mounts {
		if m.Driver == "local" {
			count++
		}
	}

	return strconv.Itoa(count)
}

// Networks returns a comma-separated string of networks that the container is
// attached to.
func (c *ContainerContext) Networks() string {
	if c.c.NetworkSettings == nil {
		return ""
	}

	networks := make([]string, 0, len(c.c.NetworkSettings.Networks))
	for k := range c.c.NetworkSettings.Networks {
		networks = append(networks, k)
	}

	return strings.Join(networks, ",")
}

// DisplayablePorts returns formatted string representing open ports of container
// e.g. "0.0.0.0:80->9090/tcp, 9988/tcp"
// it's used by command 'docker ps'
func DisplayablePorts(ports []container.Port) string {
	type portGroup struct {
		first uint16
		last  uint16
	}
	groupMap := make(map[string]*portGroup)
	var result []string //nolint:prealloc
	var hostMappings []string
	var groupMapKeys []string
	sort.Slice(ports, func(i, j int) bool {
		return comparePorts(ports[i], ports[j])
	})

	for _, port := range ports {
		current := port.PrivatePort
		portKey := port.Type
		if port.IP != "" {
			if port.PublicPort != current {
				hAddrPort := net.JoinHostPort(port.IP, strconv.Itoa(int(port.PublicPort)))
				hostMappings = append(hostMappings, fmt.Sprintf("%s->%d/%s", hAddrPort, port.PrivatePort, port.Type))
				continue
			}
			portKey = port.IP + "/" + port.Type
		}
		group := groupMap[portKey]

		if group == nil {
			groupMap[portKey] = &portGroup{first: current, last: current}
			// record order that groupMap keys are created
			groupMapKeys = append(groupMapKeys, portKey)
			continue
		}
		if current == (group.last + 1) {
			group.last = current
			continue
		}

		result = append(result, formGroup(portKey, group.first, group.last))
		groupMap[portKey] = &portGroup{first: current, last: current}
	}
	for _, portKey := range groupMapKeys {
		g := groupMap[portKey]
		result = append(result, formGroup(portKey, g.first, g.last))
	}
	result = append(result, hostMappings...)
	return strings.Join(result, ", ")
}

func formGroup(key string, start, last uint16) string {
	parts := strings.Split(key, "/")
	groupType := parts[0]
	var ip string
	if len(parts) > 1 {
		ip = parts[0]
		groupType = parts[1]
	}
	group := strconv.Itoa(int(start))
	if start != last {
		group = fmt.Sprintf("%s-%d", group, last)
	}
	if ip != "" {
		group = fmt.Sprintf("%s->%s", net.JoinHostPort(ip, group), group)
	}
	return group + "/" + groupType
}

func comparePorts(i, j container.Port) bool {
	if i.PrivatePort != j.PrivatePort {
		return i.PrivatePort < j.PrivatePort
	}

	if i.IP != j.IP {
		return i.IP < j.IP
	}

	if i.PublicPort != j.PublicPort {
		return i.PublicPort < j.PublicPort
	}

	return i.Type < j.Type
}
