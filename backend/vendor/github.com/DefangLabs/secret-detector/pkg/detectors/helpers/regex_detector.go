package helpers

import (
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

type DetectionVerifier func(string, string) bool

type regexDetector struct {
	secretType    string
	keyValueRegex *KeyValueRegex
	valueRegex    *ValueRegex
	verifier      DetectionVerifier
}

func (d *regexDetector) SecretType() string {
	return d.secretType
}

func (d *regexDetector) Scan(in string) ([]secrets.DetectedSecret, error) {
	res := make([]secrets.DetectedSecret, 0)
	matches, err := d.keyValueRegex.FindAll(in)
	for _, match := range matches {
		if d.verifyDetection(match.Key, match.Value) {
			res = append(res, secrets.DetectedSecret{Key: match.Key, Type: d.SecretType(), Value: match.Value})
		}
	}
	return res, err
}

func (d *regexDetector) ScanMap(keyValueMap map[string]string) ([]secrets.DetectedSecret, error) {
	res := make([]secrets.DetectedSecret, 0)
	for key, value := range keyValueMap {
		if d.valueRegex.Match(value) && d.verifyDetection(key, value) {
			res = append(res, secrets.DetectedSecret{Key: key, Type: d.SecretType(), Value: value})
		}
	}
	return res, nil
}

func (d *regexDetector) verifyDetection(key, value string) bool {
	if d.verifier != nil {
		return d.verifier(key, value)
	}
	return true
}
