package oidcjwk

import (
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/lestrrat-go/jwx/v4/jwa"
)

func Algorithms() []string {
	return []string{
		jwa.MLDSA44().String(),
		jwa.MLDSA65().String(),
		jwa.MLDSA87().String(),
	}
}

func SupportedSigningAlgs() []string {
	return append([]string{
		oidc.RS256, oidc.RS384, oidc.RS512,
		oidc.ES256, oidc.ES384, oidc.ES512,
		oidc.PS256, oidc.PS384, oidc.PS512,
		oidc.EdDSA,
	}, Algorithms()...)
}
