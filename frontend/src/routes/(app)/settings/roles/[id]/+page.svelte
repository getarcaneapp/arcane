<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { goto } from '$app/navigation';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import type { UpdateRole, CreateRole, Role } from '#lib/types/auth.js';
	import { m } from '#lib/paraglide/messages.js';
	import { roleService } from '#lib/services/role-service.js';
	import RoleEditorPage from '../role-editor-page.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
	// Undefined while SvelteKit hands this departing page another route's data.
	const role = $derived<Role | undefined>(data.role);

	let isLoading = $state(false);

	async function handleSubmit(payload: UpdateRole) {
		if (!role) return;
		const roleId = role.id;
		isLoading = true;
		const safeName = payload.name?.trim() || role.name || m.common_unknown();
		await handleApiResultWithCallbacks({
			result: await tryCatch(roleService.update(roleId, payload)),
			message: m.common_update_failed({ resource: `${m.resource_role()} "${safeName}"` }),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async () => {
				toast.success(m.common_update_success({ resource: `${m.resource_role()} "${safeName}"` }));
				if (role?.id === roleId) await goto('/settings/roles');
			}
		});
	}

	async function handleClone() {
		if (!role) return;
		const roleId = role.id;
		isLoading = true;
		const clonePayload: CreateRole = {
			name: `${role.name} (copy)`,
			description: role.description,
			permissions: [...role.permissions]
		};

		const result = await tryCatch(roleService.create(clonePayload));
		await handleApiResultWithCallbacks({
			result,
			message: m.roles_clone_failed(),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async (newRole) => {
				toast.success(m.roles_clone_success({ name: newRole.name }));
				if (role?.id === roleId) await goto(`/settings/roles/${newRole.id}`);
			}
		});
	}
</script>

{#if role}
	<RoleEditorPage
		title={m.roles_edit_title()}
		{role}
		manifest={data.permissionsManifest}
		{isLoading}
		onSubmit={handleSubmit}
		onClone={handleClone}
	/>
{/if}
