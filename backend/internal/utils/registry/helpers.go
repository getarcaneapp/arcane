package registry

import (
	"fmt"
	"sort"
	"strings"

	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	dockerregistry "github.com/moby/moby/api/types/registry"
	ref "go.podman.io/image/v5/docker/reference"
)

func GetRegistryAddress(imageRef string) (string, error) {
	named, err := ref.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", err
	}
	addr := ref.Domain(named)
	if addr == DefaultRegistryDomain {
		return DefaultRegistryHost, nil
	}
	return addr, nil
}

func ExtractRegistryHost(imageRef string) string {
	if i := strings.IndexByte(imageRef, '@'); i != -1 {
		imageRef = imageRef[:i]
	}

	hostCandidate, _, found := strings.Cut(imageRef, "/")
	if !found {
		return "docker.io"
	}

	if !strings.Contains(hostCandidate, ".") && !strings.Contains(hostCandidate, ":") {
		return "docker.io"
	}
	return hostCandidate
}

func NormalizeRegistryForComparison(url string) string {
	url = strings.TrimSpace(strings.ToLower(url))
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, "/")

	if slash := strings.Index(url, "/"); slash != -1 {
		url = url[:slash]
	}

	if url == "docker.io" || url == "registry-1.docker.io" || url == "index.docker.io" {
		return "docker.io"
	}
	return url
}

func NormalizeRegistryURL(url string) string {
	normalized := NormalizeRegistryForComparison(url)
	if normalized == "docker.io" {
		return "https://index.docker.io/v1/"
	}

	result := strings.TrimSpace(url)
	result = strings.TrimPrefix(result, "https://")
	result = strings.TrimPrefix(result, "http://")
	result = strings.TrimSuffix(result, "/")

	return result
}

func IsRegistryMatch(left, right string) bool {
	return NormalizeRegistryForComparison(left) == NormalizeRegistryForComparison(right)
}

func EncodeAuthHeader(username, password, serverAddress string) (string, error) {
	auth, err := dockerauthconfig.Encode(dockerregistry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: serverAddress,
	})
	if err != nil {
		return "", fmt.Errorf("encode registry auth header: %w", err)
	}
	return auth, nil
}

func DecodeAuthHeader(authEncoded string) (dockerregistry.AuthConfig, error) {
	cfg, err := dockerauthconfig.Decode(strings.TrimSpace(authEncoded))
	if err != nil {
		return dockerregistry.AuthConfig{}, fmt.Errorf("decode registry auth header: %w", err)
	}
	if cfg == nil {
		return dockerregistry.AuthConfig{}, nil
	}
	return *cfg, nil
}

func RegistryAuthLookupKeys(url string) []string {
	normalizedHost := NormalizeRegistryForComparison(url)
	if normalizedHost == "" {
		return nil
	}

	keys := map[string]struct{}{
		normalizedHost: {},
	}
	if normalizedHost == "docker.io" {
		keys["registry-1.docker.io"] = struct{}{}
		keys["index.docker.io"] = struct{}{}
	}

	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
