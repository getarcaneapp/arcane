import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { volumeService } from '$lib/services/volume-service';
import { containerService } from '$lib/services/container-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const { volumeName } = params;

	try {
		const volume = await queryClient.fetchQuery({
			queryKey: queryKeys.volumes.detail(envId, volumeName),
			queryFn: () => volumeService.getVolumeForEnvironment(envId, volumeName)
		});

		let containersDetailed: { id: string; name: string }[] = [];
		if (volume.containers && volume.containers.length > 0) {
			containersDetailed = await Promise.all(
				volume.containers.map(async (id: string) => {
					try {
						const c = await queryClient.fetchQuery({
							queryKey: queryKeys.containers.detail(envId, id),
							queryFn: () => containerService.getContainerForEnvironment(envId, id)
						});
						const idVal = (c?.id || c?.Id || id) as string;
						const nameVal = (c?.name ||
							c?.Name ||
							(c?.Names && c?.Names[0]?.replace?.(/^\//, '')) ||
							idVal?.substring(0, 12)) as string;
						return { id: idVal, name: nameVal };
					} catch {
						return { id, name: id.substring(0, 12) };
					}
				})
			);
		}

		return {
			volume,
			containersDetailed
		};
	} catch (err: any) {
		console.error('Failed to load volume:', err);
		if (err.status === 404) throw err;
		throw error(500, err.message || 'Failed to load volume details');
	}
};
