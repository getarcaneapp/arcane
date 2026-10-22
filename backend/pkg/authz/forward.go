package authz

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
)

const HeaderAuthz = "X-Arcane-Authz"

const (
	forwardedAuthzVersion = 1
	forwardedAuthzMaxSize = 16 << 10
)

var (
	ErrForwardedAuthzInvalid  = errors.New("invalid forwarded authorization")
	ErrForwardedAuthzTooLarge = errors.New("forwarded authorization exceeds size limit")
)

type ForwardedAuthz struct {
	V        int         `json:"v"`
	Actor    types.Actor `json:"act"`
	Admin    bool        `json:"adm,omitempty"`
	Perms    []string    `json:"p,omitempty"`
	IssuedAt int64       `json:"iat"`
}

func EncodeForwardedAuthz(ps *PermissionSet, envID string, actor types.Actor) (string, error) {
	if ps == nil {
		return "", fmt.Errorf("%w: missing permission set", ErrForwardedAuthzInvalid)
	}
	raw, err := json.Marshal(ForwardedAuthz{
		V:        forwardedAuthzVersion,
		Actor:    actor,
		Admin:    ps.IsGlobalAdmin(),
		Perms:    forwardedPermissions(ps, envID),
		IssuedAt: time.Now().Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode forwarded authorization: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeForwardedAuthz(raw, localEnvID string) (*PermissionSet, types.Actor, error) {
	if localEnvID == "" {
		return nil, types.Actor{}, fmt.Errorf("%w: missing local environment", ErrForwardedAuthzInvalid)
	}
	if len(raw) > forwardedAuthzMaxSize {
		return nil, types.Actor{}, fmt.Errorf("%w: %d bytes", ErrForwardedAuthzTooLarge, len(raw))
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, types.Actor{}, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, err)
	}
	var forwarded ForwardedAuthz
	if unmarshalErr := json.Unmarshal(payload, &forwarded); unmarshalErr != nil {
		return nil, types.Actor{}, fmt.Errorf("%w: %w", ErrForwardedAuthzInvalid, unmarshalErr)
	}
	if forwarded.V != forwardedAuthzVersion {
		return nil, types.Actor{}, fmt.Errorf("%w: unsupported version %d", ErrForwardedAuthzInvalid, forwarded.V)
	}
	ps := NewPermissionSet()
	if forwarded.Admin {
		ps.AddGlobal(PermAll)
		return ps, forwarded.Actor, nil
	}
	perms := slices.DeleteFunc(forwarded.Perms, func(p string) bool {
		return p != PermAll && !IsKnownPermission(p)
	})
	ps.AddGlobal(slices.DeleteFunc(slices.Clone(perms), func(p string) bool {
		return !IsOrgLevel(p)
	})...)
	ps.AddEnv(localEnvID, slices.DeleteFunc(perms, IsOrgLevel)...)
	return ps, forwarded.Actor, nil
}

func forwardedPermissions(ps *PermissionSet, envID string) []string {
	if ps.IsGlobalAdmin() {
		return nil
	}
	return slices.DeleteFunc(ps.PermissionsForEnv(envID), func(p string) bool {
		return p != PermAll && !IsKnownPermission(p) || IsOrgLevel(p) && !ps.Allows(p, "")
	})
}
