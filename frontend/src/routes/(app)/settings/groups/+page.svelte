<script lang="ts">
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import GroupFormSheet from '#lib/components/sheets/group-form-sheet.svelte';
	import { UsersIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { groupService } from '#lib/services/group-service.js';
	import type { GroupCreate } from '#lib/types/auth.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import GroupsTable from './components/groups-table.svelte';

	let { data } = $props();

	let groups = $derived(data.groups);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.groupsRequestOptions);
	let sheetOpen = $state(false);
	let isCreating = $state(false);

	async function refreshGroups() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(groupService.getGroups(requestOptions)),
			message: m.common_refresh_failed({ resource: m.groups_title() }),
			onSuccess: (page) => {
				groups = page;
			}
		});
	}

	async function createGroup(group: Required<GroupCreate>) {
		const safeName = group.name || m.common_unknown();
		await handleApiResultWithCallbacks({
			result: await tryCatch(groupService.create(group)),
			message: m.common_create_failed({ resource: `${m.resource_group()} "${safeName}"` }),
			setLoadingState: (value) => (isCreating = value),
			onSuccess: async (created) => {
				toast.success(m.common_create_success({ resource: `${m.resource_group_cap()} "${safeName}"` }));
				sheetOpen = false;
				await goto(`/settings/groups/${created.id}`);
			}
		});
	}

	const actionButtons: ActionButton[] = $derived.by(() =>
		hasPermission(PERMISSIONS.GROUPS_CREATE)
			? [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_create_button({ resource: m.resource_group_cap() }),
						onclick: () => (sheetOpen = true),
						loading: isCreating,
						disabled: isCreating
					}
				]
			: []
	);
</script>

<SettingsPageLayout
	title={m.groups_title()}
	description={m.groups_subtitle()}
	icon={UsersIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<GroupsTable bind:groups bind:selectedIds bind:requestOptions onGroupsChanged={refreshGroups} />
	{/snippet}

	{#snippet additionalContent()}
		<GroupFormSheet bind:open={sheetOpen} groupToEdit={null} isLoading={isCreating} onSubmit={createGroup} />
	{/snippet}
</SettingsPageLayout>
