package mailchimp

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name        = "mailchimp"
	secretType  = "Mailchimp API Key"
	apiKeyRegex = `[0-9a-f]{32}-us[0-9]{1,2}`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, apiKeyRegex).Build(),
	}
}
