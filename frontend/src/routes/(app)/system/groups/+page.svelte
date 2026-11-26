<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { groupListQuery } from '#lib/query/groups.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';

	import GroupsTable from './components/groups-table.svelte';

	let { data } = $props();

	const isAdmin = $derived(userStore.isGlobalAdmin());

	const pageState = new ResourceListPageState(untrack(() => data.groupsRequestOptions));
	const groupsQuery = createQuery(() => ({
		...groupListQuery(pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const groups = $derived(groupsQuery.data ?? data.groups);

	const actionButtons: ActionButton[] = $derived.by(() => {
		if (!isAdmin) {
			return [];
		}
		return [
			{
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.common_create_button({ resource: m.group() }),
				onclick: () => goto(resolve('/(app)/system/groups/new'))
			}
		];
	});
</script>

<SettingsPageLayout title={m.groups()} description={m.groups_subtitle()} pageType="management" {actionButtons}>
	{#snippet mainContent()}
		<GroupsTable
			{groups}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => groupsQuery.refetch()}
			refreshError={groupsQuery.error}
		/>
	{/snippet}
</SettingsPageLayout>
