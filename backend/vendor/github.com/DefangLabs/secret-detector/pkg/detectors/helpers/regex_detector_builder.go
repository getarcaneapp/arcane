package helpers

import (
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

type RegexDetectorBuilder struct {
	secretType    string
	regexes       []string
	keyValueRegex *KeyValueRegex
	valueRegex    *ValueRegex
	verifier      DetectionVerifier
}

// NewRegexDetectorBuilder creates a generic regex detector builder.
// It can be used as an embedded struct to implement most types of regex based detectors.
func NewRegexDetectorBuilder(secretType string, regex ...string) *RegexDetectorBuilder {
	return &RegexDetectorBuilder{
		secretType:    secretType,
		regexes:       regex,
		keyValueRegex: NewDefaultKeyValueRegex(regex...),
		valueRegex:    NewValueRegex(regex...),
	}
}

func (builder *RegexDetectorBuilder) WithVerifier(verifier DetectionVerifier) *RegexDetectorBuilder {
	builder.verifier = verifier
	return builder
}

// WithKeyValueRegexWithoutNewLine set the default regex that tries to find key-value matches,
// to not accept new lines between the key and the value
// it can be used in cases that the secret ends with delimiter (for example base64 ends with =)
// in that case the match be corrupted with the default regex.
func (builder *RegexDetectorBuilder) WithKeyValueRegexWithoutNewLine() *RegexDetectorBuilder {
	builder.keyValueRegex = NewDefaultKeyValueRegexWithoutNewLine(builder.regexes...)
	return builder
}

func (builder *RegexDetectorBuilder) Build() secrets.Detector {
	return &regexDetector{
		secretType:    builder.secretType,
		keyValueRegex: builder.keyValueRegex,
		valueRegex:    builder.valueRegex,
		verifier:      builder.verifier,
	}
}
