package square

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name             = "square"
	secretType       = "Square authentication"
	accessTokenRegex = `sq0atp-[0-9A-Za-z\\\-_]{22}`
	oAuthSecretRegex = `sq0csp-[0-9A-Za-z\\\-_]{43}`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

// detector for Square authentication - https://squareup.com/
type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	d := &detector{}
	d.Detector = helpers.NewRegexDetectorBuilder(secretType, accessTokenRegex, oAuthSecretRegex).Build()
	return d
}
