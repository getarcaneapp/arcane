package mldsajose

import (
	"crypto/mldsa"
	"encoding/base64"
	"encoding/json/v2"
	"strings"

	"emperror.dev/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

const (
	AlgMLDSA44 = "ML-DSA-44"
	AlgMLDSA65 = "ML-DSA-65"
	AlgMLDSA87 = "ML-DSA-87"
	KeyTypeAKP = "AKP"

	ErrInvalidKeyType = errors.Sentinel("mldsajose: invalid key type")
	ErrVerification   = errors.Sentinel("mldsajose: verification error")
)

var Parameters = mldsa.MLDSA87()

func Algorithms() []string {
	return []string{AlgMLDSA44, AlgMLDSA65, AlgMLDSA87}
}

func SupportedSigningAlgs() []string {
	return append([]string{
		oidc.RS256, oidc.RS384, oidc.RS512,
		oidc.ES256, oidc.ES384, oidc.ES512,
		oidc.PS256, oidc.PS384, oidc.PS512,
		oidc.EdDSA,
	}, Algorithms()...)
}

func IsAlg(alg string) bool {
	_, ok := ParametersForAlg(alg)
	return ok
}

func ParametersForAlg(alg string) (mldsa.Parameters, bool) {
	switch alg {
	case AlgMLDSA44:
		return mldsa.MLDSA44(), true
	case AlgMLDSA65:
		return mldsa.MLDSA65(), true
	case AlgMLDSA87:
		return mldsa.MLDSA87(), true
	}
	return mldsa.Parameters{}, false
}

type Key struct {
	KeyID  string
	Alg    string
	Public *mldsa.PublicKey
}

func ParseJWKS(body []byte) ([]Key, error) {
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Pub string `json:"pub"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, errors.WrapIf(err, "failed to parse jwks")
	}

	var keys []Key
	for _, entry := range set.Keys {
		if entry.Kty != KeyTypeAKP {
			continue
		}
		params, ok := ParametersForAlg(entry.Alg)
		if !ok {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(entry.Pub)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid AKP key %q", entry.Kid)
		}
		public, err := mldsa.NewPublicKey(params, raw)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid AKP key %q", entry.Kid)
		}
		keys = append(keys, Key{KeyID: entry.Kid, Alg: entry.Alg, Public: public})
	}
	return keys, nil
}

func VerifyCompact(token string, keys []Key) ([]byte, error) {
	jws, err := jose.ParseSignedCompact(token, joseAlgorithmsInternal(Algorithms()))
	if err != nil {
		return nil, errors.WrapIf(err, "malformed jws")
	}
	if len(jws.Signatures) != 1 {
		return nil, errors.New("expected exactly one signature")
	}
	sig := jws.Signatures[0]

	dot := strings.LastIndexByte(token, '.')
	if dot < 0 {
		return nil, errors.New("malformed compact jws")
	}
	signingInput := []byte(token[:dot])

	for _, key := range keys {
		if key.Alg != sig.Header.Algorithm || (sig.Header.KeyID != "" && key.KeyID != sig.Header.KeyID) {
			continue
		}
		if mldsa.Verify(key.Public, signingInput, sig.Signature, nil) == nil {
			return jws.UnsafePayloadWithoutVerification(), nil
		}
	}
	return nil, ErrVerification
}

func joseAlgorithmsInternal(algs []string) []jose.SignatureAlgorithm {
	out := make([]jose.SignatureAlgorithm, 0, len(algs))
	for _, alg := range algs {
		out = append(out, jose.SignatureAlgorithm(alg))
	}
	return out
}

type SigningMethodMLDSA struct {
	params mldsa.Parameters
	alg    string
}

var SigningMethodMLDSA87 = &SigningMethodMLDSA{params: mldsa.MLDSA87(), alg: AlgMLDSA87}

func init() {
	jwt.RegisterSigningMethod(AlgMLDSA87, func() jwt.SigningMethod { return SigningMethodMLDSA87 })
}

func (m *SigningMethodMLDSA) Alg() string {
	return m.alg
}

func (m *SigningMethodMLDSA) Sign(signingString string, key any) ([]byte, error) {
	sk, ok := key.(*mldsa.PrivateKey)
	if !ok || sk.PublicKey().Parameters() != m.params {
		return nil, ErrInvalidKeyType
	}
	return sk.Sign(nil, []byte(signingString), nil)
}

func (m *SigningMethodMLDSA) Verify(signingString string, sig []byte, key any) error {
	pk, ok := key.(*mldsa.PublicKey)
	if !ok || pk.Parameters() != m.params {
		return ErrInvalidKeyType
	}
	if len(sig) != m.params.SignatureSize() {
		return ErrVerification
	}
	if err := mldsa.Verify(pk, []byte(signingString), sig, nil); err != nil {
		return ErrVerification
	}
	return nil
}
