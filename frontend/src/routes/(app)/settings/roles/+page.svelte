<script lang="ts">
	import { ShieldAlertIcon } from '$lib/icons';
	import { goto } from '$app/navigation';
	import { untrack } from 'svelte';
	import RolesTable from './roles-table.svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import { m } from '$lib/paraglide/messages';
	import { roleService } from '$lib/services/role-service';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts/index.js';
	import userStore from '$lib/stores/user-store';

	let { data } = $props();

	const isAdmin = $derived(userStore.isGlobalAdmin());

	let roles = $state(untrack(() => data.roles));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.rolesRequestOptions));

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
						label: m.common_create_button({ resource: m.resource_role_cap() }),
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
