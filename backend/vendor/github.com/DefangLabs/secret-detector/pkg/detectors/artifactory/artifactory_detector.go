package artifactory

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "artifactory"
	secretType = "JFrog Artifactory credentials"

	// artifactoryTokenRegex represents a regex that matches an artifactory token.
	// Artifactory token always begins with AKC.
	artifactoryTokenRegex = `AKC[a-zA-Z0-9]{10,}`

	// artifactoryEncryptedPasswordRegex represents a regex that matches an artifactory encrypted password.
	// Artifactory encrypted passwords always begin with AP.
	// this detection is removed
	artifactoryEncryptedPasswordRegex = `AP[\dABCDEF][a-zA-Z0-9]{8,}`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	d := &detector{}
	d.Detector = helpers.NewRegexDetectorBuilder(secretType, artifactoryTokenRegex).Build()
	return d
}
