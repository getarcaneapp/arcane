package registryauth

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	ref "github.com/distribution/reference"
	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	dockerregistry "github.com/moby/moby/api/types/registry"
	kit "go.getarcane.app/kit/pkg"
)

func GetRegistryAddress(imageRef string) (string, error) {
	named, err := ref.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", err
	}
	addr := ref.Domain(named)
	return kit.Ternary(addr == DefaultRegistryDomain, DefaultRegistryHost, addr), nil
}

func ExtractRegistryHost(imageRef string) string {
	if i := strings.IndexByte(imageRef, '@'); i != -1 {
		imageRef = imageRef[:i]
	}

	hostCandidate, _, found := strings.Cut(imageRef, "/")
	if !found {
		return "docker.io"
	}

	return kit.Ternary(!strings.Contains(hostCandidate, ".") && !strings.Contains(hostCandidate, ":"), "docker.io", hostCandidate)
}

// SplitRegistryURL separates a registry URL into its comparison host and an
// optional repository namespace: "https://ghcr.io/acme/" yields ("ghcr.io", "acme").
func SplitRegistryURL(url string) (host, namespace string) {
	url = strings.TrimSpace(url)
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	host, namespace, _ = strings.Cut(strings.Trim(url, "/"), "/")
	host = strings.ToLower(host)
	if host == "registry-1.docker.io" || host == "index.docker.io" {
		host = "docker.io"
	}
	// "https://index.docker.io/v1/" is Docker Hub's API path, not a namespace.
	if host == "docker.io" && (namespace == "v1" || strings.HasPrefix(namespace, "v1/")) {
		namespace = strings.TrimPrefix(namespace[2:], "/")
	}
	return host, namespace
}

func NormalizeRegistryForComparison(url string) string {
	host, _ := SplitRegistryURL(url)
	return host
}

func NormalizeRegistryURL(url string) string {
	host, namespace := SplitRegistryURL(url)
	if host == "docker.io" {
		return "https://index.docker.io/v1/"
	}
	return kit.Ternary(namespace != "", host+"/"+namespace, host)
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

func LookupKeys(url string) []string {
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

	out := slices.Sorted(maps.Keys(keys))
	return out
}
