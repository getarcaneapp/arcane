<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { DetailMetaStrip, DetailSection, type DetailMetaItem } from '#lib/components/resource-detail/index.js';
	import BackupStateBadge from './backup-state-badge.svelte';
	import BackupHistoryDialog from './backup-history-dialog.svelte';
	import BackupResolveDialog from './backup-resolve-dialog.svelte';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { m } from '#lib/paraglide/messages.js';
	import { ClockIcon, GitBranchIcon, SettingsIcon, TrashIcon, UploadIcon, ShieldAlertIcon } from '#lib/icons/index.js';

	let {
		environmentId,
		projectId,
		projectName
	}: {
		environmentId: string;
		projectId: string;
		projectName: string;
	} = $props();

	const queryClient = useQueryClient();

	let historyOpen = $state(false);
	let resolveOpen = $state(false);
	let backingUp = $state(false);
	let disconnecting = $state(false);

	const backups = createQuery(() => ({
		queryKey: queryKeys.gitOpsSyncs.projectBackup(environmentId, projectId),
		queryFn: () =>
			gitOpsSyncService.getSyncs(environmentId, {
				pagination: { page: 1, limit: 1 },
				filters: { projectId, mode: 'backup' }
			}),
		enabled: !!environmentId && !!projectId
	}));

	const sync = $derived(backups.data?.data?.[0] ?? null);
	const canSync = $derived(hasPermission('gitops:sync', environmentId));
	const canDelete = $derived(hasPermission('gitops:delete', environmentId));
	const needsAttention = $derived(sync?.backupState === 'needs_attention');
	const showError = $derived(!!sync?.lastSyncError && (sync?.backupState === 'failed' || needsAttention));
	const createHref = $derived(`/environments/${environmentId}/gitops?action=create&mode=backup&projectId=${projectId}`);

	const metaItems = $derived.by<DetailMetaItem[]>(() => {
		if (!sync) return [];
		const repository = sync.repository?.name ?? sync.repositoryId;
		const directory = sync.backupDirectory ? ` / ${sync.backupDirectory}` : '';
		return [
			{
				icon: GitBranchIcon,
				label: m.destination(),
				value: `${repository} · ${sync.branch}${directory}`,
				mono: true
			},
			{
				icon: UploadIcon,
				label: m.last_backup(),
				value: sync.lastBackupAt ? formatDateTimeShort(sync.lastBackupAt) : m.common_never()
			},
			{
				icon: ClockIcon,
				label: m.last_check(),
				value: sync.lastSyncAt ? formatDateTimeShort(sync.lastSyncAt) : m.common_never()
			}
		];
	});

	async function invalidate() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.gitOpsSyncs.all });
		await queryClient.invalidateQueries({ queryKey: queryKeys.gitOpsSyncs.projectBackup(environmentId, projectId) });
	}

	async function backUpNow() {
		if (!sync) return;
		backingUp = true;
		const result = await tryCatch(gitOpsSyncService.performSync(environmentId, sync.id));
		await handleApiResultWithCallbacks({
			result,
			message: m.backup_failed(),
			setLoadingState: (value) => (backingUp = value),
			onSuccess: async () => {
				toast.success(m.backup_started());
				await invalidate();
			}
		});
		backingUp = false;
	}

	function disconnect() {
		if (!sync) return;
		const syncId = sync.id;
		confirmAndRun({
			title: m.disconnect_backup_title({ name: projectName }),
			message: m.disconnect_backup_message(),
			confirmLabel: m.common_disconnect(),
			destructive: true,
			setLoading: (value) => (disconnecting = value),
			run: () => gitOpsSyncService.deleteSync(environmentId, syncId),
			failureMessage: m.disconnect_backup_failed(),
			onSuccess: async () => {
				toast.success(m.disconnect_backup_success());
				await invalidate();
			}
		});
	}
</script>

<DetailSection title={m.git_backup()} icon={GitBranchIcon} class="mb-4 space-y-3 border-t-0 pt-0">
	{#if !sync}
		<div class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/50 px-4 py-3">
			<p class="text-sm text-muted-foreground">{m.not_backed_up_to_git()}</p>
			<ArcaneButton action="base" tone="outline-primary" href={createHref} icon={UploadIcon} customLabel={m.back_up_to_git()} />
		</div>
	{:else}
		<DetailMetaStrip items={metaItems}>
			<BackupStateBadge {sync} />
		</DetailMetaStrip>

		{#if showError}
			<p class="text-sm break-words text-destructive">{sync.lastSyncError}</p>
		{/if}

		<div class="flex flex-wrap items-center gap-2">
			{#if canSync}
				<ArcaneButton
					action="base"
					tone="outline-primary"
					icon={UploadIcon}
					customLabel={m.back_up_now()}
					loading={backingUp}
					disabled={backingUp || disconnecting}
					onclick={backUpNow}
				/>
			{/if}

			<RowActionsMenu>
				<DropdownMenu.Item onclick={() => (historyOpen = true)}>
					<ClockIcon class="size-4" />
					{m.history()}
				</DropdownMenu.Item>

				<DropdownMenu.Item onclick={() => goto(`/environments/${environmentId}/gitops?action=edit&syncId=${sync.id}`)}>
					<SettingsIcon class="size-4" />
					{m.settings()}
				</DropdownMenu.Item>

				{#if needsAttention && canSync}
					<DropdownMenu.Item onclick={() => (resolveOpen = true)}>
						<ShieldAlertIcon class="size-4" />
						{m.resolve()}
					</DropdownMenu.Item>
				{/if}

				{#if canDelete}
					<DropdownMenu.Separator />

					<DropdownMenu.Item variant="destructive" disabled={disconnecting} onclick={disconnect}>
						<TrashIcon class="size-4" />
						{m.common_disconnect()}
					</DropdownMenu.Item>
				{/if}
			</RowActionsMenu>
		</div>
	{/if}
</DetailSection>

<BackupHistoryDialog bind:open={historyOpen} {environmentId} {sync} />
<BackupResolveDialog bind:open={resolveOpen} {environmentId} {sync} onResolved={invalidate} />
