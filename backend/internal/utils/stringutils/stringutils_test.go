package stringutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCapitalizeFirstLetter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"single lowercase", "a", "A"},
		{"single uppercase", "A", "A"},
		{"word starting with lowercase", "hello", "Hello"},
		{"word starting with uppercase", "Hello", "Hello"},
		{"sentence", "hello world", "Hello world"},
		{"starts with number", "123hello", "123hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, CapitalizeFirstLetter(tt.input))
		})
	}
}

func TestCamelCaseToSnakeCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"simple", "camelCase", "camel_case"},
		{"multiple words", "thisIsALongerString", "this_is_a_longer_string"},
		{"already snake", "already_snake", "already_snake"},
		{"single word", "word", "word"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, CamelCaseToSnakeCase(tt.input))
		})
	}
}

func TestCamelCaseToScreamingSnakeCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"simple", "camelCase", "CAMEL_CASE"},
		{"multiple words", "thisIsALongerString", "THIS_IS_A_LONGER_STRING"},
		{"with number", "version2", "VERSION2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, CamelCaseToScreamingSnakeCase(tt.input))
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	t.Run("length check", func(t *testing.T) {
		length := 32
		s := GenerateRandomString(length)
		assert.Len(t, s, length)
	})

	t.Run("randomness check", func(t *testing.T) {
		s1 := GenerateRandomString(16)
		s2 := GenerateRandomString(16)
		assert.NotEqual(t, s1, s2, "Consecutive calls should produce different strings")
	})
}

func TestTrimQuotes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"no quotes", "hello", "hello"},
		{"double quotes", "\"hello\"", "hello"},
		{"single quotes", "'hello'", "hello"},
		{"only starting double quote", "\"hello", "\"hello"},
		{"only ending single quote", "hello'", "hello'"},
		{"mismatched quotes", "'hello\"", "'hello\""},
		{"long string with quotes", "\"this is a test\"", "this is a test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, TrimQuotes(tt.input))
		})
	}
}
