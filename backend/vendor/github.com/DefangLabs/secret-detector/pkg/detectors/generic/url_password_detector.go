package generic

import (
	"net/url"
	"strings"

	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	URLPasswordDetectorName       = "url_password"
	urlPasswordDetectorSecretType = "URL with password"

	// urlPasswordRegex represents a regex that matches urls with user & password.
	// e.g. scheme://user:pass@domain.com/
	urlPasswordRegex = `[a-z][a-z0-9+.-]+://[^:\s]*:[^@:\s]+@[^\s'"\];]+`
)

func init() {
	secrets.GetDetectorFactory().Register(URLPasswordDetectorName, NewURLPasswordDetector)
}

type urlPasswordDetector struct {
	secrets.Detector
}

func NewURLPasswordDetector(config ...string) secrets.Detector {
	d := &urlPasswordDetector{}
	d.Detector = helpers.NewRegexDetectorBuilder(urlPasswordDetectorSecretType, urlPasswordRegex).WithVerifier(isUrlWithPassword).Build()
	return d
}

func isUrlWithPassword(_, s string) bool {
	u, _ := url.Parse(s)
	if u == nil || u.User == nil {
		return false
	}

	pwd, _ := u.User.Password()
	return pwd != "" && !strings.HasPrefix(pwd, "$")
}
