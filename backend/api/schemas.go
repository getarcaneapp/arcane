package api

import (
	"reflect"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/swarm"
)

type (
	dockerMountDriver                  mount.Driver
	dockerNetworkConfigReference       network.ConfigReference
	dockerRegistryServiceConfig        registry.ServiceConfig
	dockerSwarmConfigReference         swarm.ConfigReference
	dockerSwarmDiscreteGenericResource swarm.DiscreteGenericResource
	dockerSwarmDriver                  swarm.Driver
	dockerSwarmGenericResource         swarm.GenericResource
	dockerSwarmPlacement               swarm.Placement
	dockerSwarmResources               swarm.Resources
	dockerSwarmRestartPolicy           swarm.RestartPolicy
	dockerSwarmUpdateConfig            swarm.UpdateConfig
)

var schemaTypeAliases = map[reflect.Type]reflect.Type{
	reflect.TypeFor[mount.Driver]():                  reflect.TypeFor[dockerMountDriver](),
	reflect.TypeFor[network.ConfigReference]():       reflect.TypeFor[dockerNetworkConfigReference](),
	reflect.TypeFor[registry.ServiceConfig]():        reflect.TypeFor[dockerRegistryServiceConfig](),
	reflect.TypeFor[swarm.ConfigReference]():         reflect.TypeFor[dockerSwarmConfigReference](),
	reflect.TypeFor[swarm.DiscreteGenericResource](): reflect.TypeFor[dockerSwarmDiscreteGenericResource](),
	reflect.TypeFor[swarm.Driver]():                  reflect.TypeFor[dockerSwarmDriver](),
	reflect.TypeFor[swarm.GenericResource]():         reflect.TypeFor[dockerSwarmGenericResource](),
	reflect.TypeFor[swarm.Placement]():               reflect.TypeFor[dockerSwarmPlacement](),
	reflect.TypeFor[swarm.Resources]():               reflect.TypeFor[dockerSwarmResources](),
	reflect.TypeFor[swarm.RestartPolicy]():           reflect.TypeFor[dockerSwarmRestartPolicy](),
	reflect.TypeFor[swarm.UpdateConfig]():            reflect.TypeFor[dockerSwarmUpdateConfig](),
}
