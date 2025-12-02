package container

import (
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/docker/api/types/container"
)

const (
	defaultDiffTableFormat = "table {{.Type}}\t{{.Path}}"

	changeTypeHeader = "CHANGE TYPE"
	pathHeader       = "PATH"
)

// NewDiffFormat returns a format for use with a diff Context
//
// Deprecated: this function was only used internally and will be removed in the next release.
func NewDiffFormat(source string) formatter.Format {
	return newDiffFormat(source)
}

// newDiffFormat returns a format for use with a diff [formatter.Context].
func newDiffFormat(source string) formatter.Format {
	if source == formatter.TableFormatKey {
		return defaultDiffTableFormat
	}
	return formatter.Format(source)
}

// DiffFormatWrite writes formatted diff using the Context
//
// Deprecated: this function was only used internally and will be removed in the next release.
func DiffFormatWrite(fmtCtx formatter.Context, changes []container.FilesystemChange) error {
	return diffFormatWrite(fmtCtx, changes)
}

// diffFormatWrite writes formatted diff using the [formatter.Context].
func diffFormatWrite(fmtCtx formatter.Context, changes []container.FilesystemChange) error {
	return fmtCtx.Write(newDiffContext(), func(format func(subContext formatter.SubContext) error) error {
		for _, change := range changes {
			if err := format(&diffContext{c: change}); err != nil {
				return err
			}
		}
		return nil
	})
}

type diffContext struct {
	formatter.HeaderContext
	c container.FilesystemChange
}

func newDiffContext() *diffContext {
	return &diffContext{
		HeaderContext: formatter.HeaderContext{
			Header: formatter.SubHeaderContext{
				"Type": changeTypeHeader,
				"Path": pathHeader,
			},
		},
	}
}

func (d *diffContext) MarshalJSON() ([]byte, error) {
	return formatter.MarshalJSON(d)
}

func (d *diffContext) Type() string {
	return d.c.Kind.String()
}

func (d *diffContext) Path() string {
	return d.c.Path
}
