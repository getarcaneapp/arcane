<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { dev } from '$app/env';
	import { page } from '$app/state';
	import NewEnvironmentSheet from '#lib/components/sheets/new-environment-sheet.svelte';
	import EnvironmentTable from './environment-table.svelte';
	import { m } from '#lib/paraglide/messages';
	import { environmentManagementService } from '#lib/services/env-mgmt-service';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { simpleRefresh } from '#lib/utils/api';
	import { hasPermission } from '#lib/utils/auth';
	import { DownloadIcon, UpdateIcon } from '#lib/icons';
	import UpdateAllDialog from '#lib/components/dialogs/update-all-dialog.svelte';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions';

	let { data } = $props();

	let environments = $derived(data.environments);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived(data.environmentRequestOptions);
	let showEnvironmentSheet = $state(false);
	let showUpdateAllDialog = $state(false);
	let isLoading = $state({ refresh: false, creating: false, deleting: false });

	async function refresh() {
		await simpleRefresh(
			() => environmentManagementService.getEnvironments(requestOptions),
			(data) => (environments = data),
			m.common_refresh_failed({ resource: m.environments_title() }),
			(v) => (isLoading.refresh = v)
		);
	}

	const canCreateEnvironment = $derived(hasPermission('environments:create'));
	const canDeleteEnvironment = $derived(hasPermission('environments:delete'));
	const canUpdateEnvironments = $derived(hasPermission('system:upgrade'));

	function handleBulkDelete(ids: string[] = selectedIds) {
		if (ids.length === 0) return;
		const environmentIds = [...ids];

		bulkConfirmAndRun({
			ids: environmentIds,
			title: m.environments_remove_selected_title({ count: environmentIds.length }),
			message: m.environments_remove_selected_message(),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: (id) => environmentManagementService.delete(id),
			messages: {
				success: (count) => m.common_bulk_remove_success({ count, resource: m.environments_title() }),
				partial: (success, total, failed) =>
					m.common_bulk_remove_partial({ success, total, failed, resource: m.environments_title() }),
				failure: () => m.common_bulk_remove_failed({ count: environmentIds.length, resource: m.environments_title() })
			},
			setLoading: (loading) => (isLoading.deleting = loading),
			onComplete: async ({ success }) => {
				if (success === 0) return;
				await refresh();
				await environmentStore.initialize(environments.data);
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	// Dev-only: `?updateAllDemo=1` opens the update-all dialog against a scripted fake
	// fleet, so its progress and result states can be reviewed under `just dev` without
	// updating anything. `dev` is compiled out of production builds.
	const updateAllDemo = $derived(dev && page.url.searchParams.get('updateAllDemo') === '1');
	$effect(() => {
		if (updateAllDemo) showUpdateAllDialog = true;
	});

	async function onEnvironmentCreated() {
		showEnvironmentSheet = false;
		environments = await environmentManagementService.getEnvironments(requestOptions);
		await environmentStore.initialize(environments.data);
		toast.success(m.common_create_success({ resource: m.resource_environment() }));
	}

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0 && canDeleteEnvironment
			? [
					{
						id: 'remove-selected',
						action: 'remove' as const,
						label: m.common_remove_selected(),
						onclick: handleBulkDelete,
						loading: isLoading.deleting,
						disabled: isLoading.deleting
					}
				]
			: []),
		...(canCreateEnvironment
			? [
					{
						id: 'create',
						action: 'create' as const,
						label: m.common_add_button({ resource: m.resource_environment_cap() }),
						onclick: () => (showEnvironmentSheet = true)
					}
				]
			: []),
		...(canCreateEnvironment && data.settings?.edgeMTLSManagerCAAvailable
			? [
					{
						id: 'download-edge-ca',
						action: 'save' as const,
						label: m.environments_download_edge_ca(),
						href: '/api/edge-mtls/ca',
						rel: 'external',
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
		{
			id: 'refresh',
			action: 'restart' as const,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);
</script>

<ResourcePageLayout title={m.environments_title()} subtitle={m.environments_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<EnvironmentTable
			bind:environments
			bind:selectedIds
			bind:requestOptions
			onDeleteSelected={handleBulkDelete}
			bulkDeleteLoading={isLoading.deleting}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<NewEnvironmentSheet bind:open={showEnvironmentSheet} {onEnvironmentCreated} />
		<UpdateAllDialog bind:open={showUpdateAllDialog} debugDemo={updateAllDemo} onFinished={refresh} />
	{/snippet}
</ResourcePageLayout>
