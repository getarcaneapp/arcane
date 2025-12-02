package npm

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "npm"
	secretType = "npm auth token"

	// see https://docs.npmjs.com/using-private-packages-in-a-ci-cd-workflow
	authTokenRegex = `//\S+/:_authToken=[^${\s]\S+`
	// see https://docs.npmjs.com/creating-and-viewing-access-tokens
	accessTokenRegex = `npm_[a-zA-Z0-9]{36}`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, authTokenRegex, accessTokenRegex).Build(),
	}
}
