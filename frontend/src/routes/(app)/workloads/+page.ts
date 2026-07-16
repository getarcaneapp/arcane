import { redirect } from '@sveltejs/kit';
import { canReachAccessSurface } from '$lib/utils/access-policy';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, url }) => {
	const { permissionsManifest, user } = await parent();
	const environmentId = await environmentStore.getCurrentEnvironmentId();
	const canOpenProjects =
		!permissionsManifest?.accessSurfaces?.length ||
		canReachAccessSurface(permissionsManifest, 'route.projects', user, environmentId);
	const destination = canOpenProjects ? '/workloads/projects' : '/workloads/containers';
	redirect(308, `${destination}${url.search}`);
};
