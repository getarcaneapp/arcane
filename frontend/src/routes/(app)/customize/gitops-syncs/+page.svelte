<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { toast } from 'svelte-sonner';
	import type { GitOpsSync, GitOpsSyncCreateDto, GitOpsSyncUpdateDto } from '$lib/types/gitops.type';
	import GitOpsSyncFormSheet from '$lib/components/sheets/gitops-sync-sheet.svelte';
	import SyncTable from './sync-table.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import CheckCircleIcon from '@lucide/svelte/icons/check-circle';

	let { data } = $props();

	let syncs = $state(untrack(() => data.syncs));
	let selectedIds = $state<string[]>([]);
	let isSyncDialogOpen = $state(false);
	let syncToEdit = $state<GitOpsSync | null>(null);
	let requestOptions = $state(untrack(() => data.syncRequestOptions));

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false
	});

	const activeSyncs = $derived(syncs.data?.filter((s) => s.enabled && s.autoSync).length ?? 0);
	const lastSyncCount = $derived(syncs.data?.filter((s) => s.lastSyncStatus === 'success').length ?? 0);

	async function refreshSyncs() {
		isLoading.refresh = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.getSyncs(requestOptions)),
			message: m.common_refresh_failed({ resource: m.gitops_syncs_title() }),
			setLoadingState: (value) => (isLoading.refresh = value),
			onSuccess: async (newSyncs) => {
				syncs = newSyncs;
				toast.success(m.common_refresh_success({ resource: m.gitops_syncs_title() }));
			}
		});
	}

	function openCreateSyncDialog() {
		syncToEdit = null;
		isSyncDialogOpen = true;
	}

	function openEditSyncDialog(sync: GitOpsSync) {
		syncToEdit = sync;
		isSyncDialogOpen = true;
	}

	async function handleSyncDialogSubmit(detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) {
		const { sync, isEditMode } = detail;
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && syncToEdit?.id) {
				await gitOpsSyncService.updateSync(syncToEdit.id, sync as GitOpsSyncUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_sync() }));
			} else {
				await gitOpsSyncService.createSync(sync as GitOpsSyncCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_sync() }));
			}

			syncs = await gitOpsSyncService.getSyncs(requestOptions);
			isSyncDialogOpen = false;
		} catch (error) {
			console.error('Error saving sync:', error);
			toast.error(error instanceof Error ? error.message : m.common_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	const statCards = $derived<StatCardConfig[]>([
		{
			title: m.common_total(),
			value: syncs?.pagination?.totalItems ?? 0,
			icon: RefreshCwIcon,
			iconColor: 'text-blue-500',
			class: 'border-l-4 border-l-blue-500'
		},
		{
			title: m.common_active(),
			value: activeSyncs,
			icon: ClockIcon,
			iconColor: 'text-purple-500',
			class: 'border-l-4 border-l-purple-500'
		},
		{
			title: m.common_successful(),
			value: lastSyncCount,
			icon: CheckCircleIcon,
			iconColor: 'text-green-500',
			class: 'border-l-4 border-l-green-500'
		}
	]);

	const actionButtons: ActionButton[] = [
		{
			id: 'create',
			action: 'create',
			label: m.common_add_button({ resource: m.resource_sync_cap() }),
			onclick: openCreateSyncDialog
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshSyncs,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	];
</script>

<ResourcePageLayout
	title={m.gitops_syncs_title()}
	subtitle={m.gitops_syncs_subtitle()}
	{actionButtons}
	{statCards}
	statCardsColumns={3}
>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Card.Root class="flex flex-col gap-6 border py-3 shadow-sm">
				<SyncTable bind:syncs bind:selectedIds bind:requestOptions onEditSync={openEditSyncDialog} />
			</Card.Root>
		</div>
	{/snippet}

	{#snippet additionalContent()}
		<GitOpsSyncFormSheet
			bind:open={isSyncDialogOpen}
			bind:syncToEdit
			onSubmit={handleSyncDialogSubmit}
			isLoading={isLoading.create || isLoading.edit}
		/>
	{/snippet}
</ResourcePageLayout>
