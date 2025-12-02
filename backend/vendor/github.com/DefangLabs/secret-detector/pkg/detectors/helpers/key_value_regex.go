package helpers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/go-multierror"
)

// opt = optional
const (
	multilineFlag                        = `(?m)`
	linePrefix                           = `(?:^|[\t ]+)`
	optDeclareKeyword                    = `(?i)(?:(?:var|let|set|dim|declare|export|const|readonly)[\t ]+)?(?-i)`
	keyOptPrefix                         = `\[?[\t ]*['"]?`
	defaultKeyCaptureGroup               = `([\t a-zA-Z0-9$\/_-]+)`
	keyOptSuffix                         = `['"]?[\t ]*\]?`
	delimiterCaptureGroup                = `\s*(:|=|:=)\s*`
	delimiterCaptureGroupWithoutNewLines = `[^\S\r\n]*(:|=|:=)[^\S\r\n]*`
	valueOptPrefix                       = `\[?[\t ]*['"]?`
	placeholderCaptureGroup              = `(%s)`
	valueOptSuffix                       = `['"]?[\t ]*\]?[\t ]*;?`
	lineSuffix                           = `(?:[\t ]+|$)`

	// defaultKeyValueRegex = (?m)(?:^|[\t ]+)(?:(?i)(?:(?:var|let|set|dim|declare|export|const|readonly)[\t ]+)?(?-i)\[?[\t ]*['"]?([\t a-zA-Z0-9$\/_-]+)['"]?[\t ]*\]?\s*(:|=|:=)\s*)?\[?[\t ]*['"]?(%s)['"]?[\t ]*\]?[\t ]*;?(?:[\t ]+|$)
	defaultKeyValueRegex = multilineFlag + linePrefix +
		`(?:` + optDeclareKeyword + keyOptPrefix + defaultKeyCaptureGroup + keyOptSuffix + delimiterCaptureGroup + `)?` +
		valueOptPrefix + placeholderCaptureGroup + valueOptSuffix + lineSuffix

	// defaultKeyValueRegexWithoutNewLine = (?m)(?:^|[\t ]+)(?:(?i)(?:(?:var|let|set|dim|declare|export|const|readonly)[\t ]+)?(?-i)\[?[\t ]*['"]?([\t a-zA-Z0-9$\/_-]+)['"]?[\t ]*\]?[^\S\r\n]*(:|=|:=)[^\S\r\n]*)?\[?[\t ]*['"]?(%s)['"]?[\t ]*\]?[\t ]*;?(?:[\t ]+|$)
	defaultKeyValueRegexWithoutNewLine = multilineFlag + linePrefix +
		`(?:` + optDeclareKeyword + keyOptPrefix + defaultKeyCaptureGroup + keyOptSuffix + delimiterCaptureGroupWithoutNewLines + `)?` +
		valueOptPrefix + placeholderCaptureGroup + valueOptSuffix + lineSuffix

	// keyValueRegex = (?m)(?:^|[\t ]+)(?i)(?:(?:var|let|set|dim|declare|export|const|readonly)[\t ]+)?(?-i)\[?[\t ]*['"]?(%s)['"]?[\t ]*\]?\s*(:|=|:=)\s*\[?[\t ]*['"]?(%s)['"]?[\t ]*\]?[\t ]*;?(?:[\t ]+|$)
	keyValueRegex = multilineFlag + linePrefix +
		optDeclareKeyword + keyOptPrefix + placeholderCaptureGroup + keyOptSuffix + delimiterCaptureGroup +
		valueOptPrefix + placeholderCaptureGroup + valueOptSuffix + lineSuffix
)

type MatchResult struct {
	FullMatch       string
	Key             string
	Delimiter       string
	Value           string
	ValueSubmatches []string
}

