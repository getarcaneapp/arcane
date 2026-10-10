package registry

import (
	"errors"
	"regexp"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

var ecrRegistryEndpointPattern = regexp.MustCompile(`^(https://)?[0-9]{12}\.(dkr\.ecr(-fips)?\.[a-z0-9-]+\.amazonaws\.com(\.cn)?|dkr-ecr(-fips)?\.[a-z0-9-]+\.on\.aws)(:443)?/?$`)

func validateECRRegistryEndpoint(registryURL string, insecure bool) error {
	// Match the whole address so credentials cannot reach a user-controlled host.
	if !ecrRegistryEndpointPattern.MatchString(strings.TrimSpace(registryURL)) {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "url", Err: errors.New("ECR registry URL must be an AWS private ECR endpoint using HTTPS")})
	}
	if insecure {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "insecure", Err: errors.New("ECR registries require TLS verification")})
	}
	return nil
}
