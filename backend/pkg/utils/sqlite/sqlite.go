package sqlite

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

var (
	registerOnce sync.Once
	errRegister  error
)

// RegisterFunctions makes normalize(text, form) available to new SQLite connections.
func RegisterFunctions() error {
	registerOnce.Do(func() {
		errRegister = sqlite.RegisterDeterministicScalarFunction("normalize", 2,
			func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				if args[0] == nil {
					return nil, nil
				}
				value, ok := args[0].(string)
				if !ok {
					return nil, errors.New("normalize requires text as its first argument")
				}
				name, ok := args[1].(string)
				if !ok {
					return nil, errors.New("normalize requires a form as its second argument")
				}
				var form norm.Form
				switch strings.ToLower(name) {
				case "nfc":
					form = norm.NFC
				case "nfd":
					form = norm.NFD
				case "nfkc":
					form = norm.NFKC
				case "nfkd":
					form = norm.NFKD
				default:
					return nil, fmt.Errorf("unsupported normalization form: %s", name)
				}
				return form.String(value), nil
			},
		)
	})
	return errRegister
}
