import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import { userHasPermission } from '#lib/utils/auth';
import { containerService } from '#lib/services/container-service';
import { queryKeys } from '#lib/query/query-keys';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient, user } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const containerId = params.containerId;

	if (!userHasPermission(user, 'containers:edit', envId)) {
		redirect(302, `/containers/${containerId}`);
	}

	const editConfig = await queryClient.fetchQuery({
		queryKey: queryKeys.containers.editConfig(envId, containerId),
		queryFn: () => containerService.getContainerEditConfig(containerId, envId)
	});

	// Compose-managed containers are edited via their project; self-managed
	// (Arcane server) containers cannot be edited at all.
	if (editConfig.isCompose || editConfig.editDisabled) {
		redirect(302, `/containers/${containerId}`);
	}

	return { editConfig, envId, containerId };
};
