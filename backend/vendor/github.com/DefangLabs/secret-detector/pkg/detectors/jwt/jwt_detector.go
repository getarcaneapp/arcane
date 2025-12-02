package jwt

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	jwtparser "github.com/golang-jwt/jwt/v5"

	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "jwt"
	secretType = "JSON Web Token"
	jwtRegex   = `eyJ[A-Za-z0-9-_=]+\.eyJ[A-Za-z0-9-_=]+\.?[A-Za-z0-9-_.+/=]*`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
	jwtParser *jwtparser.Parser
}

func NewDetector(config ...string) secrets.Detector {
	d := &detector{}
	d.jwtParser = &jwtparser.Parser{}
	d.Detector = helpers.NewRegexDetectorBuilder(secretType, jwtRegex).WithVerifier(d.isTokenValid).Build()
	return d
}

func (d *detector) isTokenValid(_, token string) bool {
	numberOfSegments := strings.Count(token, ".") + 1
	if numberOfSegments == 2 {
		return d.ensureUnsignedTokenValidity(token)
	} else if numberOfSegments == 3 {
		return d.ensureSignedTokenValidity(token)
	}
	// This line should be unreachable since regex checks for 2 or 3 segments
	return false
}

// ensureUnsignedTokenValidity ensures that both header and payload decode to a valid JSON objects,
//
//	and header "alg" field is set to "none"
func (d *detector) ensureUnsignedTokenValidity(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		// Shouldn't happen, just for safety
		return false
	}

	header, headerOk := base64ToMap(parts[0])
	if !headerOk {
		return false
	}
	if header["alg"] != "none" {
		return false
	}

	_, payloadOk := base64ToMap(parts[1])
	return payloadOk

}

func (d *detector) ensureSignedTokenValidity(token string) bool {
	_, _, err := d.jwtParser.ParseUnverified(token, jwtparser.MapClaims{})
	return err == nil
}

func base64ToMap(s string) (map[string]interface{}, bool) {
	bytes, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}

	var keyValueMap map[string]interface{}
	err = json.Unmarshal(bytes, &keyValueMap)
	if err != nil {
		return nil, false
	}
	return keyValueMap, true
}
