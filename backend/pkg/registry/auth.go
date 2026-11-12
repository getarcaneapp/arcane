package registry

import (
	"cmp"
	"fmt"
	"slices"
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

// MatchingForImage returns the items whose registry URL serves imageRef, most specific namespace first.
func MatchingForImage[T any](imageRef string, items []T, registryURL func(T) string) ([]T, error) {
	named, err := ref.ParseNormalizedNamed(imageRef)
	if err != nil {
		return nil, err
	}
	imageHost, _ := SplitRegistryURL(ref.Domain(named))
	if imageHost == "" {
		return nil, nil
	}
	matches := slices.DeleteFunc(slices.Clone(items), func(item T) bool {
		host, _ := SplitRegistryURL(registryURL(item))
		return host != imageHost
	})
	slices.SortStableFunc(matches, func(a, b T) int {
		return cmp.Compare(namespaceMatchLength(ref.Path(named), registryURL(b)), namespaceMatchLength(ref.Path(named), registryURL(a)))
	})
	return matches, nil
}

func namespaceMatchLength(repository, registryURL string) int {
	_, namespace := SplitRegistryURL(registryURL)
	if namespace != "" && repository != namespace && !strings.HasPrefix(repository, namespace+"/") {
		return -1
	}
	return len(namespace)
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
