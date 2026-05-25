<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { goto } from '$app/navigation';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import RoleEditor from '$lib/components/role-editor/role-editor.svelte';
	import type { UpdateRole, CreateRole } from '$lib/types/auth';
	import { m } from '$lib/paraglide/messages';
	import { roleService } from '$lib/services/role-service';
	import { SettingsPageLayout } from '$lib/layouts/index.js';
	import { ShieldAlertIcon } from '$lib/icons';

	let { data } = $props();

	let isLoading = $state(false);

	async function handleSubmit(payload: UpdateRole) {
		isLoading = true;
		const safeName = payload.name?.trim() || data.role.name || m.common_unknown();
		handleApiResultWithCallbacks({
			result: await tryCatch(roleService.update(data.role.id, payload)),
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
		const clonePayload: CreateRole = {
			name: `${data.role.name} (copy)`,
			description: data.role.description,
			permissions: [...data.role.permissions]
		};

		const result = await tryCatch(roleService.create(clonePayload));
		handleApiResultWithCallbacks({
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

<SettingsPageLayout title={m.roles_edit_title()} description={m.roles_subtitle()} icon={ShieldAlertIcon} pageType="form">
	{#snippet mainContent()}
		<RoleEditor role={data.role} manifest={data.permissionsManifest} {isLoading} onSubmit={handleSubmit} onClone={handleClone} />
	{/snippet}
</SettingsPageLayout>
