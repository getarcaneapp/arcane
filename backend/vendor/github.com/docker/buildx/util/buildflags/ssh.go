package buildflags

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/moby/buildkit/util/gitutil"
)

type SSHKeys []*SSH

func (s SSHKeys) Merge(other SSHKeys) SSHKeys {
	if other == nil {
		s.Normalize()
		return s
	} else if s == nil {
		other.Normalize()
		return other
	}

	return append(s, other...).Normalize()
}

func (s SSHKeys) Normalize() SSHKeys {
	if len(s) == 0 {
		return nil
	}
	return removeSSHDupes(s)
}

type SSH struct {
	ID    string   `json:"id,omitempty" cty:"id"`
	Paths []string `json:"paths,omitempty" cty:"paths"`
}

func (s *SSH) Equal(other *SSH) bool {
	return s.Less(other) == 0
}

func (s *SSH) Less(other *SSH) int {
	if s.ID != other.ID {
		return cmp.Compare(s.ID, other.ID)
	}
	return slices.Compare(s.Paths, other.Paths)
}

func (s *SSH) String() string {
	if len(s.Paths) == 0 {
		return s.ID
	}

	var b csvBuilder
	paths := strings.Join(s.Paths, ",")
	b.Write(s.ID, paths)
	return b.String()
}

func (s *SSH) UnmarshalJSON(data []byte) error {
	var v struct {
		ID    string   `json:"id,omitempty"`
		Paths []string `json:"paths,omitempty"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	s.ID = v.ID
	s.Paths = v.Paths
	return nil
}

func (s *SSH) UnmarshalText(text []byte) error {
	parts := strings.SplitN(string(text), "=", 2)

	s.ID = parts[0]
	if len(parts) > 1 {
		s.Paths = strings.Split(parts[1], ",")
	} else {
		s.Paths = nil
	}
	return nil
}

func ParseSSHSpecs(sl []string) ([]*SSH, error) {
	var outs []*SSH
	if len(sl) == 0 {
		return nil, nil
	}

	for _, s := range sl {
		if s == "" {
			continue
		}

		var out SSH
		if err := out.UnmarshalText([]byte(s)); err != nil {
			return nil, err
		}
		outs = append(outs, &out)
	}
	return outs, nil
}

// IsGitSSH returns true if the given repo URL is accessed over ssh
func IsGitSSH(repo string) bool {
	url, err := gitutil.ParseURL(repo)
	if err != nil {
		return false
	}
	return url.Scheme == gitutil.SSHProtocol
}

func removeSSHDupes(s []*SSH) []*SSH {
	var res []*SSH
	m := map[string]int{}
	for _, ssh := range s {
		if i, ok := m[ssh.ID]; ok {
			res[i] = ssh
		} else {
			m[ssh.ID] = len(res)
			res = append(res, ssh)
		}
	}
	return res
}
