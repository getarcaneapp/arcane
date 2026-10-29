<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { m } from '#lib/paraglide/messages.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { RoleCreate } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { goto } from '#lib/utils/base-path.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RoleEditorPage from '../components/role-editor-page.svelte';

	let { data } = $props();

	let isLoading = $state(false);

	async function handleSubmit(payload: RoleCreate) {
		isLoading = true;
		const safeName = payload.name?.trim() || m.common_unknown();
		await handleApiResultWithCallbacks({
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
