package azure

import (
	"strings"

	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "azure"
	secretType = "Azure Storage Account access key"
	// Account Key (AccountKey=xxxxxxxxx)
	azureStorageKeyRegex = `(AccountKey=)?[a-zA-Z0-9+\/=]{88}`
	expectedMatchKey     = "AccountKey"
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	d := &detector{}
	d.Detector = helpers.NewRegexDetectorBuilder(secretType, azureStorageKeyRegex).WithVerifier(d.isStorageKey).Build()
	return d
}

func (d *detector) isStorageKey(key, value string) bool {
	// the expectedMatchKey is necessary, we will also check the key for it if it's part of a map
	return strings.HasPrefix(value, expectedMatchKey) || strings.HasSuffix(key, expectedMatchKey)
}
