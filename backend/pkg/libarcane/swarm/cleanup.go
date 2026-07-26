package swarm

import (
	"context"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"emperror.dev/errors"

	cerrdefs "github.com/containerd/errdefs"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	dockerclient "github.com/moby/moby/client"
)

// Swarm tears tasks down asynchronously, so a config, secret, or network can
// stay referenced for a short window after the owning services are removed.
const staleSwarmResourceRemoveAttemptsInternal = 5

var staleSwarmResourceRemoveBackoffInternal = 500 * time.Millisecond

func cleanupStackResourcesInternal(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	stackName string,
	configMetaByKey map[string]resourceMeta,
	secretMetaByKey map[string]resourceMeta,
) error {
	desiredConfigNames := make(map[string]struct{}, len(configMetaByKey))
	for _, meta := range configMetaByKey {
		desiredConfigNames[meta.Name] = struct{}{}
	}
	if err := cleanupStaleManagedResourcesInternal(
		ctx,
		stackName,
		desiredConfigNames,
		configFileResourceAdapterInternal(dockerClient),
		true,
		true,
	); err != nil {
		return err
	}

	desiredSecretNames := make(map[string]struct{}, len(secretMetaByKey))
	for _, meta := range secretMetaByKey {
		desiredSecretNames[meta.Name] = struct{}{}
	}
	if err := cleanupStaleManagedResourcesInternal(
		ctx,
		stackName,
		desiredSecretNames,
		secretFileResourceAdapterInternal(dockerClient),
		true,
		true,
	); err != nil {
		return err
	}

	return nil
}

func collectStaleManagedResourcesInternal[T any](items []T, convert func(T) staleManagedResourceInternal) []staleManagedResourceInternal {
	resources := make([]staleManagedResourceInternal, 0, len(items))
	for _, item := range items {
		resources = append(resources, convert(item))
	}
	return resources
}

type staleManagedResourceInternal struct {
	ID   string
	Name string
}

func cleanupStaleManagedResourcesInternal(
	ctx context.Context,
	stackName string,
	desiredNames map[string]struct{},
	adapter fileResourceAdapterInternal,
	managedOnly bool,
	tolerateInUse bool,
) error {
	resourceTypeLabels := []string{""}
	if managedOnly {
		resourceTypeLabels = []string{resourceTypeLabel, legacyResourceTypeLabel}
	}

	resources := make([]staleManagedResourceInternal, 0)
	seenResourceIDs := make(map[string]struct{})
	for _, typeLabel := range resourceTypeLabels {
		filters := make(dockerclient.Filters)
		filters.Add("label", fmt.Sprintf("%s=%s", swarmtypes.StackNamespaceLabel, stackName))
		if typeLabel != "" {
			filters.Add("label", typeLabel+"="+adapter.ResourceType)
		}

		listedResources, err := adapter.List(ctx, filters)
		if err != nil {
			return err
		}
		for _, resource := range listedResources {
			if _, seen := seenResourceIDs[resource.ID]; seen {
				continue
			}
			seenResourceIDs[resource.ID] = struct{}{}
			resources = append(resources, resource)
		}
	}

	for _, resource := range resources {
		if _, ok := desiredNames[resource.Name]; ok {
			continue
		}
		if err := removeStaleSwarmResourceInternal(ctx, tolerateInUse, func(ctx context.Context) error {
			return adapter.Remove(ctx, resource.ID)
		}); err != nil {
			return errors.WrapIff(err, "failed to remove stale stack %s %s", adapter.ResourceType, resource.Name)
		}
	}

	return nil
}

// removeStaleSwarmResourceInternal removes a resource, retrying while Swarm still
// reports it as in use. When tolerateInUse is set the caller is reconciling a live
// stack and a later cleanup pass picks the resource up, so a single attempt is enough;
// otherwise the stack is being deleted and a lingering resource must surface as an error
// instead of being silently orphaned.
func removeStaleSwarmResourceInternal(ctx context.Context, tolerateInUse bool, remove func(context.Context) error) error {
	attempts := staleSwarmResourceRemoveAttemptsInternal
	if tolerateInUse {
		attempts = 1
	}

	var err error
	for attempt := range attempts {
		err = remove(ctx)
		if err == nil || cerrdefs.IsNotFound(err) {
			return nil
		}
		if !isStaleSwarmResourceStillInUseInternal(err) {
			return err
		}
		if attempt == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return stderrors.Join(ctx.Err(), err)
		case <-time.After(staleSwarmResourceRemoveBackoffInternal):
		}
	}

	if tolerateInUse {
		return nil
	}
	return err
}

// RemoveStackResources removes configs, secrets, and networks owned by a stack.
func RemoveStackResources(ctx context.Context, dockerClient *dockerclient.Client, stackName string) error {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return invalidStackErrorInternal(errors.New("stack name is required"))
	}

	emptyDesiredSet := map[string]struct{}{}
	if err := cleanupStaleManagedResourcesInternal(ctx, stackName, emptyDesiredSet, configFileResourceAdapterInternal(dockerClient), false, false); err != nil {
		return err
	}
	if err := cleanupStaleManagedResourcesInternal(ctx, stackName, emptyDesiredSet, secretFileResourceAdapterInternal(dockerClient), false, false); err != nil {
		return err
	}

	filters := make(dockerclient.Filters).Add("label", fmt.Sprintf("%s=%s", swarmtypes.StackNamespaceLabel, stackName))
	networksResult, err := dockerClient.NetworkList(ctx, dockerclient.NetworkListOptions{Filters: filters})
	if err != nil {
		return errors.WrapIf(err, "failed to list stack networks")
	}
	networks := networksResult.Items
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Name < networks[j].Name
	})
	for _, stackNetwork := range networks {
		if stackNetwork.Ingress {
			continue
		}
		if err := removeStaleSwarmResourceInternal(ctx, false, func(ctx context.Context) error {
			_, err := dockerClient.NetworkRemove(ctx, stackNetwork.ID, dockerclient.NetworkRemoveOptions{})
			return err
		}); err != nil {
			return errors.WrapIff(err, "failed to remove stack network %s", stackNetwork.Name)
		}
	}

	return nil
}

func isStaleSwarmResourceStillInUseInternal(err error) bool {
	if cerrdefs.IsConflict(err) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "in use")
}
