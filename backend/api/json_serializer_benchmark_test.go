package api

import (
	"bytes"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"io"
	"testing"
)

type jsonBenchmarkItem struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	State   string   `json:"state"`
	Image   string   `json:"image"`
	Ports   []int    `json:"ports"`
	Aliases []string `json:"aliases"`
}

type jsonBenchmarkResponse struct {
	Items []jsonBenchmarkItem `json:"items"`
	Total int                 `json:"total"`
}

var jsonBenchmarkPayload = func() jsonBenchmarkResponse {
	items := make([]jsonBenchmarkItem, 64)
	for index := range items {
		items[index] = jsonBenchmarkItem{
			ID:      "01JARCANEJSONBENCHMARK",
			Name:    "arcane-backend",
			State:   "running",
			Image:   "ghcr.io/getarcaneapp/arcane:latest",
			Ports:   []int{3552, 3553},
			Aliases: []string{"arcane", "backend"},
		}
	}

	return jsonBenchmarkResponse{Items: items, Total: len(items)}
}()

func BenchmarkJSONResponseEncoding(b *testing.B) {
	b.Run("v1 encoder", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := jsonv1.NewEncoder(io.Discard).Encode(jsonBenchmarkPayload); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("v2 MarshalWrite", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := jsonv2.MarshalWrite(io.Discard, jsonBenchmarkPayload, jsonV2APIOptions); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkJSONRequestDecoding(b *testing.B) {
	data, err := jsonv2.Marshal(jsonBenchmarkPayload)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))

	b.Run("v1 decoder", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var output jsonBenchmarkResponse
			if err := jsonv1.NewDecoder(bytes.NewReader(data)).Decode(&output); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("v2 UnmarshalRead", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var output jsonBenchmarkResponse
			if err := jsonv2.UnmarshalRead(bytes.NewReader(data), &output, jsonV2APIOptions); err != nil {
				b.Fatal(err)
			}
		}
	})
}
