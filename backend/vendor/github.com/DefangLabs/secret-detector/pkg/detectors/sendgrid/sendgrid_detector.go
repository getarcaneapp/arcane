package sendgrid

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "sendgrid"
	secretType = "Sendgrid API key"

	// see https://docs.sendgrid.com/ui/account-and-settings/api-keys
	//     https://web.archive.org/web/20200202153737/https://d2w67tjf43xwdp.cloudfront.net/Classroom/Basics/API/what_is_my_api_key.html
	apiKeyRegex = `SG\.[a-zA-Z0-9_-]{22}\.[a-zA-Z0-9_-]{43}`
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
