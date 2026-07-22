package docker

import (
	"net/netip"
	"strings"

	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// NewNetworkCreateOptions converts API network options to Docker SDK options.
func NewNetworkCreateOptions(o networktypes.CreateOptions) client.NetworkCreateOptions {
	opts := client.NetworkCreateOptions{
		Driver:     o.Driver,
		Internal:   o.Internal,
		Attachable: o.Attachable,
		Ingress:    o.Ingress,
		EnableIPv6: boolPtrIfTrue(o.EnableIPv6),
		Options:    o.Options,
		Labels:     o.Labels,
	}

	if o.IPAM != nil {
		opts.IPAM = toDockerIPAM(o.IPAM)
	}

	return opts
}

func boolPtrIfTrue(v bool) *bool {
	if !v {
		return nil
	}

	return &v
}

func toDockerIPAM(src *networktypes.IPAM) *network.IPAM {
	dockerIPAM := &network.IPAM{
		Driver:  src.Driver,
		Options: src.Options,
	}

	for _, cfg := range src.Config {
		if dockerCfg, ok := toDockerIPAMConfig(cfg); ok {
			dockerIPAM.Config = append(dockerIPAM.Config, dockerCfg)
		}
	}

	return dockerIPAM
}

func toDockerIPAMConfig(cfg networktypes.IPAMConfig) (network.IPAMConfig, bool) {
	dockerCfg := network.IPAMConfig{}
	hasAny := false

	if parsed, ok := parsePrefix(cfg.Subnet); ok {
		dockerCfg.Subnet = parsed
		hasAny = true
	}
	if parsed, ok := parseAddr(cfg.Gateway); ok {
		dockerCfg.Gateway = parsed
		hasAny = true
	}
	if parsed, ok := parsePrefix(cfg.IPRange); ok {
		dockerCfg.IPRange = parsed
		hasAny = true
	}
	if aux := parseAuxAddress(cfg.AuxAddress); len(aux) > 0 {
		dockerCfg.AuxAddress = aux
		hasAny = true
	}

	return dockerCfg, hasAny
}

func parsePrefix(raw string) (netip.Prefix, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return netip.Prefix{}, false
	}

	return prefix, true
}

func parseAddr(raw string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, false
	}

	return addr, true
}

func parseAuxAddress(auxAddress map[string]string) map[string]netip.Addr {
	if len(auxAddress) == 0 {
		return nil
	}

	aux := make(map[string]netip.Addr, len(auxAddress))
	for key, rawAddr := range auxAddress {
		if parsed, ok := parseAddr(rawAddr); ok {
			aux[key] = parsed
		}
	}

	return aux
}

// NewNetworkSummary creates an API network summary from a Docker network summary.
func NewNetworkSummary(s network.Summary) networktypes.Summary {
	return networktypes.Summary{
		ID:      s.ID,
		Name:    s.Name,
		Driver:  s.Driver,
		Scope:   s.Scope,
		Created: s.Created,
		Options: s.Options,
		Labels:  s.Labels,
		// InUse is computed by service-layer usage checks that include container attachments.
		InUse: false,
		// Keep this narrower than IsDefaultNetwork: ingress is not marked as a default in the API DTO mapping.
		IsDefault: s.Name == "bridge" || s.Name == "host" || s.Name == "none",
	}
}
