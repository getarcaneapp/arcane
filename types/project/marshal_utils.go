package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/go-units"
)

// unitBytesType is used to detect compose-go fields that need string-or-number
// normalization before they can be decoded by encoding/json.
var unitBytesType = reflect.TypeFor[composetypes.UnitBytes]()

// runtimeServiceJSON is an internal decode shape that lets RuntimeService
// handle serviceConfig separately from the rest of the payload.
type runtimeServiceJSON struct {
	runtimeServiceAlias
	ServiceConfig json.RawMessage `json:"serviceConfig,omitempty"`
}

// runtimeServiceAlias prevents recursive calls back into RuntimeService's
// custom UnmarshalJSON implementation.
type runtimeServiceAlias RuntimeService

// detailsJSON is an internal decode shape that lets Details intercept the
// services array while still decoding the rest of the struct normally.
type detailsJSON struct {
	detailsAlias
	Services        []json.RawMessage `json:"services,omitempty"`
	RuntimeServices []RuntimeService  `json:"runtimeServices,omitempty"`
}

// detailsAlias prevents recursive calls back into Details.UnmarshalJSON.
type detailsAlias Details

// UnmarshalJSON makes RuntimeService tolerant of compose-go UnitBytes fields
// encoded as either JSON strings or JSON numbers inside serviceConfig.
//
// This only affects JSON decoding of RuntimeService values. It does not change
// API response shapes, backend serialization, YAML loading, or any non-JSON
// code paths.
func (r *RuntimeService) UnmarshalJSON(data []byte) error {
	var aux runtimeServiceJSON
	if err := decodeJSONWithNumbersInternal(data, &aux); err != nil {
		return err
	}

	*r = RuntimeService(aux.runtimeServiceAlias)

	serviceConfig, err := unmarshalComposeServiceConfigJSONInternal(aux.ServiceConfig)
	if err != nil {
		return fmt.Errorf("serviceConfig: %w", err)
	}
	r.ServiceConfig = serviceConfig

	return nil
}

// UnmarshalJSON makes Details tolerant of compose-go UnitBytes fields encoded
// as either JSON strings or JSON numbers inside services and runtime service
// configs.
//
// The rest of the Details payload is decoded normally. Only compose service
// payloads are normalized before they are unmarshaled into
// composetypes.ServiceConfig values.
func (d *Details) UnmarshalJSON(data []byte) error {
	var aux detailsJSON
	if err := decodeJSONWithNumbersInternal(data, &aux); err != nil {
		return err
	}

	*d = Details(aux.detailsAlias)
	d.RuntimeServices = aux.RuntimeServices

	if len(aux.Services) == 0 {
		d.Services = nil
		return nil
	}

	services := make([]composetypes.ServiceConfig, 0, len(aux.Services))
	for i, rawService := range aux.Services {
		service, err := unmarshalComposeServiceConfigJSONInternal(rawService)
		if err != nil {
			return fmt.Errorf("services[%d]: %w", i, err)
		}
		if service == nil {
			continue
		}
		services = append(services, *service)
	}
	d.Services = services

	return nil
}

// unmarshalComposeServiceConfigJSON decodes a raw service JSON object into a
// compose-go ServiceConfig after recursively normalizing any UnitBytes-backed
// fields so compose-go can accept string and numeric JSON representations.
func unmarshalComposeServiceConfigJSONInternal(data []byte) (*composetypes.ServiceConfig, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var raw any
	if err := decodeJSONWithNumbersInternal(trimmed, &raw); err != nil {
		return nil, err
	}

	normalized, err := normalizeJSONValueForTypeInternal(raw, reflect.TypeFor[composetypes.ServiceConfig]())
	if err != nil {
		return nil, err
	}

	normalizedBytes, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}

	var service composetypes.ServiceConfig
	if err := json.Unmarshal(normalizedBytes, &service); err != nil {
		return nil, err
	}

	return &service, nil
}

// decodeJSONWithNumbers preserves JSON numbers as json.Number so the
// normalizer can distinguish integer-like values from quoted strings.
func decodeJSONWithNumbersInternal(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(dest)
}

