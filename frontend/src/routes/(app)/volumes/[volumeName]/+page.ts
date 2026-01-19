import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { volumeService } from '$lib/services/volume-service';
import { containerService } from '$lib/services/container-service';

export interface VolumeContainerInfo {
	id: string;
	name: string;
	mountPath: string;
	isRunning: boolean;
}

export const load: PageLoad = async ({ params }) => {
	const { volumeName } = params;

	try {
		const volume = await volumeService.getVolume(volumeName);

		let containersDetailed: VolumeContainerInfo[] = [];
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
						const isRunning = c?.state?.running === true;
						
						// Find the mount path for this volume
						let mountPath = '/';
						if (c?.mounts) {
							const mount = c.mounts.find(
								(m: any) => m.name === volumeName || m.source?.includes(volumeName)
							);
							if (mount?.destination) {
								mountPath = mount.destination;
							}
						}
						
						return { id: idVal, name: nameVal, mountPath, isRunning };
					} catch {
						return { id, name: id.substring(0, 12), mountPath: '/', isRunning: false };
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
