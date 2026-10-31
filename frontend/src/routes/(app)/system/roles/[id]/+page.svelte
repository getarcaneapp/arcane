<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { Role, RoleCreate, RoleWritable } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RoleEditorPage from '../components/role-editor-page.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

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
				await queryClient.invalidateQueries({ queryKey: queryKeys.roles.root });
				toast.success(m.common_update_success({ resource: `${m.resource_role()} "${safeName}"` }));
				await goto(resolve('/(app)/system/roles'));
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
				await queryClient.invalidateQueries({ queryKey: queryKeys.roles.root });
				toast.success(m.roles_clone_success({ name: newRole.name }));
				await goto(resolve('/(app)/system/roles/[id]', { id: newRole.id }));
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
