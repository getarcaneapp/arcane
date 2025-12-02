// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package formatter

import "strings"

// Common header constants
const (
	CreatedSinceHeader = "CREATED"
	CreatedAtHeader    = "CREATED AT"
	SizeHeader         = "SIZE"
	LabelsHeader       = "LABELS"
	NameHeader         = "NAME"
	DescriptionHeader  = "DESCRIPTION"
	DriverHeader       = "DRIVER"
	ScopeHeader        = "SCOPE"
	StateHeader        = "STATE"
	StatusHeader       = "STATUS"
	PortsHeader        = "PORTS"
	ImageHeader        = "IMAGE"
	ErrorHeader        = "ERROR"
	ContainerIDHeader  = "CONTAINER ID"
)

// SubContext defines what Context implementation should provide
type SubContext interface {
	FullHeader() any
}

// SubHeaderContext is a map destined to formatter header (table format)
type SubHeaderContext map[string]string

// Label returns the header label for the specified string
func (SubHeaderContext) Label(name string) string {
	n := strings.Split(name, ".")
	r := strings.NewReplacer("-", " ", "_", " ")
	h := r.Replace(n[len(n)-1])

	return h
}

// HeaderContext provides the subContext interface for managing headers
type HeaderContext struct {
	Header any
}

// FullHeader returns the header as an interface
func (c *HeaderContext) FullHeader() any {
	return c.Header
}
