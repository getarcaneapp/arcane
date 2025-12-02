package stripe

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "stripe"
	secretType = "Stripe API key"

	// Stripe standard API Key begins with sk_live_
	// Stripe restricted API Key begins with rk_live_
	// More information: https://stripe.com/docs/keys
	apiKeyRegex = `[rs]k_(live|test)_[0-9a-zA-Z]{24}`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

// detector for Stripe API keys - https://stripe.com/
type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, apiKeyRegex).Build(),
	}
}
