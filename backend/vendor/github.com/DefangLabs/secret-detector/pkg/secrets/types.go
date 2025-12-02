package secrets

import (
	"io"

	"github.com/DefangLabs/secret-detector/pkg/dataformat"
)

type Detector interface {
	// Scan scans secrets from the given string.
	// Input structure is unexpected, and may contain multiple secrets.
	Scan(in string) ([]DetectedSecret, error)
	// ScanMap scans secrets from the given key-value pairs.
	ScanMap(keyValueMap map[string]string) ([]DetectedSecret, error)
	// SecretType returns a human-readable string that describes the type of secret that is detected by this Detector.
	// This string should be returned as DetectedSecret.Type when detecting a secret.
	SecretType() string
}

type Transformer interface {
	Transform(in string) (map[string]string, bool)
	SupportedFormats() []dataformat.DataFormat
	SupportFiles() bool
}

type Scanner interface {
	ScanFile(path string) ([]DetectedSecret, error)
	ScanFileReader(in io.Reader, path string, size int64) ([]DetectedSecret, error)
	ScanWithFormat(in io.Reader, format dataformat.DataFormat) ([]DetectedSecret, error)
	ScanStringWithFormat(inStr string, format dataformat.DataFormat) ([]DetectedSecret, error)
	ScanReader(in io.Reader) ([]DetectedSecret, error)
	Scan(in string) ([]DetectedSecret, error)
}

// DetectedSecret represents a secret detected by the engine.
type DetectedSecret struct {
	// Type is the type of the detector that detected the secret
	Type string
	// Key is the key of the detected secret.
	// Key can be empty if a secret was detected without one.
	Key string
	// Value is the value of the detected secret
	Value string
}
