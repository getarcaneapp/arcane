package utils

import (
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
)

func CapitalizeFirstLetter(str string) string {
	if len(str) == 0 {
		return str
	}
	return strings.ToUpper(str[:1]) + str[1:]
}

func CamelCaseToSnakeCase(str string) string {
	var result strings.Builder
	for i, r := range str {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

var camelCaseToScreamingSnakeCaseRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func CamelCaseToScreamingSnakeCase(s string) string {
	// Insert underscores before uppercase letters (except the first one)
	stringValue := camelCaseToScreamingSnakeCaseRe.ReplaceAllString(s, `${1}_${2}`)

	// Convert to uppercase
	return strings.ToUpper(stringValue)
}

func GenerateRandomString(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}

func TrimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
