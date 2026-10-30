<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { RoleCreate } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RoleEditorPage from '../components/role-editor-page.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let isLoading = $state(false);

	async function handleSubmit(payload: RoleCreate) {
		isLoading = true;
		const safeName = payload.name?.trim() || m.common_unknown();
		await handleApiResultWithCallbacks({
			result: await tryCatch(roleService.create(payload)),
			message: m.common_create_failed({ resource: `${m.resource_role()} "${safeName}"` }),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async () => {
				await queryClient.invalidateQueries({ queryKey: queryKeys.roles.root });
				toast.success(m.common_create_success({ resource: `${m.resource_role()} "${safeName}"` }));
				await goto(resolve('/(app)/settings/roles'));
			}
		});
	}
</script>

<RoleEditorPage
	title={m.roles_create_title()}
	role={null}
	manifest={data.permissionsManifest}
	{isLoading}
	onSubmit={handleSubmit}
/>
