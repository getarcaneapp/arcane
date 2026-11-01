package jsonx

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
)

var DurationNano = json.JoinOptions(
	json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
		return enc.WriteToken(jsontext.Int(int64(d)))
	})),
	json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, d *time.Duration) error {
		return json.UnmarshalDecode(dec, (*int64)(d))
	})),
)

var HumaFormat = huma.Format{
	Marshal: func(writer io.Writer, value any) error {
		return json.MarshalWrite(writer, value, DurationNano)
	},
	Unmarshal: func(data []byte, value any) error {
		return json.Unmarshal(data, value, DurationNano)
	},
}

type Serializer struct{}

func (Serializer) Serialize(c *echo.Context, value any, indent string) error {
	return json.MarshalWrite(c.Response(), value, DurationNano, jsontext.WithIndent(indent), jsontext.Multiline(indent != ""))
}

func (Serializer) Deserialize(c *echo.Context, value any) error {
	if err := json.UnmarshalRead(c.Request().Body, value, DurationNano); err != nil {
		return echo.ErrBadRequest.Wrap(err)
	}
	return nil
}
