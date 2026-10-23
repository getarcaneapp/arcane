<script lang="ts">
	import { dev } from '$app/env';
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import UpdateAllDialog from '#lib/components/dialogs/update-all-dialog.svelte';
	import NewEnvironmentSheet from '#lib/components/sheets/new-environment-sheet.svelte';
	import { DownloadIcon, UpdateIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { GLOBAL_SCOPE } from '#lib/types/auth.js';
	import { simpleRefresh } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import { hasShownPermission } from '#lib/utils/environments.js';

	import EnvironmentTable from './components/environment-table.svelte';

	let { data } = $props();

	let environments = $derived(data.environments);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived(data.environmentRequestOptions);
	let showEnvironmentSheet = $state(false);
	let environmentSession = $state(0);
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

	const connectivityFingerprint = $derived(
		environmentStore.available
			.map((environment) => `${environment.id}:${environment.status}:${environment.connected}:${!!environment.lastPollAt}`)
			.join('|')
	);
	let lastConnectivityFingerprint: string | undefined;
	$effect(() => {
		const fingerprint = connectivityFingerprint;
		const changed = untrack(() => {
			const differs = lastConnectivityFingerprint !== undefined && lastConnectivityFingerprint !== fingerprint;
			lastConnectivityFingerprint = fingerprint;
			return differs;
		});
		if (!changed) return;
		const timer = setTimeout(() => void refresh(), 1000);
		return () => clearTimeout(timer);
	});

	const canCreateEnvironment = $derived(hasPermission('environments:create', GLOBAL_SCOPE));
	const canDeleteEnvironment = $derived(hasPermission('environments:delete', GLOBAL_SCOPE));
	const canUpdateEnvironments = $derived(hasShownPermission('system:upgrade'));

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
	// set of environments, so its progress and result states can be reviewed under `just dev` without
	// updating anything. `dev` is compiled out of production builds.
	const updateAllDemo = $derived(dev && page.url.searchParams.get('updateAllDemo') === '1');
	afterNavigate(() => {
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
						group: 'danger' as const,
						destructive: true,
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
						placement: 'primary' as const,
						label: m.common_add_button({ resource: m.resource_environment_cap() }),
						onclick: () => {
							environmentSession += 1;
							showEnvironmentSheet = true;
						}
					}
				]
			: []),
		...(canCreateEnvironment && settingsStore.runtime.edgeMTLSManagerCAAvailable
			? [
					{
						id: 'download-edge-ca',
						action: 'save' as const,
						label: m.environments_download_edge_ca(),
						href: '/api/edge/mtls/ca',
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
			action: 'refresh' as const,
			placement: 'secondary',
			iconOnly: true,
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
		{#if environmentSession > 0}
			{#key environmentSession}
				<NewEnvironmentSheet bind:open={showEnvironmentSheet} {onEnvironmentCreated} />
			{/key}
		{/if}
		<UpdateAllDialog bind:open={showUpdateAllDialog} debugDemo={updateAllDemo} onFinished={refresh} />
	{/snippet}
</ResourcePageLayout>
