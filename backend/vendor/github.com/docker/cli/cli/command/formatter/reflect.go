// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package formatter

import (
	"encoding/json"
	"reflect"
	"unicode"

	"github.com/pkg/errors"
)

// MarshalJSON marshals x into json
// It differs a bit from encoding/json MarshalJSON function for formatter
func MarshalJSON(x any) ([]byte, error) {
	m, err := marshalMap(x)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// marshalMap marshals x to map[string]any
func marshalMap(x any) (map[string]any, error) {
	val := reflect.ValueOf(x)
	if val.Kind() != reflect.Ptr {
		return nil, errors.Errorf("expected a pointer to a struct, got %v", val.Kind())
	}
	if val.IsNil() {
		return nil, errors.Errorf("expected a pointer to a struct, got nil pointer")
	}
	valElem := val.Elem()
	if valElem.Kind() != reflect.Struct {
		return nil, errors.Errorf("expected a pointer to a struct, got a pointer to %v", valElem.Kind())
	}
	typ := val.Type()
	m := make(map[string]any)
	for i := 0; i < val.NumMethod(); i++ {
		k, v, err := marshalForMethod(typ.Method(i), val.Method(i))
		if err != nil {
			return nil, err
		}
		if k != "" {
			m[k] = v
		}
	}
	return m, nil
}

var unmarshallableNames = map[string]struct{}{"FullHeader": {}}

// marshalForMethod returns the map key and the map value for marshalling the method.
// It returns ("", nil, nil) for valid but non-marshallable parameter. (e.g. "unexportedFunc()")
func marshalForMethod(typ reflect.Method, val reflect.Value) (string, any, error) {
	if val.Kind() != reflect.Func {
		return "", nil, errors.Errorf("expected func, got %v", val.Kind())
	}
	name, numIn, numOut := typ.Name, val.Type().NumIn(), val.Type().NumOut()
	_, blackListed := unmarshallableNames[name]
	// FIXME: In text/template, (numOut == 2) is marshallable,
	//        if the type of the second param is error.
	marshallable := unicode.IsUpper(rune(name[0])) && !blackListed &&
		numIn == 0 && numOut == 1
	if !marshallable {
		return "", nil, nil
	}
	result := val.Call(make([]reflect.Value, numIn))
	intf := result[0].Interface()
	return name, intf, nil
}
