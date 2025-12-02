package privatekey

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "pk"
	secretType = "Private Key"
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

var privateKeysRegex = []string{
	//".*BEGIN DSA PRIVATE KEY.*",
	//".*BEGIN EC PRIVATE KEY.*",
	//".*BEGIN OPENSSH PRIVATE KEY.*",
	//".*BEGIN PGP PRIVATE KEY BLOCK.*",
	//".*BEGIN PRIVATE KEY.*",
	//".*BEGIN RSA PRIVATE KEY.*",
	//".*BEGIN SSH2 ENCRYPTED PRIVATE KEY.*",
	// PUTTY user key
	".*PuTTY-User-Key-File-.*",
	// private key regex, for example it includes:
	//".*BEGIN DSA PRIVATE KEY.*",
	//".*BEGIN EC PRIVATE KEY.*",
	//".*BEGIN OPENSSH PRIVATE KEY.*",
	//".*BEGIN PGP PRIVATE KEY BLOCK.*",
	//".*BEGIN PRIVATE KEY.*",
	//".*BEGIN RSA PRIVATE KEY.*",
	//".*BEGIN SSH2 ENCRYPTED PRIVATE KEY.*",
	"(-*BEGIN[ \\S]+?PRIVATE KEY(?: BLOCK)?-*)([\\S\n]{4,}?)(-*END[ \\S]+?PRIVATE KEY(?: BLOCK)?-*)",
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, privateKeysRegex...).Build(),
	}
}
