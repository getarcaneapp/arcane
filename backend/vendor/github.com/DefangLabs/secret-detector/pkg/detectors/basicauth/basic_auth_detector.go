package basicauth

import (
	"encoding/base64"
	"strings"

	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "basic_auth"
	secretType = "HTTP Basic Authentication"

	// basicAuthRegex represents a regex that matches HTTP basic authentication.
	// the parameter is a valid base 64, where the encoded value is ID and password joined by a single colon :.
	// https://en.wikipedia.org/wiki/Basic_access_authentication
	basicAuthRegex = `(?i)(?:\"?authorization\"? *[:=] *)?\"?basic(?-i) +[a-zA-Z0-9+\/,_\-]{2,}={0,2}\"?`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, basicAuthRegex).WithVerifier(isParameterValidBase64).WithKeyValueRegexWithoutNewLine().Build(),
	}
}

func isParameterValidBase64(_, s string) bool {
	words := strings.Split(s, " ")
	if len(words) == 0 {
		return false
	}
	parameter := words[len(words)-1]
	if parameter[len(parameter)-1] == '"' { // clean the optional " at the end
		parameter = parameter[:len(parameter)-1]
	}

	encodedValue, err := base64.StdEncoding.DecodeString(parameter)
	if err != nil {
		return false
	}

	// check if the encoded value contains : in the middle
	for _, char := range encodedValue[1 : len(encodedValue)-1] {
		if char == ':' {
			return true
		}
	}
	return false
}