// KeyValueRegex should match strings in a structure of key (default or injected via NewKeyValueRegex), a delimiter,
//
//	and value injected via argument.
//
// Expected pattern examples:
//
//	key: value
//	"key": "value"
//	key=value
//	"key" = 'value'
//	key := value
//	[ "key" ] = [ "value" ] ;
//	value
//	"value"
//
// Regex breakdown:
//
//		Key:
//		  (?i)(?:(?:var|let|set|dim|declare|export|const|readonly)[\t ]+)?(?-i)
//		                           an optional declaration keyword from commonly used languages like bash, powershell, JS.
//		                           supported keywords: var, let, set, dim, declare, export, const, readonly
//
//		  \[?[\t ]*['"]?			an optional prefix: a square bracket ([) and a single (') or double quote (").
//		  ([\t a-zA-Z0-9\/_-]+)	default key name pattern, comprised of letters, digits, spaces, tabs, slash (/),
//		                           underscore (_) and dash (-).
//		  							captured as capture group #1.
//		  ['"]?[\t ]*\]?			an optional suffix: single (') or double quote (") and a square bracket (]).
//
//		Delimiter:
//		  \s*						optional whitespaces between key and delimiter.
//		  (:|=|:=)					a delimiter: colon (:), equal sign (=), or colon equal sign (:=).
//		  							captured as capture group #2.
//		  \s*						optional whitespaces between delimiter and value.
//
//		  (?: )?					key and delimiter regex are surrounded by an optional non-capturing group.
//		  							That makes the key and delimiter optional (they are capture only if both exist).
//
//	    [^\S\r\n]*                 the regex without new line uses it, the \S is negative of all whitespaces and by using ^ on this we get all the whitespaces, together with \r\n we have all whitespaces except new lines.
//
//		Value:
//		  \[?[\t ]*['"]?			an optional prefix: a square bracket ([) and a single (') or double quote (").
//		  (%s)               		the value regex will be injected here.
//		  							captured as capture group #3.
//		  							internal capturing groups inside value regex will be captured as #4-#n.
//		  ['"]?[\t ]*\]?[\t ]*;?	an optional suffix: single (') or double quote ("), a square bracket (]) and a semicolon (;).
type KeyValueRegex struct {
	keyValueRegex []*regexp.Regexp
}

func NewDefaultKeyValueRegex(valueRegex ...string) *KeyValueRegex {
	s := make([]*regexp.Regexp, 0, len(valueRegex))
	for _, regex := range valueRegex {
		s = append(s, regexp.MustCompile(fmt.Sprintf(defaultKeyValueRegex, regex)))
	}

	return &KeyValueRegex{
		keyValueRegex: s,
	}
}

func NewDefaultKeyValueRegexWithoutNewLine(valueRegex ...string) *KeyValueRegex {
	s := make([]*regexp.Regexp, 0, len(valueRegex))
	for _, regex := range valueRegex {
		s = append(s, regexp.MustCompile(fmt.Sprintf(defaultKeyValueRegexWithoutNewLine, regex)))
	}

	return &KeyValueRegex{
		keyValueRegex: s,
	}
}

func NewKeyValueRegex(keyRegex, valueRegex string) *KeyValueRegex {
	return &KeyValueRegex{
		keyValueRegex: []*regexp.Regexp{
			regexp.MustCompile(fmt.Sprintf(keyValueRegex, keyRegex, valueRegex)),
		},
	}
}

func (r *KeyValueRegex) FindAll(in string) ([]MatchResult, error) {
	res := make([]MatchResult, 0)
	var err error
	for _, regex := range r.keyValueRegex {
		currRes, currErr := findAll(in, regex)
		res = append(res, currRes...)
		if currErr != nil {
			err = multierror.Append(currErr)
		}
	}
	return res, err
}

func findAll(in string, regex *regexp.Regexp) ([]MatchResult, error) {
	submatches := regex.FindAllStringSubmatch(in, -1)
	if len(submatches) == 0 {
		return nil, nil
	}

	res := make([]MatchResult, 0, len(submatches))
	var err error
	for _, submatch := range submatches {
		if len(submatch) < 4 {
			// This should never happen since the base key-value regex have 3 capture groups + first is the full match
			err = multierror.Append(err,
				fmt.Errorf("unexpected number of submatches. regex=%s, input=%s, match=%v", regex, in, submatch),
			)
			continue
		}

		result := MatchResult{
			FullMatch:       strings.TrimSpace(submatch[0]),
			Key:             strings.TrimSpace(submatch[1]),
			Delimiter:       strings.TrimSpace(submatch[2]),
			Value:           strings.TrimSpace(submatch[3]),
			ValueSubmatches: submatch[4:],
		}
		res = append(res, result)
	}
	return res, err
}
