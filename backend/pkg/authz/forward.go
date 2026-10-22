package authz

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
)

const HeaderAuthz = "X-Arcane-Authz"

const (
	forwardedAuthzVersion       = 1
	forwardedAuthzGzipPrefix    = "z."
	forwardedAuthzGzipThreshold = 4 << 10
	forwardedAuthzMaxSize       = 16 << 10
	forwardedAuthzMaxDecoded    = 1 << 20
)

var (
	ErrForwardedAuthzInvalid  = errors.New("invalid forwarded authorization")
	ErrForwardedAuthzTooLarge = errors.New("forwarded authorization exceeds size limit")
)

type ForwardedGrant struct {
	P []string                 `json:"p"`
	S []types.ResourceSelector `json:"s,omitempty"`
}

type ForwardedAuthz struct {
	V        int              `json:"v"`
	Actor    types.Actor      `json:"act"`
	Admin    bool             `json:"adm,omitempty"`
	Grants   []ForwardedGrant `json:"g,omitempty"`
	IssuedAt int64            `json:"iat"`
}

func EncodeForwardedAuthz(ps *PermissionSet, envID string, actor types.Actor) (string, error) {
	if ps == nil {
		return "", fmt.Errorf("%w: missing permission set", ErrForwardedAuthzInvalid)
	}
	grants, err := forwardedGrants(ps, envID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(ForwardedAuthz{
		V:        forwardedAuthzVersion,
		Actor:    actor,
		Admin:    ps.IsGlobalAdmin(),
		Grants:   grants,
		IssuedAt: time.Now().Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode forwarded authorization: %w", err)
	}
	encoded, err := encodeForwardedPayload(raw)
	if err != nil {
		return "", err
	}
	if len(encoded) > forwardedAuthzMaxSize {
		return "", fmt.Errorf("%w: %d bytes", ErrForwardedAuthzTooLarge, len(encoded))
	}
	return encoded, nil
}

func DecodeForwardedAuthz(raw, localEnvID string) (*PermissionSet, types.Actor, error) {
	if localEnvID == "" {
		return nil, types.Actor{}, fmt.Errorf("%w: missing local environment", ErrForwardedAuthzInvalid)
	}
	if len(raw) > forwardedAuthzMaxSize {
		return nil, types.Actor{}, fmt.Errorf("%w: %d bytes", ErrForwardedAuthzTooLarge, len(raw))
	}
	payload, err := decodeForwardedPayload(raw)
	if err != nil {
		return nil, types.Actor{}, err
	}
	var forwarded ForwardedAuthz
	if unmarshalErr := json.Unmarshal(payload, &forwarded); unmarshalErr != nil {
		return nil, types.Actor{}, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, unmarshalErr)
	}
	if forwarded.V != forwardedAuthzVersion {
		return nil, types.Actor{}, fmt.Errorf("%w: unsupported version %d", ErrForwardedAuthzInvalid, forwarded.V)
	}
	ps := &PermissionSet{
		Global:    make(map[string]struct{}),
		PerEnv:    make(map[string]map[string]struct{}),
		Scoped:    make(map[string]map[string][]types.ResourceSelector),
		SubjectID: forwarded.Actor.ID,
	}
	if forwarded.Admin {
		ps.AddGlobal(PermAll)
		return ps, forwarded.Actor, nil
	}
	for _, grant := range forwarded.Grants {
		perms := slices.DeleteFunc(slices.Clone(grant.P), func(p string) bool {
			return p != PermAll && !IsKnownPermission(p)
		})
		if len(grant.S) == 0 {
			ps.AddGlobal(slices.DeleteFunc(slices.Clone(perms), func(p string) bool {
				return !IsOrgLevel(p)
			})...)
			ps.AddEnv(localEnvID, slices.DeleteFunc(perms, IsOrgLevel)...)
			continue
		}
		perms = slices.DeleteFunc(perms, IsOrgLevel)
		for _, sel := range grant.S {
			if validateErr := ValidateSelector(sel); validateErr != nil {
				return nil, types.Actor{}, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, validateErr)
			}
			ps.AddScopedEnv(localEnvID, sel, perms...)
		}
	}
	return ps, forwarded.Actor, nil
}

func forwardedGrants(ps *PermissionSet, envID string) ([]ForwardedGrant, error) {
	if ps.IsGlobalAdmin() {
		return nil, nil
	}
	all, _ := ps.PermissionsForEnv(envID)
	var grants []ForwardedGrant
	index := make(map[string]int)
	for _, p := range all {
		if p != PermAll && !IsKnownPermission(p) || IsOrgLevel(p) && !ps.Allows(p, "") {
			continue
		}
		selectors, key, err := canonicalSelectors(ps.Scope(p, envID).selectors)
		if err != nil {
			return nil, err
		}
		if i, ok := index[key]; ok {
			grants[i].P = append(grants[i].P, p)
			continue
		}
		index[key] = len(grants)
		grants = append(grants, ForwardedGrant{
			P: []string{p},
			S: selectors,
		})
	}
	return grants, nil
}

func canonicalSelectors(selectors []types.ResourceSelector) ([]types.ResourceSelector, string, error) {
	byKey := make(map[string]types.ResourceSelector, len(selectors))
	for _, sel := range selectors {
		encoded, err := json.Marshal(sel)
		if err != nil {
			return nil, "", fmt.Errorf("encode resource selector: %w", err)
		}
		byKey[string(encoded)] = sel
	}
	keys := slices.Sorted(maps.Keys(byKey))
	sorted := make([]types.ResourceSelector, len(keys))
	for i, key := range keys {
		sorted[i] = byKey[key]
	}
	return sorted, strings.Join(keys, "\n"), nil
}

func encodeForwardedPayload(raw []byte) (string, error) {
	if len(raw) <= forwardedAuthzGzipThreshold {
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return "", fmt.Errorf("compress forwarded authorization: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("compress forwarded authorization: %w", err)
	}
	return forwardedAuthzGzipPrefix + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

func decodeForwardedPayload(raw string) ([]byte, error) {
	body, gzipped := strings.CutPrefix(raw, forwardedAuthzGzipPrefix)
	data, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, err)
	}
	if !gzipped {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, forwardedAuthzMaxDecoded+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, err)
	}
	if len(out) > forwardedAuthzMaxDecoded {
		return nil, ErrForwardedAuthzTooLarge
	}
	return out, nil
}
