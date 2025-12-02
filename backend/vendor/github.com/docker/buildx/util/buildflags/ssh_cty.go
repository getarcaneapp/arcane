package buildflags

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
)

var sshType = sync.OnceValue(func() cty.Type {
	return cty.ObjectWithOptionalAttrs(
		map[string]cty.Type{
			"id":    cty.String,
			"paths": cty.List(cty.String),
		},
		[]string{"id", "paths"},
	)
})

func (s *SSHKeys) FromCtyValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return s.fromCtyValue(in, p)
	}

	want := cty.List(sshType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (s *SSHKeys) fromCtyValue(in cty.Value, p cty.Path) (retErr error) {
	*s = make([]*SSH, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		entry := &SSH{}
		if retErr = entry.FromCtyValue(value, p); retErr != nil {
			return false
		}
		*s = append(*s, entry)
		return true
	}
	eachElement(in)(yield)
	return retErr
}

func (s SSHKeys) ToCtyValue() cty.Value {
	if len(s) == 0 {
		return cty.ListValEmpty(sshType())
	}

	vals := make([]cty.Value, len(s))
	for i, entry := range s {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (e *SSH) FromCtyValue(in cty.Value, p cty.Path) error {
	if in.Type() == cty.String {
		if err := e.UnmarshalText([]byte(in.AsString())); err != nil {
			return p.NewError(err)
		}
		return nil
	}

	conv, err := convert.Convert(in, sshType())
	if err != nil {
		return err
	}

	if id := conv.GetAttr("id"); !id.IsNull() && id.IsKnown() {
		e.ID = id.AsString()
	}
	if paths := conv.GetAttr("paths"); !paths.IsNull() && paths.IsKnown() {
		if err := gocty.FromCtyValue(paths, &e.Paths); err != nil {
			return err
		}
	}
	return nil
}

func (e *SSH) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(sshType())
	}

	var ctyPaths cty.Value
	if len(e.Paths) > 0 {
		paths := make([]cty.Value, len(e.Paths))
		for i, path := range e.Paths {
			paths[i] = cty.StringVal(path)
		}
		ctyPaths = cty.ListVal(paths)
	} else {
		ctyPaths = cty.ListValEmpty(cty.String)
	}

	return cty.ObjectVal(map[string]cty.Value{
		"id":    cty.StringVal(e.ID),
		"paths": ctyPaths,
	})
}
