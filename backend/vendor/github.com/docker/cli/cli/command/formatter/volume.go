package formatter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-units"
)

const (
	defaultVolumeQuietFormat = "{{.Name}}"
	defaultVolumeTableFormat = "table {{.Driver}}\t{{.Name}}"

	idHeader           = "ID"
	volumeNameHeader   = "VOLUME NAME"
	mountpointHeader   = "MOUNTPOINT"
	linksHeader        = "LINKS"
	groupHeader        = "GROUP"
	availabilityHeader = "AVAILABILITY"
	statusHeader       = "STATUS"
)

// NewVolumeFormat returns a format for use with a volume Context
func NewVolumeFormat(source string, quiet bool) Format {
	switch source {
	case TableFormatKey:
		if quiet {
			return defaultVolumeQuietFormat
		}
		return defaultVolumeTableFormat
	case RawFormatKey:
		if quiet {
			return `name: {{.Name}}`
		}
		return `name: {{.Name}}\ndriver: {{.Driver}}\n`
	}
	return Format(source)
}

// VolumeWrite writes formatted volumes using the Context
func VolumeWrite(ctx Context, volumes []*volume.Volume) error {
	render := func(format func(subContext SubContext) error) error {
		for _, vol := range volumes {
			if err := format(&volumeContext{v: *vol}); err != nil {
				return err
			}
		}
		return nil
	}
	return ctx.Write(newVolumeContext(), render)
}

type volumeContext struct {
	HeaderContext
	v volume.Volume
}

func newVolumeContext() *volumeContext {
	volumeCtx := volumeContext{}
	volumeCtx.Header = SubHeaderContext{
		"ID":           idHeader,
		"Name":         volumeNameHeader,
		"Group":        groupHeader,
		"Driver":       DriverHeader,
		"Scope":        ScopeHeader,
		"Availability": availabilityHeader,
		"Mountpoint":   mountpointHeader,
		"Labels":       LabelsHeader,
		"Links":        linksHeader,
		"Size":         SizeHeader,
		"Status":       statusHeader,
	}
	return &volumeCtx
}

func (c *volumeContext) MarshalJSON() ([]byte, error) {
	return MarshalJSON(c)
}

func (c *volumeContext) Name() string {
	return c.v.Name
}

func (c *volumeContext) Driver() string {
	return c.v.Driver
}

func (c *volumeContext) Scope() string {
	return c.v.Scope
}

func (c *volumeContext) Mountpoint() string {
	return c.v.Mountpoint
}

func (c *volumeContext) Labels() string {
	if c.v.Labels == nil {
		return ""
	}

	joinLabels := make([]string, 0, len(c.v.Labels))
	for k, v := range c.v.Labels {
		joinLabels = append(joinLabels, k+"="+v)
	}
	return strings.Join(joinLabels, ",")
}

func (c *volumeContext) Label(name string) string {
	if c.v.Labels == nil {
		return ""
	}
	return c.v.Labels[name]
}

func (c *volumeContext) Links() string {
	if c.v.UsageData == nil {
		return "N/A"
	}
	return strconv.FormatInt(c.v.UsageData.RefCount, 10)
}

func (c *volumeContext) Size() string {
	if c.v.UsageData == nil {
		return "N/A"
	}
	return units.HumanSize(float64(c.v.UsageData.Size))
}

func (c *volumeContext) Group() string {
	if c.v.ClusterVolume == nil {
		return "N/A"
	}

	return c.v.ClusterVolume.Spec.Group
}

func (c *volumeContext) Availability() string {
	if c.v.ClusterVolume == nil {
		return "N/A"
	}

	return string(c.v.ClusterVolume.Spec.Availability)
}

func (c *volumeContext) Status() string {
	if c.v.ClusterVolume == nil {
		return "N/A"
	}

	if c.v.ClusterVolume.Info == nil || c.v.ClusterVolume.Info.VolumeID == "" {
		return "pending creation"
	}

	l := len(c.v.ClusterVolume.PublishStatus)
	switch l {
	case 0:
		return "created"
	case 1:
		return "in use (1 node)"
	default:
		return fmt.Sprintf("in use (%d nodes)", l)
	}
}
