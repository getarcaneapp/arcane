package buildflags

import (
	"strconv"
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var attestType = sync.OnceValue(func() cty.Type {
	return cty.Map(cty.String)
})

func (e *Attests) FromCtyValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return e.fromCtyValue(in, p)
	}

	want := cty.List(attestType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (e *Attests) fromCtyValue(in cty.Value, p cty.Path) (retErr error) {
	*e = make([]*Attest, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		entry := &Attest{}
		if retErr = entry.FromCtyValue(value, p); retErr != nil {
			return false
		}
		*e = append(*e, entry)
		return true
	}
	eachElement(in)(yield)
	return retErr
}

func (e Attests) ToCtyValue() cty.Value {
	if len(e) == 0 {
		return cty.ListValEmpty(attestType())
	}

	vals := make([]cty.Value, len(e))
	for i, entry := range e {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (e *Attest) FromCtyValue(in cty.Value, p cty.Path) error {
	if in.Type() == cty.String {
		if err := e.UnmarshalText([]byte(in.AsString())); err != nil {
			return p.NewError(err)
		}
		return nil
	}

	conv, err := convert.Convert(in, cty.Map(cty.String))
	if err != nil {
		return err
	}

	e.Attrs = map[string]string{}
	for it := conv.ElementIterator(); it.Next(); {
		k, v := it.Element()
		if !v.IsKnown() {
			continue
		}

		switch key := k.AsString(); key {
		case "type":
			e.Type = v.AsString()
		case "disabled":
			b, err := strconv.ParseBool(v.AsString())
			if err != nil {
				return err
			}
			e.Disabled = b
		default:
			e.Attrs[key] = v.AsString()
		}
	}
	return nil
}

func (e *Attest) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(cty.Map(cty.String))
	}

	vals := make(map[string]cty.Value, len(e.Attrs)+2)
	for k, v := range e.Attrs {
		vals[k] = cty.StringVal(v)
	}
	vals["type"] = cty.StringVal(e.Type)
	if e.Disabled {
		vals["disabled"] = cty.StringVal("true")
	}
	return cty.MapVal(vals)
}
