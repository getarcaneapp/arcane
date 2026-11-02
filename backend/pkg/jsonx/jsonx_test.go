package jsonx

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestDurationNano(t *testing.T) {
	type payload struct {
		Timeout  *time.Duration `json:"timeout"`
		Interval time.Duration  `json:"interval"`
	}

	encoded, err := json.Marshal(payload{Interval: 2 * time.Second, Timeout: new(time.Millisecond)}, DurationNano)
	require.NoError(t, err)
	require.JSONEq(t, `{"interval":2000000000,"timeout":1000000}`, string(encoded))

	var decoded payload
	require.NoError(t, json.Unmarshal([]byte(`{"interval":3000000000,"timeout":null}`), &decoded, DurationNano))
	require.Equal(t, payload{Interval: 3 * time.Second}, decoded)

	require.Error(t, json.Unmarshal([]byte(`{"interval":"3s"}`), &decoded, DurationNano))
}

func TestSerializer_Serialize(t *testing.T) {
	type response struct {
		Items           []string      `json:"items"`
		Count           int           `json:"count,omitempty"`
		HeartbeatPeriod time.Duration `json:"heartbeatPeriod"`
	}

	tests := []struct {
		name   string
		indent string
		want   string
	}{
		{name: "compact", want: `{"items":[],"count":0,"heartbeatPeriod":5000000000}`},
		{name: "indented", indent: "  ", want: "{\n  \"items\": [],\n  \"count\": 0,\n  \"heartbeatPeriod\": 5000000000\n}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", http.NoBody), recorder)

			require.NoError(t, Serializer{}.Serialize(ctx, response{HeartbeatPeriod: 5 * time.Second}, tc.indent))
			require.Equal(t, tc.want, recorder.Body.String())
		})
	}
}

func TestSerializer_Deserialize(t *testing.T) {
	type requestBody struct {
		Name string `json:"name"`
	}

	t.Run("field names are case sensitive", func(t *testing.T) {
		ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"Name":"arcane"}`)), httptest.NewRecorder())

		var body requestBody
		require.NoError(t, Serializer{}.Deserialize(ctx, &body))
		require.Empty(t, body.Name)
	})

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{name: "duplicate names", body: []byte(`{"name":"first","name":"second"}`)},
		{name: "invalid UTF-8", body: []byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"', '}'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(tc.body)), httptest.NewRecorder())

			var body requestBody
			require.Equal(t, http.StatusBadRequest, echo.StatusCode(Serializer{}.Deserialize(ctx, &body)))
		})
	}
}
