<script lang="ts">
	import { ShieldAlertIcon } from '#lib/icons/index.js';
	import { goto } from '$app/navigation';
	import RolesTable from './roles-table.svelte';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { m } from '#lib/paraglide/messages.js';
	import { roleService } from '#lib/services/role-service.js';
	import { SettingsPageLayout, type SettingsActionButton } from '#lib/layouts/index.js';
	import userStore from '#lib/stores/user-store.js';

	let { data } = $props();

	const isAdmin = $derived(userStore.isGlobalAdmin());

	let roles = $derived(data.roles);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.rolesRequestOptions);

	async function refreshRoles() {
		roles = await roleService.getRoles(requestOptions);
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() =>
		!isAdmin
			? []
			: [
					{
						id: 'create',
						action: 'create',
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