// normalizeJSONValueForType walks decoded JSON using the destination Go type
// as the source of truth.
//
// That keeps the fix generic: any field reachable from ServiceConfig that uses
// composetypes.UnitBytes gets normalized without maintaining a hardcoded field
// name list.
func normalizeJSONValueForTypeInternal(value any, destType reflect.Type) (any, error) {
	if destType == nil || value == nil {
		return value, nil
	}

	for destType.Kind() == reflect.Pointer {
		destType = destType.Elem()
	}

	if destType == unitBytesType {
		return normalizeUnitBytesValueInternal(value)
	}

	switch destType.Kind() {
	case reflect.Struct:
		return normalizeJSONObjectForTypeInternal(value, destType)
	case reflect.Slice, reflect.Array:
		return normalizeJSONArrayForTypeInternal(value, destType)
	case reflect.Map:
		return normalizeJSONMapForTypeInternal(value, destType)
	case reflect.Pointer:
		// Pointer kinds are fully dereferenced above. This branch only exists to
		// keep the reflect.Kind switch exhaustive for linting.
		return value, nil
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16,
		reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.String, reflect.UnsafePointer:
		return value, nil
	}

	return value, nil
}

// normalizeJSONObjectForType normalizes a decoded JSON object according to the
// exported JSON fields of the destination struct type.
func normalizeJSONObjectForTypeInternal(value any, destType reflect.Type) (any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}

	normalized := make(map[string]any, len(object))
	maps.Copy(normalized, object)

	for field := range destType.Fields() {
		if field.PkgPath != "" {
			continue
		}

		if field.Anonymous {
			embedded, err := normalizeJSONValueForTypeInternal(normalized, field.Type)
			if err != nil {
				return nil, err
			}
			embeddedMap, ok := embedded.(map[string]any)
			if ok {
				normalized = embeddedMap
			}
			continue
		}

		fieldName, ok := jsonFieldNameInternal(field)
		if !ok {
			continue
		}

		fieldValue, exists := normalized[fieldName]
		if !exists {
			continue
		}

		normalizedValue, err := normalizeJSONValueForTypeInternal(fieldValue, field.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fieldName, err)
		}
		normalized[fieldName] = normalizedValue
	}

	return normalized, nil
}

// normalizeJSONArrayForType normalizes each decoded array element according to
// the destination element type.
func normalizeJSONArrayForTypeInternal(value any, destType reflect.Type) (any, error) {
	items, ok := value.([]any)
	if !ok {
		return value, nil
	}

	normalized := make([]any, len(items))
	for i, item := range items {
		normalizedItem, err := normalizeJSONValueForTypeInternal(item, destType.Elem())
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		normalized[i] = normalizedItem
	}

	return normalized, nil
}

// normalizeJSONMapForType normalizes map values for string-keyed JSON objects
// whose destination type is a Go map.
func normalizeJSONMapForTypeInternal(value any, destType reflect.Type) (any, error) {
	if destType.Key().Kind() != reflect.String {
		return value, nil
	}

	items, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}

	normalized := make(map[string]any, len(items))
	for key, item := range items {
		normalizedItem, err := normalizeJSONValueForTypeInternal(item, destType.Elem())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		normalized[key] = normalizedItem
	}

	return normalized, nil
}

// normalizeUnitBytesValue converts supported JSON representations for
// compose-go UnitBytes fields into integer byte counts.
//
// Supported inputs:
// - json numbers
// - quoted byte strings such as "268435456"
// - human-readable Docker memory strings such as "256m" or "1g"
func normalizeUnitBytesValueInternal(value any) (any, error) {
	switch v := value.(type) {
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed, nil
		}

		floatValue, err := strconv.ParseFloat(v.String(), 64)
		if err != nil {
			return nil, err
		}
		if floatValue != float64(int64(floatValue)) {
			return nil, fmt.Errorf("invalid UnitBytes value %q", v.String())
		}
		return int64(floatValue), nil

	case string:
		parsed, err := units.RAMInBytes(strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		return parsed, nil

	default:
		return nil, fmt.Errorf("unsupported type %T for UnitBytes field", value)
	}
}

// jsonFieldName returns the JSON field name that should be used for a struct
// field during normalization.
func jsonFieldNameInternal(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}

	if tag == "" {
		return field.Name, true
	}

	name := strings.Split(tag, ",")[0]
	if name == "" {
		return field.Name, true
	}

	return name, true
}
