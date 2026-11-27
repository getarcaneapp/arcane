package api

import (
	"encoding/json/jsontext"
	"reflect"

	mobyMount "github.com/moby/moby/api/types/mount"
	mobyNetwork "github.com/moby/moby/api/types/network"
	mobyRegistry "github.com/moby/moby/api/types/registry"
	mobySwarm "github.com/moby/moby/api/types/swarm"
)

type (
	dockerMountDriver                  mobyMount.Driver
	dockerNetworkConfigReference       mobyNetwork.ConfigReference
	dockerRegistryServiceConfig        mobyRegistry.ServiceConfig
	dockerSwarmConfigReference         mobySwarm.ConfigReference
	dockerSwarmDiscreteGenericResource mobySwarm.DiscreteGenericResource
	dockerSwarmDriver                  mobySwarm.Driver
	dockerSwarmGenericResource         mobySwarm.GenericResource
	dockerSwarmPlacement               mobySwarm.Placement
	dockerSwarmResources               mobySwarm.Resources
	dockerSwarmRestartPolicy           mobySwarm.RestartPolicy
	dockerSwarmUpdateConfig            mobySwarm.UpdateConfig
)

var schemaTypeAliases = map[reflect.Type]reflect.Type{
	reflect.TypeFor[jsontext.Value]():                    reflect.TypeFor[any](),
	reflect.TypeFor[mobyMount.Driver]():                  reflect.TypeFor[dockerMountDriver](),
	reflect.TypeFor[mobyNetwork.ConfigReference]():       reflect.TypeFor[dockerNetworkConfigReference](),
	reflect.TypeFor[mobyRegistry.ServiceConfig]():        reflect.TypeFor[dockerRegistryServiceConfig](),
	reflect.TypeFor[mobySwarm.ConfigReference]():         reflect.TypeFor[dockerSwarmConfigReference](),
	reflect.TypeFor[mobySwarm.DiscreteGenericResource](): reflect.TypeFor[dockerSwarmDiscreteGenericResource](),
	reflect.TypeFor[mobySwarm.Driver]():                  reflect.TypeFor[dockerSwarmDriver](),
	reflect.TypeFor[mobySwarm.GenericResource]():         reflect.TypeFor[dockerSwarmGenericResource](),
	reflect.TypeFor[mobySwarm.Placement]():               reflect.TypeFor[dockerSwarmPlacement](),
	reflect.TypeFor[mobySwarm.Resources]():               reflect.TypeFor[dockerSwarmResources](),
	reflect.TypeFor[mobySwarm.RestartPolicy]():           reflect.TypeFor[dockerSwarmRestartPolicy](),
	reflect.TypeFor[mobySwarm.UpdateConfig]():            reflect.TypeFor[dockerSwarmUpdateConfig](),
}
