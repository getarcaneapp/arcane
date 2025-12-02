package aws

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	SecretKeyDetectorName       = "aws_secret_key"
	secretKeyDetectorSecretType = "AWS Secret Key"
	awsSecretKeyRegex           = `(?:AWS|aws).{0,20}['\"][0-9a-zA-Z\/+]{40}['\"]`
)

func init() {
	secrets.GetDetectorFactory().Register(SecretKeyDetectorName, NewSecretKeyDetector)
}

type awsSecretKeyDetector struct {
	secrets.Detector
}

func NewSecretKeyDetector(config ...string) secrets.Detector {
	return &awsSecretKeyDetector{
		Detector: helpers.NewRegexDetectorBuilder(secretKeyDetectorSecretType, awsSecretKeyRegex).Build(),
	}
}
