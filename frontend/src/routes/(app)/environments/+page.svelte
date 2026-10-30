<script lang="ts">
	import { dev } from '$app/env';
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import UpdateAllDialog from '#lib/components/dialogs/update-all-dialog.svelte';
	import NewEnvironmentSheet from '#lib/components/sheets/new-environment-sheet.svelte';
	import { DownloadIcon, SettingsIcon, UpdateIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentListQuery } from '#lib/query/environments.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { GLOBAL_SCOPE } from '#lib/types/auth.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission, isGlobalAdmin } from '#lib/utils/auth.js';
	import { downloadFromUrl } from '#lib/utils/browser-download.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import { hasShownPermission } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';

	import EnvironmentSettingsDialog from './components/environment-settings-dialog.svelte';
	import EnvironmentTable from './components/environment-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.environmentRequestOptions));
	const environmentsQuery = createQuery(() => ({
		...environmentListQuery(pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const loadedEnvironments = $derived(environmentsQuery.data ?? data.environments);
	const liveEnvironments = $derived(new Map(environmentStore.available.map((environment) => [environment.id, environment])));
	const environments = $derived({
		...loadedEnvironments,
		data: loadedEnvironments.data.map((environment) => liveEnvironments.get(environment.id) ?? environment)
	});
	let environmentSession = $state(0);
	let showUpdateAllDialog = $state(false);
	let showSettingsDialog = $state(false);
	let isDeleting = $state(false);
	const isRefreshing = $derived(environmentsQuery.isFetching && !environmentsQuery.isPending);

	async function refresh() {
		const result = await environmentsQuery.refetch();
		if (result.isError) {
			toast.error(m.common_refresh_failed({ resource: m.environments_title() }), {
				description: extractApiErrorMessage(result.error)
			});
		}
	}

	const canCreateEnvironment = $derived(hasPermission('environments:create', GLOBAL_SCOPE));
	const canDeleteEnvironment = $derived(hasPermission('environments:delete', GLOBAL_SCOPE));
	const canUpdateEnvironments = $derived(hasShownPermission('system:upgrade'));

	function handleBulkDelete(ids: string[] = pageState.selectedIds) {
		if (ids.length === 0) return;
		const environmentIds = [...ids];
		const removedIds = new Set<string>();

		bulkConfirmAndRun({
			ids: environmentIds,
			title: m.environments_remove_selected_title({ count: environmentIds.length }),
			message: m.environments_remove_selected_message(),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: async (id) => {
				await environmentManagementService.delete(id);
				removedIds.add(id);
			},
			messages: {
				success: (count) => m.common_bulk_remove_success({ count, resource: m.environments_title() }),
				partial: (success, total, failed) =>
					m.common_bulk_remove_partial({ success, total, failed, resource: m.environments_title() }),
				failure: () => m.common_bulk_remove_failed({ count: environmentIds.length, resource: m.environments_title() })
			},
			setLoading: (loading) => (isDeleting = loading),
			onComplete: async ({ success }) => {
				if (success === 0) return;
				await environmentStore.initialize(environmentStore.available.filter((environment) => !removedIds.has(environment.id)));
				await queryClient.invalidateQueries({ queryKey: queryKeys.environments.all });
			},
			clearSelection: () => (pageState.selectedIds = []),
			sequential: true
		});
	}

	// Dev-only: `?updateAllDemo=1` opens the update-all dialog against a scripted fake
	// set of environments, so its progress and result states can be reviewed under `just dev` without
	// updating anything. `dev` is compiled out of production builds.
	const updateAllDemo = $derived(dev && page.url.searchParams.get('updateAllDemo') === '1');
	afterNavigate(() => {
		if (updateAllDemo) showUpdateAllDialog = true;
	});

	function onEnvironmentCreated() {
		pageState.isCreateDialogOpen = false;
		toast.success(m.common_create_success({ resource: m.resource_environment() }));
	}

	const actionButtons: ActionButton[] = $derived([
		...(pageState.selectedIds.length > 0 && canDeleteEnvironment
			? [
					{
						id: 'remove-selected',
						action: 'remove' as const,
						group: 'danger' as const,
						destructive: true,
						label: m.common_remove_selected(),
						onclick: handleBulkDelete,
						loading: isDeleting,
						disabled: isDeleting
					}
				]
			: []),
		...(canCreateEnvironment
			? [
					{
						id: 'create',
						action: 'create' as const,
						placement: 'primary' as const,
						label: m.common_add_button({ resource: m.resource_environment_cap() }),
						onclick: () => {
							environmentSession += 1;
							pageState.isCreateDialogOpen = true;
						}
					}
				]
			: []),
		...(canCreateEnvironment && settingsStore.runtime.agentMTLSManagerCAAvailable
			? [
					{
						id: 'download-agent-ca',
						action: 'save' as const,
						label: m.environments_download_agent_ca(),
						onclick: () => downloadFromUrl('/api/agent/mtls/ca'),
						icon: DownloadIcon
					}
				]
			: []),
		...(canUpdateEnvironments
			? [
					{
						id: 'update-all',
						action: 'update' as const,
						label: m.update_all(),
						icon: UpdateIcon,
						onclick: () => (showUpdateAllDialog = true)
					}
				]
			: []),
		...(isGlobalAdmin()
			? [
					{
						id: 'settings',
						action: 'edit' as const,
						placement: 'secondary' as const,
						iconOnly: true,
						label: m.settings(),
						icon: SettingsIcon,
						onclick: () => (showSettingsDialog = true)
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'refresh' as const,
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: environmentsQuery.isFetching
		}
	]);
</script>

<ResourcePageLayout title={m.environments_title()} subtitle={m.environments_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<EnvironmentTable
			{environments}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => environmentsQuery.refetch()}
			onDeleteSelected={handleBulkDelete}
			bulkDeleteLoading={isDeleting}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#if environmentSession > 0}
			{#key environmentSession}
				<NewEnvironmentSheet bind:open={pageState.isCreateDialogOpen} {onEnvironmentCreated} />
			{/key}
		{/if}
		<UpdateAllDialog bind:open={showUpdateAllDialog} debugDemo={updateAllDemo} onFinished={refresh} />
		<EnvironmentSettingsDialog bind:open={showSettingsDialog} />
	{/snippet}
</ResourcePageLayout>
