package project

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"strconv"
	"strings"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/go-units"
)

var unitBytesUnmarshalers = json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, value *composetypes.UnitBytes) error {
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}

	switch token.Kind() {
	case jsontext.KindNull:
		return nil
	case jsontext.KindString:
		parsed, err := units.RAMInBytes(strings.TrimSpace(token.String()))
		if err != nil {
			return err
		}
		*value = composetypes.UnitBytes(parsed)
		return nil
	case jsontext.KindNumber:
		raw := token.String()
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			floatValue, parseErr := strconv.ParseFloat(raw, 64)
			if parseErr != nil {
				return parseErr
			}
			parsed = int64(floatValue)
			if floatValue != float64(parsed) {
				return errors.Errorf("invalid UnitBytes value %q", raw)
			}
		}
		*value = composetypes.UnitBytes(parsed)
		return nil
	case jsontext.KindInvalid, jsontext.KindFalse, jsontext.KindTrue,
		jsontext.KindBeginObject, jsontext.KindEndObject,
		jsontext.KindBeginArray, jsontext.KindEndArray:
		return errors.Errorf("unsupported JSON kind %s for UnitBytes", token.Kind())
	}

	return errors.Errorf("unsupported JSON kind %s for UnitBytes", token.Kind())
})

type runtimeServiceAlias RuntimeService

func (r *RuntimeService) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*runtimeServiceAlias)(r), json.WithUnmarshalers(unitBytesUnmarshalers))
}

type detailsAlias Details

func (d *Details) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*detailsAlias)(d), json.WithUnmarshalers(unitBytesUnmarshalers))
}
