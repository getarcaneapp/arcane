package github

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "github"
	secretType = "Github authentication"

	// see https://github.blog/2021-04-05-behind-githubs-new-authentication-token-formats/
	tokenRegex = `gh[pousr]_[A-Za-z0-9_]{36}`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, tokenRegex).Build(),
	}
}
