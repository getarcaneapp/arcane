import { queryOptions } from '@tanstack/svelte-query';

import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import { networkService } from '#lib/services/network-service.js';
import { podmanService } from '#lib/services/podman-service.js';
import { volumeService } from '#lib/services/volume-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { PodmanStatus } from '#lib/types/podman.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { hasPermission } from '#lib/utils/auth.js';
import { fetchAcrossEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';

export function podmanPodListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.podman.pods(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) => podmanService.getPods(id, request, config))
	});
}

export function podmanQuadletListQuery(
	scope: string,
	environments: EnvironmentRef[],
	requestOptions: SearchPaginationSortRequest
) {
	return queryOptions({
		queryKey: queryKeys.podman.quadlets(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				podmanService.getQuadlets(id, request, config)
			)
	});
}

export function podmanSecretListQuery(
	scope: string,
	environments: EnvironmentRef[],
	requestOptions: SearchPaginationSortRequest
) {
	return queryOptions({
		queryKey: queryKeys.podman.secrets(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				podmanService.getSecrets(id, request, config)
			)
	});
}

const EMPTY_PODMAN_STATUS: PodmanStatus = {
	detected: false,
	podCount: 0,
	rootless: false,
	quadletsSupported: false,
	quadletAppsSupported: false,
	autoUpdateMode: 'unavailable',
	systemdAvailable: false
};

const LIST_ALL = { pagination: { page: 1, limit: -1 } };

export function podmanStatusQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.podman.status(environments.map((env) => env.id).join(',')),
		queryFn: () =>
			sumAcrossEnvironments(environments, (scope, config) => podmanService.getStatus(scope, config), EMPTY_PODMAN_STATUS)
	});
}

export function podmanInfoQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.podman.info(environmentId),
		queryFn: () => podmanService.getInfo(environmentId)
	});
}

export function podmanPodQuery(environmentId: string, podId: string) {
	return queryOptions({
		queryKey: queryKeys.podman.pod(environmentId, podId),
		queryFn: () => podmanService.getPod(podId, environmentId)
	});
}

export function podmanPodTopQuery(environmentId: string, podId: string) {
	return queryOptions({
		queryKey: queryKeys.podman.podTop(environmentId, podId),
		queryFn: () => podmanService.getPodTop(podId, environmentId)
	});
}

export function podmanQuadletQuery(environmentId: string, name: string) {
	return queryOptions({
		queryKey: queryKeys.podman.quadlet(environmentId, name),
		queryFn: () => podmanService.getQuadlet(name, environmentId)
	});
}

export function podmanQuadletFileQuery(environmentId: string, name: string) {
	return queryOptions({
		queryKey: queryKeys.podman.quadletFile(environmentId, name),
		queryFn: () => podmanService.getQuadletFile(name, environmentId)
	});
}

export function podmanKubeNetworksQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.podman.kubeNetworks(environmentId),
		queryFn: () => networkService.getNetworksForEnvironment(environmentId, LIST_ALL)
	});
}

export function podmanKubeGenerateSourcesQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.podman.kubeGenerateSources(environmentId),
		queryFn: async () => {
			const canListContainers = hasPermission('containers:list', environmentId);
			const canListVolumes = hasPermission('volumes:list', environmentId);
			const [pods, containers, volumes] = await Promise.all([
				hasPermission('podman-pods:list', environmentId) ? podmanService.getPods(environmentId, LIST_ALL) : { data: [] },
				canListContainers ? containerService.getContainersForEnvironment(environmentId, LIST_ALL) : { data: [] },
				canListVolumes ? volumeService.getVolumesForEnvironment(environmentId, LIST_ALL) : { data: [] }
			]);
			return [
				...pods.data.map((pod) => ({ value: pod.name, label: pod.name, hint: m.podman_pod() })),
				...containers.data.map((container) => {
					const name = container.names[0] ?? container.id;
					return { value: name, label: name, hint: m.resource_container() };
				}),
				...volumes.data.map((volume) => ({ value: volume.name, label: volume.name, hint: m.resource_volume() }))
			].filter((item, index, all) => all.findIndex((other) => other.value === item.value) === index);
		}
	});
}
