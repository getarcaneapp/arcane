import { redirect } from '@sveltejs/kit';

import { PERMISSIONS } from '#lib/permissions.generated.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { environmentHref, resolveDetailEnvironmentId } from '#lib/utils/environments.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient, user } = await parent();
	const envId = await resolveDetailEnvironmentId(url);
	const containerId = params.containerId;

	if (!userHasPermission(user, PERMISSIONS.CONTAINERS_EDIT, envId)) {
		redirect(302, environmentHref(`/containers/${containerId}`, envId));
	}

	const editConfig = await queryClient.query({
		queryKey: queryKeys.containers.editConfig(envId, containerId),
		queryFn: () => containerService.getContainerEditConfig(containerId, envId)
	});

	// Compose-managed containers are edited via their project; the running
	// Arcane server or agent container cannot be edited at all.
	if (editConfig.isCompose || editConfig.editDisabled) {
		redirect(302, environmentHref(`/containers/${containerId}`, envId));
	}

	return { editConfig, envId, containerId };
};
