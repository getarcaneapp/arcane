package helpers

import (
	"fmt"
	"regexp"
)

const (
	// exactValueCaptureRegex = (?m)^\s*\[?[\t ]*['"]?%s['"]?[\t ]*\]?[\t ]*;?\s*$
	exactValueCaptureRegex = multilineFlag + `^\s*` + valueOptPrefix + `%s` + valueOptSuffix + `\s*$`

	// exactKeyCaptureRegex = (?m)^\s*(?i)(?:(?:var|let|set|dim|declare|export|const|readonly)[\t ]+)?(?-i)\[?[\t ]*['"]?%s['"]?[\t ]*\]?\s*$
	exactKeyCaptureRegex = multilineFlag + `^\s*` + optDeclareKeyword + keyOptPrefix + `%s` + keyOptSuffix + `\s*$`
)

type ValueRegex struct {
	regex []*regexp.Regexp
}

func NewValueRegex(regex ...string) *ValueRegex {
	return newValueRegex(exactValueCaptureRegex, regex)
}

func NewKeyRegex(regex ...string) *ValueRegex {
	return newValueRegex(exactKeyCaptureRegex, regex)
}

func newValueRegex(format string, regex []string) *ValueRegex {
	s := make([]*regexp.Regexp, 0, len(regex))
	for _, r := range regex {
		s = append(s, regexp.MustCompile(fmt.Sprintf(format, r)))
	}

	return &ValueRegex{
		regex: s,
	}
}

func (arr *ValueRegex) Match(in string) bool {
	for _, regex := range arr.regex {
		if regex.MatchString(in) {
			return true
		}
	}
	return false
}

func (arr *ValueRegex) Find(in string) string {
	for _, regex := range arr.regex {
		if res := regex.FindString(in); res != "" {
			return res
		}
	}
	return ""
}

func (arr *ValueRegex) FindAll(in string) []string {
	res := make([]string, 0)
	for _, regex := range arr.regex {
		currRes := regex.FindAllString(in, -1)
		res = append(res, currRes...)
	}
	return res
}
