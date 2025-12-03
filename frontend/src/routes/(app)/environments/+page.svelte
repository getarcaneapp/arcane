<script lang="ts">
	import { toast } from 'svelte-sonner';
	import NewEnvironmentSheet from '$lib/components/sheets/new-environment-sheet.svelte';
	import EnvironmentTable from './environment-table.svelte';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { simpleRefresh } from '$lib/utils/refresh.util';

	let { data } = $props();

	let environments = $state(untrack(() => data.environments));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state(untrack(() => data.environmentRequestOptions));
	let showEnvironmentSheet = $state(false);
	let isLoading = $state({ refresh: false, creating: false, deleting: false });

	async function refresh() {
		await simpleRefresh(
			() => environmentManagementService.getEnvironments(requestOptions),
			(data) => (environments = data),
			m.common_refresh_failed({ resource: m.environments_title() }),
			(v) => (isLoading.refresh = v)
		);
	}

	async function handleBulkDelete() {
		if (selectedIds.length === 0) return;

		openConfirmDialog({
			title: m.environments_remove_selected_title({ count: selectedIds.length }),
			message: m.environments_remove_selected_message(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.deleting = true;
					let successCount = 0;
					let failureCount = 0;

					for (const id of selectedIds) {
						const result = await tryCatch(environmentManagementService.delete(id));
						handleApiResultWithCallbacks({
							result,
							message: m.common_bulk_remove_failed({ count: selectedIds.length, resource: m.environments_title() }),
							setLoadingState: () => {},
							onSuccess: () => { successCount += 1; }
						});
						if (result.error) failureCount += 1;
					}

					isLoading.deleting = false;
					if (successCount > 0) {
						toast.success(m.common_bulk_remove_success({ count: successCount, resource: m.environments_title() }));
						await refresh();
						await environmentStore.initialize(environments.data);
					}
					if (failureCount > 0) {
						toast.error(m.common_bulk_remove_failed({ count: failureCount, resource: m.environments_title() }));
					}
					selectedIds = [];
				}
			}
		});
	}

	async function onEnvironmentCreated() {
		showEnvironmentSheet = false;
		environments = await environmentManagementService.getEnvironments(requestOptions);
		await environmentStore.initialize(environments.data);
		toast.success(m.common_create_success({ resource: m.resource_environment() }));
	}

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0
			? [{ id: 'remove-selected', action: 'remove' as const, label: m.environments_remove_selected_button(), onclick: handleBulkDelete, loading: isLoading.deleting, disabled: isLoading.deleting }]
			: []),
		{ id: 'create', action: 'create' as const, label: m.common_add_button({ resource: m.resource_environment_cap() }), onclick: () => (showEnvironmentSheet = true) },
		{ id: 'refresh', action: 'restart' as const, label: m.common_refresh(), onclick: refresh, loading: isLoading.refresh, disabled: isLoading.refresh }
	]);
</script>

<ResourcePageLayout title={m.environments_title()} subtitle={m.environments_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<EnvironmentTable bind:environments bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<NewEnvironmentSheet bind:open={showEnvironmentSheet} {onEnvironmentCreated} />
	{/snippet}
</ResourcePageLayout>
