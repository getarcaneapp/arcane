<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { goto } from '$app/navigation';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import type { CreateRole } from '$lib/types/auth';
	import { m } from '$lib/paraglide/messages';
	import { roleService } from '$lib/services/role-service';
	import RoleEditorPage from '../role-editor-page.svelte';

	let { data } = $props();

	let isLoading = $state(false);

	async function handleSubmit(payload: CreateRole) {
		isLoading = true;
		const safeName = payload.name?.trim() || m.common_unknown();
		handleApiResultWithCallbacks({
			result: await tryCatch(roleService.create(payload)),
			message: m.common_create_failed({ resource: `${m.resource_role()} "${safeName}"` }),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_role()} "${safeName}"` }));
				await goto('/settings/roles');
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
