package buildflags

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var exportEntryType = sync.OnceValue(func() cty.Type {
	return cty.Map(cty.String)
})

func (e *Exports) FromCtyValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return e.fromCtyValue(in, p)
	}

	want := cty.List(exportEntryType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (e *Exports) fromCtyValue(in cty.Value, p cty.Path) (retErr error) {
	*e = make([]*ExportEntry, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		entry := &ExportEntry{}
		if retErr = entry.FromCtyValue(value, p); retErr != nil {
			return false
		}
		*e = append(*e, entry)
		return true
	}
	eachElement(in)(yield)
	return retErr
}

func (e Exports) ToCtyValue() cty.Value {
	if len(e) == 0 {
		return cty.ListValEmpty(exportEntryType())
	}

	vals := make([]cty.Value, len(e))
	for i, entry := range e {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (e *ExportEntry) FromCtyValue(in cty.Value, p cty.Path) error {
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

	m := conv.AsValueMap()
	if err := getAndDelete(m, "type", &e.Type); err != nil {
		return err
	}
	if err := getAndDelete(m, "dest", &e.Destination); err != nil {
		return err
	}
	e.Attrs = asMap(m)
	return e.validate()
}

func (e *ExportEntry) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(cty.Map(cty.String))
	}

	vals := make(map[string]cty.Value, len(e.Attrs)+2)
	for k, v := range e.Attrs {
		vals[k] = cty.StringVal(v)
	}
	vals["type"] = cty.StringVal(e.Type)
	vals["dest"] = cty.StringVal(e.Destination)
	return cty.MapVal(vals)
}
