<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { m } from '#lib/paraglide/messages.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { Role, RoleCreate, RoleWritable } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { goto } from '#lib/utils/base-path.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RoleEditorPage from '../components/role-editor-page.svelte';

	let { data } = $props();

	let isLoading = $state(false);

	async function handleSubmit(payload: Pick<Role, RoleWritable>) {
		isLoading = true;
		const safeName = payload.name.trim() || data.role.name || m.common_unknown();
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				roleService.update(data.role.id, changedFields({ ...data.role, description: data.role.description ?? null }, payload))
			),
			message: m.common_update_failed({ resource: `${m.resource_role()} "${safeName}"` }),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async () => {
				toast.success(m.common_update_success({ resource: `${m.resource_role()} "${safeName}"` }));
				await goto('/settings/roles');
			}
		});
	}

	async function handleClone() {
		isLoading = true;
		const clonePayload: RoleCreate = {
			name: `${data.role.name} (copy)`,
			description: data.role.description,
			permissions: [...data.role.permissions]
		};

		const result = await tryCatch(roleService.create(clonePayload));
		await handleApiResultWithCallbacks({
			result,
			message: m.roles_clone_failed(),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async (newRole) => {
				toast.success(m.roles_clone_success({ name: newRole.name }));
				await goto(`/settings/roles/${newRole.id}`);
			}
		});
	}
</script>

<RoleEditorPage
	title={m.roles_edit_title()}
	role={data.role}
	manifest={data.permissionsManifest}
	{isLoading}
	onSubmit={handleSubmit}
	onClone={handleClone}
/>
