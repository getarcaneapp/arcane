package buildflags

import (
	"encoding/csv"
	"encoding/json"
	"maps"
	"regexp"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

type Exports []*ExportEntry

func (e Exports) Merge(other Exports) Exports {
	if other == nil {
		e.Normalize()
		return e
	} else if e == nil {
		other.Normalize()
		return other
	}

	return append(e, other...).Normalize()
}

func (e Exports) Normalize() Exports {
	if len(e) == 0 {
		return nil
	}
	return removeDupes(e)
}

type ExportEntry struct {
	Type        string            `json:"type"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Destination string            `json:"dest,omitempty"`
}

func (e *ExportEntry) Equal(other *ExportEntry) bool {
	if e.Type != other.Type || e.Destination != other.Destination {
		return false
	}
	return maps.Equal(e.Attrs, other.Attrs)
}

func (e *ExportEntry) String() string {
	var b csvBuilder
	if e.Type != "" {
		b.Write("type", e.Type)
	}
	if e.Destination != "" {
		b.Write("dest", e.Destination)
	}
	if len(e.Attrs) > 0 {
		b.WriteAttributes(e.Attrs)
	}
	return b.String()
}

func (e *ExportEntry) MarshalJSON() ([]byte, error) {
	m := maps.Clone(e.Attrs)
	if m == nil {
		m = map[string]string{}
	}
	m["type"] = e.Type
	if e.Destination != "" {
		m["dest"] = e.Destination
	}
	return json.Marshal(m)
}

func (e *ExportEntry) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	e.Type = m["type"]
	delete(m, "type")

	e.Destination = m["dest"]
	delete(m, "dest")

	e.Attrs = m
	return e.validate()
}

func (e *ExportEntry) UnmarshalText(text []byte) error {
	s := string(text)
	fields, err := csvvalue.Fields(s, nil)
	if err != nil {
		return err
	}

	// Clear the target entry.
	e.Type = ""
	e.Attrs = map[string]string{}
	e.Destination = ""

	if len(fields) == 1 && fields[0] == s && !strings.HasPrefix(s, "type=") {
		if s != "-" {
			e.Type = client.ExporterLocal
			e.Destination = s
			return nil
		}

		e.Type = client.ExporterTar
		e.Destination = s
	}

	if e.Type == "" {
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				return errors.Errorf("invalid value %s", field)
			}
			key := strings.TrimSpace(strings.ToLower(parts[0]))
			value := parts[1]
			switch key {
			case "type":
				e.Type = value
			case "dest":
				e.Destination = value
			default:
				e.Attrs[key] = value
			}
		}
	}
	return e.validate()
}

func (e *ExportEntry) validate() error {
	if e.Type == "" {
		return errors.Errorf("type is required for output")
	}
	return nil
}

func ParseExports(inp []string) ([]*ExportEntry, error) {
	if len(inp) == 0 {
		return nil, nil
	}

	exports := make(Exports, 0, len(inp))
	for _, s := range inp {
		if s == "" {
			continue
		}

		var out ExportEntry
		if err := out.UnmarshalText([]byte(s)); err != nil {
			return nil, err
		}
		exports = append(exports, &out)
	}
	return exports, nil
}

func ParseAnnotations(inp []string) (map[exptypes.AnnotationKey]string, error) {
	// TODO: use buildkit's annotation parser once it supports setting custom prefix and ":" separator

	// type followed by optional platform specifier in square brackets
	annotationTypeRegexp := regexp.MustCompile(`^([a-z-]+)(?:\[([A-Za-z0-9_/-]+)\])?$`)

	annotations := make(map[exptypes.AnnotationKey]string)
	for _, inp := range inp {
		if inp == "" {
			continue
		}

		k, v, ok := strings.Cut(inp, "=")
		if !ok {
			return nil, errors.Errorf("invalid annotation %q, expected key=value", inp)
		}

		types, key, ok := strings.Cut(k, ":")
		if !ok {
			// no types specified, swap Cut outputs
			key = types

			ak := exptypes.AnnotationKey{Key: key}
			annotations[ak] = v
			continue
		}

		for typeAndPlatform := range strings.SplitSeq(types, ",") {
			groups := annotationTypeRegexp.FindStringSubmatch(typeAndPlatform)
			if groups == nil {
				return nil, errors.Errorf(
					"invalid annotation type %q, expected type and optional platform in square brackets",
					typeAndPlatform)
			}

			typ, platform := groups[1], groups[2]

			switch typ {
			case "":
			case exptypes.AnnotationIndex,
				exptypes.AnnotationIndexDescriptor,
				exptypes.AnnotationManifest,
				exptypes.AnnotationManifestDescriptor:
			default:
				return nil, errors.Errorf("unknown annotation type %q", typ)
			}

			var ociPlatform *ocispecs.Platform
			if platform != "" {
				p, err := platforms.Parse(platform)
				if err != nil {
					return nil, errors.Wrapf(err, "invalid platform %q", platform)
				}
				ociPlatform = &p
			}

			ak := exptypes.AnnotationKey{
				Type:     typ,
				Platform: ociPlatform,
				Key:      key,
			}
			annotations[ak] = v
		}
	}
	return annotations, nil
}

type csvBuilder struct {
	sb strings.Builder
}

func (w *csvBuilder) Write(key, value string) {
	if w.sb.Len() > 0 {
		w.sb.WriteByte(',')
	}

	pair := key + "=" + value
	if strings.ContainsRune(pair, ',') || strings.ContainsRune(pair, '"') {
		var attr strings.Builder
		writer := csv.NewWriter(&attr)
		writer.Write([]string{pair})
		writer.Flush()
		// Strips the extra newline added by the csv writer
		pair = strings.TrimSpace(attr.String())
	}

	w.sb.WriteString(pair)
}

func (w *csvBuilder) WriteAttributes(attrs map[string]string) {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		w.Write(key, attrs[key])
	}
}

func (w *csvBuilder) String() string {
	return w.sb.String()
}
