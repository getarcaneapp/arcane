package registry

import (
	"fmt"
	"strings"

	ref "github.com/distribution/reference"
	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	dockerregistry "github.com/moby/moby/api/types/registry"

	"kit.getarcane.app"
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

	return kit.Ternary(!strings.Contains(hostCandidate, ".") && !strings.Contains(hostCandidate, ":") && hostCandidate != "localhost", "docker.io", hostCandidate)
}

// SplitRegistryURL separates a registry URL into its comparison host and an
// optional repository namespace: "https://ghcr.io/acme/" yields ("ghcr.io", "acme").
func SplitRegistryURL(url string) (host, namespace string) {
	url = strings.TrimSpace(url)
	for _, scheme := range []string{"https://", "http://"} {
		if len(url) >= len(scheme) && strings.EqualFold(url[:len(scheme)], scheme) {
			url = url[len(scheme):]
			break
		}
	}
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

func NormalizeRegistryURL(url string) string {
	host, namespace := SplitRegistryURL(url)
	if host == "docker.io" {
		return "https://index.docker.io/v1/"
	}
	return kit.Ternary(namespace != "", host+"/"+namespace, host)
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
