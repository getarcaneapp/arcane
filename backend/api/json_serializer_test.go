package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
)

func TestJSONV2SerializerUsesV2ResponseSemantics(t *testing.T) {
	type response struct {
		Items []string `json:"items"`
		Count int      `json:"count,omitempty"`
	}

	e := echo.New()
	recorder := httptest.NewRecorder()
	context := e.NewContext(httptest.NewRequest("GET", "/", nil), recorder)

	err := (jsonV2Serializer{}).Serialize(context, response{}, "")
	if err != nil {
		t.Fatalf("serialize response: %v", err)
	}
	if got, want := recorder.Body.String(), `{"items":[],"count":0}`; got != want {
		t.Fatalf("serialized response = %s, want %s", got, want)
	}
}

func TestJSONV2SerializerPreservesDurationNanoseconds(t *testing.T) {
	type response struct {
		HeartbeatPeriod time.Duration `json:"heartbeatPeriod"`
	}

	e := echo.New()
	recorder := httptest.NewRecorder()
	context := e.NewContext(httptest.NewRequest("GET", "/", nil), recorder)

	err := (jsonV2Serializer{}).Serialize(context, response{HeartbeatPeriod: 5 * time.Second}, "")
	if err != nil {
		t.Fatalf("serialize duration: %v", err)
	}
	if got, want := recorder.Body.String(), `{"heartbeatPeriod":5000000000}`; got != want {
		t.Fatalf("serialized response = %s, want %s", got, want)
	}
}

func TestJSONV2SerializerUsesStrictV2Decoding(t *testing.T) {
	type requestBody struct {
		Name string `json:"name"`
	}

	t.Run("field names are case sensitive", func(t *testing.T) {
		e := echo.New()
		context := e.NewContext(httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"Name":"arcane"}`)), httptest.NewRecorder())

		var body requestBody
		err := (jsonV2Serializer{}).Deserialize(context, &body)
		if err != nil {
			t.Fatalf("deserialize case-variant field: %v", err)
		}
		if body.Name != "" {
			t.Fatalf("case-variant field populated Name with %q", body.Name)
		}
	})

	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "duplicate names", body: []byte(`{"name":"first","name":"second"}`)},
		{name: "invalid UTF-8", body: []byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"', '}'}},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := echo.New()
			context := e.NewContext(httptest.NewRequest("POST", "/", bytes.NewReader(test.body)), httptest.NewRecorder())

			var body requestBody
			err := (jsonV2Serializer{}).Deserialize(context, &body)
			var httpErr *echo.HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("deserialize error = %T %v, want *echo.HTTPError", err, err)
			}
			if httpErr.StatusCode() != http.StatusBadRequest {
				t.Fatalf("HTTP status = %d, want %d", httpErr.StatusCode(), http.StatusBadRequest)
			}
		})
	}
}
