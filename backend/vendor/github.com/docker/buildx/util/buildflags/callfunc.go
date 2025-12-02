package buildflags

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

const defaultCallFunc = "build"

type CallFunc struct {
	Name         string
	Format       string
	IgnoreStatus bool
}

func (x *CallFunc) String() string {
	var elems []string
	if x.Name != "" {
		elems = append(elems, fmt.Sprintf("Name:%q", x.Name))
	}
	if x.Format != "" {
		elems = append(elems, fmt.Sprintf("Format:%q", x.Format))
	}
	if x.IgnoreStatus {
		elems = append(elems, fmt.Sprintf("IgnoreStatus:%v", x.IgnoreStatus))
	}
	return strings.Join(elems, " ")
}

func ParseCallFunc(str string) (*CallFunc, error) {
	if str == "" {
		return nil, nil
	}

	fields, err := csvvalue.Fields(str, nil)
	if err != nil {
		return nil, err
	}
	f := &CallFunc{}
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) == 2 {
			switch parts[0] {
			case "format":
				f.Format = parts[1]
			case "ignorestatus":
				v, err := strconv.ParseBool(parts[1])
				if err != nil {
					return nil, errors.Wrapf(err, "invalid ignorestatus print value: %s", parts[1])
				}
				f.IgnoreStatus = v
			default:
				return nil, errors.Errorf("invalid print field: %s", field)
			}
		} else {
			if f.Name != "" {
				return nil, errors.Errorf("invalid print value: %s", str)
			}
			f.Name = field
		}
	}

	// "check" has been added as an alias for "lint",
	// in order to maintain backwards compatibility
	// we need to convert it.
	if f.Name == "check" {
		f.Name = "lint"
	}

	if f.Name == defaultCallFunc {
		return nil, nil
	}

	return f, nil
}
