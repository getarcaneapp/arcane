<script lang="ts">
	import { goto } from '$app/navigation';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ShieldAlertIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { hasPermission } from '#lib/utils/auth.js';

	import RolesTable from './components/roles-table.svelte';

	let { data } = $props();

	const canCreateRoles = $derived(hasPermission(PERMISSIONS.ROLES_CREATE));

	let roles = $derived(data.roles);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.rolesRequestOptions);

	async function refreshRoles() {
		roles = await roleService.getRoles(requestOptions);
	}

	const actionButtons: ActionButton[] = $derived.by(() =>
		!canCreateRoles
			? []
			: [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_create_button({ resource: m.common_role() }),
						onclick: () => goto('/settings/roles/new')
					}
				]
	);
</script>

<SettingsPageLayout
	title={m.roles_title()}
	description={m.roles_subtitle()}
	icon={ShieldAlertIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<RolesTable bind:roles bind:selectedIds bind:requestOptions onRolesChanged={refreshRoles} />
	{/snippet}
</SettingsPageLayout>
