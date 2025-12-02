// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package formatter

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// charWidth returns the number of horizontal positions a character occupies,
// and is used to account for wide characters when displaying strings.
//
// In a broad sense, wide characters include East Asian Wide, East Asian Full-width,
// (when not in East Asian context) see http://unicode.org/reports/tr11/.
func charWidth(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	case width.Neutral, width.EastAsianAmbiguous, width.EastAsianNarrow, width.EastAsianHalfwidth:
		return 1
	default:
		return 1
	}
}

const shortLen = 12

// TruncateID returns a shorthand version of a string identifier for presentation,
// after trimming digest algorithm prefix (if any).
//
// This function is a copy of [stringid.TruncateID] for presentation / formatting
// purposes.
//
// [stringid.TruncateID]: https://github.com/moby/moby/blob/v28.3.2/pkg/stringid/stringid.go#L19
func TruncateID(id string) string {
	if i := strings.IndexRune(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > shortLen {
		id = id[:shortLen]
	}
	return id
}

// Ellipsis truncates a string to fit within maxDisplayWidth, and appends ellipsis (…).
// For maxDisplayWidth of 1 and lower, no ellipsis is appended.
// For maxDisplayWidth of 1, first char of string will return even if its width > 1.
func Ellipsis(s string, maxDisplayWidth int) string {
	if maxDisplayWidth <= 0 {
		return ""
	}
	rs := []rune(s)
	if maxDisplayWidth == 1 {
		return string(rs[0])
	}

	byteLen := len(s)
	if byteLen == utf8.RuneCountInString(s) {
		if byteLen <= maxDisplayWidth {
			return s
		}
		return string(rs[:maxDisplayWidth-1]) + "…"
	}

	var (
		display      = make([]int, 0, len(rs))
		displayWidth int
	)
	for _, r := range rs {
		cw := charWidth(r)
		displayWidth += cw
		display = append(display, displayWidth)
	}
	if displayWidth <= maxDisplayWidth {
		return s
	}
	for i := range display {
		if display[i] <= maxDisplayWidth-1 && display[i+1] > maxDisplayWidth-1 {
			return string(rs[:i+1]) + "…"
		}
	}
	return s
}

// capitalizeFirst capitalizes the first character of string
func capitalizeFirst(s string) string {
	switch l := len(s); l {
	case 0:
		return s
	case 1:
		return strings.ToLower(s)
	default:
		return strings.ToUpper(string(s[0])) + strings.ToLower(s[1:])
	}
}

// PrettyPrint outputs arbitrary data for human formatted output by uppercasing the first letter.
func PrettyPrint(i any) string {
	switch t := i.(type) {
	case nil:
		return "None"
	case string:
		return capitalizeFirst(t)
	default:
		return capitalizeFirst(fmt.Sprintf("%s", t))
	}
}
