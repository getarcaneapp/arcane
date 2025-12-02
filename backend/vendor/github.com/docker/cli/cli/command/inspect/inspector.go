// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package inspect

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/template"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/templates"
	"github.com/sirupsen/logrus"
)

// Inspector defines an interface to implement to process elements
type Inspector interface {
	// Inspect writes the raw element in JSON format.
	Inspect(typedElement any, rawElement []byte) error
	// Flush writes the result of inspecting all elements into the output stream.
	Flush() error
}

// TemplateInspector uses a text template to inspect elements.
type TemplateInspector struct {
	out    io.Writer
	buffer *bytes.Buffer
	tmpl   *template.Template
}

// NewTemplateInspector creates a new inspector with a template.
func NewTemplateInspector(out io.Writer, tmpl *template.Template) *TemplateInspector {
	if out == nil {
		out = io.Discard
	}
	return &TemplateInspector{
		out:    out,
		buffer: new(bytes.Buffer),
		tmpl:   tmpl,
	}
}

// NewTemplateInspectorFromString creates a new TemplateInspector from a string
// which is compiled into a template.
func NewTemplateInspectorFromString(out io.Writer, tmplStr string) (Inspector, error) {
	if out == nil {
		return nil, errors.New("no output stream")
	}
	if tmplStr == "" {
		return NewIndentedInspector(out), nil
	}

	if tmplStr == "json" {
		return NewJSONInspector(out), nil
	}

	tmpl, err := templates.Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("template parsing error: %w", err)
	}
	return NewTemplateInspector(out, tmpl), nil
}

// GetRefFunc is a function which used by Inspect to fetch an object from a
// reference
type GetRefFunc func(ref string) (any, []byte, error)

// Inspect fetches objects by reference using GetRefFunc and writes the json
// representation to the output writer.
func Inspect(out io.Writer, references []string, tmplStr string, getRef GetRefFunc) error {
	if out == nil {
		return errors.New("no output stream")
	}
	inspector, err := NewTemplateInspectorFromString(out, tmplStr)
	if err != nil {
		return cli.StatusError{StatusCode: 64, Status: err.Error()}
	}

	var errs []error
	for _, ref := range references {
		element, raw, err := getRef(ref)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if err := inspector.Inspect(element, raw); err != nil {
			errs = append(errs, err)
		}
	}

	if err := inspector.Flush(); err != nil {
		logrus.Error(err)
	}

	if err := errors.Join(errs...); err != nil {
		return cli.StatusError{
			StatusCode: 1,
			Status:     err.Error(),
		}
	}
	return nil
}

// Inspect executes the inspect template.
// It decodes the raw element into a map if the initial execution fails.
// This allows docker cli to parse inspect structs injected with Swarm fields.
func (i *TemplateInspector) Inspect(typedElement any, rawElement []byte) error {
	buffer := new(bytes.Buffer)
	if err := i.tmpl.Execute(buffer, typedElement); err != nil {
		if rawElement == nil {
			return fmt.Errorf("template parsing error: %w", err)
		}
		return i.tryRawInspectFallback(rawElement)
	}
	i.buffer.Write(buffer.Bytes())
	i.buffer.WriteByte('\n')
	return nil
}

// tryRawInspectFallback executes the inspect template with a raw interface.
// This allows docker cli to parse inspect structs injected with Swarm fields.
func (i *TemplateInspector) tryRawInspectFallback(rawElement []byte) error {
	var raw any
	buffer := new(bytes.Buffer)
	rdr := bytes.NewReader(rawElement)
	dec := json.NewDecoder(rdr)
	dec.UseNumber()

	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("unable to read inspect data: %w", err)
	}

	tmplMissingKey := i.tmpl.Option("missingkey=error")
	if err := tmplMissingKey.Execute(buffer, raw); err != nil {
		return fmt.Errorf("template parsing error: %w", err)
	}

	i.buffer.Write(buffer.Bytes())
	i.buffer.WriteByte('\n')
	return nil
}

// Flush writes the result of inspecting all elements into the output stream.
func (i *TemplateInspector) Flush() error {
	if i.buffer.Len() == 0 {
		_, err := io.WriteString(i.out, "\n")
		return err
	}
	_, err := io.Copy(i.out, i.buffer)
	return err
}

// NewIndentedInspector generates a new inspector with an indented representation
// of elements.
func NewIndentedInspector(out io.Writer) Inspector {
	if out == nil {
		out = io.Discard
	}
	return &jsonInspector{
		out: out,
		raw: func(dst *bytes.Buffer, src []byte) error {
			return json.Indent(dst, src, "", "    ")
		},
		el: func(v any) ([]byte, error) {
			return json.MarshalIndent(v, "", "    ")
		},
	}
}

// NewJSONInspector generates a new inspector with a compact representation
// of elements.
func NewJSONInspector(out io.Writer) Inspector {
	if out == nil {
		out = io.Discard
	}
	return &jsonInspector{
		out: out,
		raw: json.Compact,
		el:  json.Marshal,
	}
}

type jsonInspector struct {
	out         io.Writer
	elements    []any
	rawElements [][]byte
	raw         func(dst *bytes.Buffer, src []byte) error
	el          func(v any) ([]byte, error)
}

func (e *jsonInspector) Inspect(typedElement any, rawElement []byte) error {
	if rawElement != nil {
		e.rawElements = append(e.rawElements, rawElement)
	} else {
		e.elements = append(e.elements, typedElement)
	}
	return nil
}

func (e *jsonInspector) Flush() error {
	if len(e.elements) == 0 && len(e.rawElements) == 0 {
		_, err := io.WriteString(e.out, "[]\n")
		return err
	}

	var buffer io.Reader
	if len(e.rawElements) > 0 {
		bytesBuffer := new(bytes.Buffer)
		bytesBuffer.WriteString("[")
		for idx, r := range e.rawElements {
			bytesBuffer.Write(r)
			if idx < len(e.rawElements)-1 {
				bytesBuffer.WriteString(",")
			}
		}
		bytesBuffer.WriteString("]")
		output := new(bytes.Buffer)
		if err := e.raw(output, bytesBuffer.Bytes()); err != nil {
			return err
		}
		buffer = output
	} else {
		b, err := e.el(e.elements)
		if err != nil {
			return err
		}
		buffer = bytes.NewReader(b)
	}

	if _, err := io.Copy(e.out, buffer); err != nil {
		return err
	}
	_, err := io.WriteString(e.out, "\n")
	return err
}
