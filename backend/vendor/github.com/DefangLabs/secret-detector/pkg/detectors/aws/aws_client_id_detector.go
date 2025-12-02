package aws

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	ClientIdDetectorName       = "aws_client_id"
	clientIdDetectorSecretType = "AWS Client ID"

	// awsClientIdRegex represents a regex that matches an aws client id.
	// See https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_identifiers.html
	awsClientIdRegex = `(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`
)

func init() {
	secrets.GetDetectorFactory().Register(ClientIdDetectorName, NewClientIdDetector)
}

type awsClientIdDetector struct {
	secrets.Detector
}

func NewClientIdDetector(config ...string) secrets.Detector {
	return &awsClientIdDetector{
		Detector: helpers.NewRegexDetectorBuilder(clientIdDetectorSecretType, awsClientIdRegex).Build(),
	}
}
