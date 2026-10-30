<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ShieldAlertIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { roleListQuery } from '#lib/query/roles.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';

	import RolesTable from './components/roles-table.svelte';

	let { data } = $props();

	const isAdmin = $derived(userStore.isGlobalAdmin());

	const pageState = new ResourceListPageState(untrack(() => data.rolesRequestOptions));
	const rolesQuery = createQuery(() => ({
		...roleListQuery(pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const roles = $derived(rolesQuery.data ?? data.roles);

	const actionButtons: ActionButton[] = $derived.by(() =>
		!isAdmin
			? []
			: [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_create_button({ resource: m.common_role() }),
						onclick: () => goto(resolve('/(app)/settings/roles/new'))
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
		<RolesTable
			{roles}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => rolesQuery.refetch()}
		/>
	{/snippet}
</SettingsPageLayout>
