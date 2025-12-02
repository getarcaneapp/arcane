package buildflags

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

type Attests []*Attest

func (a Attests) Merge(other Attests) Attests {
	if other == nil {
		a.Normalize()
		return a
	} else if a == nil {
		other.Normalize()
		return other
	}

	return append(a, other...).Normalize()
}

func (a Attests) Normalize() Attests {
	if len(a) == 0 {
		return nil
	}
	return removeAttestDupes(a)
}

func (a Attests) ToMap() map[string]*string {
	result := map[string]*string{}
	for _, attest := range a {
		// ignore duplicates
		if _, ok := result[attest.Type]; ok {
			continue
		}

		if attest.Disabled {
			result[attest.Type] = nil
			continue
		}

		var b csvBuilder
		if attest.Type != "" {
			b.Write("type", attest.Type)
		}
		if attest.Disabled {
			b.Write("disabled", "true")
		}
		b.WriteAttributes(attest.Attrs)

		s := b.String()
		result[attest.Type] = &s
	}
	return result
}

type Attest struct {
	Type     string            `json:"type"`
	Disabled bool              `json:"disabled,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

func (a *Attest) Equal(other *Attest) bool {
	if a.Type != other.Type || a.Disabled != other.Disabled {
		return false
	}
	return maps.Equal(a.Attrs, other.Attrs)
}

func (a *Attest) String() string {
	var b csvBuilder
	if a.Type != "" {
		b.Write("type", a.Type)
	}
	if a.Disabled {
		b.Write("disabled", "true")
	}
	if len(a.Attrs) > 0 {
		b.WriteAttributes(a.Attrs)
	}
	return b.String()
}

func (a *Attest) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, len(a.Attrs)+2)
	for k, v := range a.Attrs {
		m[k] = v
	}
	m["type"] = a.Type
	if a.Disabled {
		m["disabled"] = true
	}
	return json.Marshal(m)
}

func (a *Attest) UnmarshalJSON(data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	if typ, ok := m["type"]; ok {
		a.Type, ok = typ.(string)
		if !ok {
			return errors.Errorf("attest type must be a string")
		}
		delete(m, "type")
	}

	if disabled, ok := m["disabled"]; ok {
		a.Disabled, ok = disabled.(bool)
		if !ok {
			return errors.Errorf("attest disabled attribute must be a boolean")
		}
		delete(m, "disabled")
	}

	attrs := make(map[string]string, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return errors.Errorf("attest attribute %q must be a string", k)
		}
		attrs[k] = s
	}
	a.Attrs = attrs
	return nil
}

func (a *Attest) UnmarshalText(text []byte) error {
	in := string(text)
	fields, err := csvvalue.Fields(in, nil)
	if err != nil {
		return err
	}

	a.Attrs = map[string]string{}
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return errors.Errorf("invalid value %s", field)
		}

		switch strings.TrimSpace(strings.ToLower(key)) {
		case "type":
			a.Type = value
		case "disabled":
			disabled, err := strconv.ParseBool(value)
			if err != nil {
				return errors.Wrapf(err, "invalid value %s", field)
			}
			a.Disabled = disabled
		default:
			a.Attrs[key] = value
		}
	}
	return a.validate()
}

func (a *Attest) validate() error {
	if a.Type == "" {
		return errors.Errorf("attestation type not specified")
	}
	return nil
}

func CanonicalizeAttest(attestType string, in string) string {
	if in == "" {
		return ""
	}
	if b, err := strconv.ParseBool(in); err == nil {
		return fmt.Sprintf("type=%s,disabled=%t", attestType, !b)
	}
	return fmt.Sprintf("type=%s,%s", attestType, in)
}

func ParseAttests(in []string) (Attests, error) {
	var outs []*Attest
	for _, s := range in {
		var out Attest
		if err := out.UnmarshalText([]byte(s)); err != nil {
			return nil, err
		}
		outs = append(outs, &out)
	}
	return outs, nil
}

func removeAttestDupes(s []*Attest) []*Attest {
	res := []*Attest{}
	m := map[string]int{}
	for _, att := range s {
		if i, ok := m[att.Type]; ok {
			res[i] = att
		} else {
			m[att.Type] = len(res)
			res = append(res, att)
		}
	}
	return res
}
