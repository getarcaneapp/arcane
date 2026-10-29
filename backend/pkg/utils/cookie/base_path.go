package cookie

import (
	"strings"
	"sync/atomic"

	kit "go.getarcane.app/kit/pkg"
)

var configuredBasePath atomic.Pointer[string]

func SetBasePath(basePath string) {
	configuredBasePath.Store(&basePath)
}

func currentBasePath() string {
	return kit.FromPtr(configuredBasePath.Load())
}

func secureCookieName(name string) string {
	if rest, ok := strings.CutPrefix(name, "__Host-"); ok && currentBasePath() != "" {
		return "__Secure-" + rest
	}
	return name
}
