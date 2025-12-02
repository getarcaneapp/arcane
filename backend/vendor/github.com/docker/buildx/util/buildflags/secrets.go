package buildflags

import (
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

type Secrets []*Secret

func (s Secrets) Merge(other Secrets) Secrets {
	if other == nil {
		s.Normalize()
		return s
	} else if s == nil {
		other.Normalize()
		return other
	}

	return append(s, other...).Normalize()
}

func (s Secrets) Normalize() Secrets {
	if len(s) == 0 {
		return nil
	}
	return removeSecretDupes(s)
}

type Secret struct {
	ID       string `json:"id,omitempty"`
	FilePath string `json:"src,omitempty"`
	Env      string `json:"env,omitempty"`
}

func (s *Secret) Equal(other *Secret) bool {
	return s.ID == other.ID && s.FilePath == other.FilePath && s.Env == other.Env
}

func (s *Secret) String() string {
	var b csvBuilder
	if s.ID != "" {
		b.Write("id", s.ID)
	}
	if s.FilePath != "" {
		b.Write("src", s.FilePath)
	}
	if s.Env != "" {
		b.Write("env", s.Env)
	}
	return b.String()
}

func (s *Secret) UnmarshalJSON(data []byte) error {
	var v struct {
		ID       string `json:"id,omitempty"`
		FilePath string `json:"src,omitempty"`
		Env      string `json:"env,omitempty"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	s.ID = v.ID
	s.FilePath = v.FilePath
	s.Env = v.Env
	return nil
}

func (s *Secret) UnmarshalText(text []byte) error {
	value := string(text)
	fields, err := csvvalue.Fields(value, nil)
	if err != nil {
		return errors.Wrap(err, "failed to parse csv secret")
	}

	*s = Secret{}

	var typ string
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		key := strings.ToLower(parts[0])

		if len(parts) != 2 {
			return errors.Errorf("invalid field '%s' must be a key=value pair", field)
		}

		value := parts[1]
		switch key {
		case "type":
			if value != "file" && value != "env" {
				return errors.Errorf("unsupported secret type %q", value)
			}
			typ = value
		case "id":
			s.ID = value
		case "source", "src":
			s.FilePath = value
		case "env":
			s.Env = value
		default:
			return errors.Errorf("unexpected key '%s' in '%s'", key, field)
		}
	}
	if typ == "env" && s.Env == "" {
		s.Env = s.FilePath
		s.FilePath = ""
	}
	return nil
}

func ParseSecretSpecs(sl []string) (Secrets, error) {
	fs := make([]*Secret, 0, len(sl))
	for _, v := range sl {
		if v == "" {
			continue
		}

		s, err := parseSecret(v)
		if err != nil {
			return nil, err
		}
		fs = append(fs, s)
	}
	return fs, nil
}

func parseSecret(value string) (*Secret, error) {
	var s Secret
	if err := s.UnmarshalText([]byte(value)); err != nil {
		return nil, err
	}
	return &s, nil
}

func removeSecretDupes(s []*Secret) []*Secret {
	var res []*Secret
	m := map[string]int{}
	for _, sec := range s {
		if i, ok := m[sec.ID]; ok {
			res[i] = sec
		} else {
			m[sec.ID] = len(res)
			res = append(res, sec)
		}
	}
	return res
}
