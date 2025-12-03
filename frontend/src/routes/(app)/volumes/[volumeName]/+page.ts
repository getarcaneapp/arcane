import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { volumeService } from '$lib/services/volume-service';
import { containerService } from '$lib/services/container-service';

export const load: PageLoad = async ({ params }) => {
	const { volumeName } = params;

	try {
		const volume = await volumeService.getVolume(volumeName);

		let containersDetailed: { id: string; name: string }[] = [];
		if (volume.containers && volume.containers.length > 0) {
			containersDetailed = await Promise.all(
				volume.containers.map(async (id: string) => {
					try {
						const c = await containerService.getContainer(id);
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
