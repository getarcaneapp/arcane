package registry

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/containerd/errdefs"
	"github.com/google/go-containerregistry/pkg/name"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

type (
	registrySchemeTransport struct {
		inner  http.RoundTripper
		scheme string
		host   string
	}
)

const (
	defaultDockerRegistryHost = "registry-1.docker.io"
)

var (
	ecrRegistryEndpointPattern = regexp.MustCompile(`^(https://)?[0-9]{12}\.(dkr\.ecr(-fips)?\.[a-z0-9-]+\.amazonaws\.com(\.cn)?|dkr-ecr(-fips)?\.[a-z0-9-]+\.on\.aws)(:443)?/?$`)
	registryRetryStatusCodes   = []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		499,
		522,
	}
	distributionUnsupportedIndicators = []string{
		"not implemented",
		"status: 501",
		"status 501",
		"unsupported",
		"distribution disabled",
		"distribution api",
		"page not found",
	}
)

func (t registrySchemeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.EqualFold(req.URL.Host, t.host) && req.URL.Scheme != t.scheme {
		cloned := req.Clone(req.Context())
		cloned.URL.Scheme = t.scheme // keep: http.Request is only copyable via Clone
		return t.inner.RoundTrip(cloned)
	}
	return t.inner.RoundTrip(req)
}

func validateECRRegistryEndpoint(registryURL string, insecure bool) error {
	// Match the whole address so credentials cannot reach a user-controlled host.
	if !ecrRegistryEndpointPattern.MatchString(strings.TrimSpace(registryURL)) {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "url", Err: errors.New("ECR registry URL must be an AWS private ECR endpoint using HTTPS")})
	}
	if insecure {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "insecure", Err: errors.New("ECR registries require TLS verification")})
	}
	return nil
}

func isDistributionInspectUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	if errdefs.IsNotImplemented(err) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return slices.ContainsFunc(distributionUnsupportedIndicators, func(indicator string) bool {
		return strings.Contains(lower, indicator)
	})
}

func isRetryableRegistryTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	if temp, ok := errors.AsType[interface {
		error
		Temporary() bool
	}](err); ok && temp.Temporary() {
		return true
	}
	return false
}

func normalizeImageConfigReference(imageRef string) (name.Reference, error) {
	trimmed := strings.TrimSpace(imageRef)
	parsed, err := name.ParseReference(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid image reference %q: %w", trimmed, err)
	}
	if parsed.Context().RegistryStr() != name.DefaultRegistry {
		return parsed, nil
	}
	repoPath := defaultDockerRegistryHost + "/" + parsed.Context().RepositoryStr()
	if digestRef, ok := parsed.(name.Digest); ok {
		return name.NewDigest(repoPath + "@" + digestRef.DigestStr())
	}
	return name.NewTag(repoPath + ":" + parsed.Identifier())
}
