package buildflags

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var secretType = sync.OnceValue(func() cty.Type {
	return cty.ObjectWithOptionalAttrs(
		map[string]cty.Type{
			"id":  cty.String,
			"src": cty.String,
			"env": cty.String,
		},
		[]string{"id", "src", "env"},
	)
})

func (s *Secrets) FromCtyValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return s.fromCtyValue(in, p)
	}

	want := cty.List(secretType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (s *Secrets) fromCtyValue(in cty.Value, p cty.Path) (retErr error) {
	*s = make([]*Secret, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		entry := &Secret{}
		if retErr = entry.FromCtyValue(value, p); retErr != nil {
			return false
		}
		*s = append(*s, entry)
		return true
	}
	eachElement(in)(yield)
	return retErr
}

func (s Secrets) ToCtyValue() cty.Value {
	if len(s) == 0 {
		return cty.ListValEmpty(secretType())
	}

	vals := make([]cty.Value, len(s))
	for i, entry := range s {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (e *Secret) FromCtyValue(in cty.Value, p cty.Path) error {
	if in.Type() == cty.String {
		if err := e.UnmarshalText([]byte(in.AsString())); err != nil {
			return p.NewError(err)
		}
		return nil
	}

	conv, err := convert.Convert(in, secretType())
	if err != nil {
		return err
	}

	if id := conv.GetAttr("id"); !id.IsNull() && id.IsKnown() {
		e.ID = id.AsString()
	}
	if src := conv.GetAttr("src"); !src.IsNull() && src.IsKnown() {
		e.FilePath = src.AsString()
	}
	if env := conv.GetAttr("env"); !env.IsNull() && env.IsKnown() {
		e.Env = env.AsString()
	}
	return nil
}

func (e *Secret) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(secretType())
	}

	return cty.ObjectVal(map[string]cty.Value{
		"id":  cty.StringVal(e.ID),
		"src": cty.StringVal(e.FilePath),
		"env": cty.StringVal(e.Env),
	})
}
