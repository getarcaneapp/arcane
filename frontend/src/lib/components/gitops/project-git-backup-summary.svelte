<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import StatCard from '#lib/components/stat-card.svelte';
	import MetricRing from '#lib/components/metric-ring.svelte';
	import BackupStateBadge from './backup-state-badge.svelte';
	import BackupHistoryDialog from './backup-history-dialog.svelte';
	import BackupResolveDialog from './backup-resolve-dialog.svelte';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDateTimeShort, formatRelativeTime } from '#lib/utils/formatting.js';
	import { syncedPaths } from '#lib/utils/gitops.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { GitOpsBackupHistoryEntry, GitOpsBackupState } from '#lib/types/automation.js';
	import {
		ClockIcon,
		FileTextIcon,
		GitBranchIcon,
		SaveIcon,
		SettingsIcon,
		TrashIcon,
		UploadIcon,
		ShieldAlertIcon,
		CheckIcon,
		AlertIcon,
		PauseIcon
	} from '#lib/icons/index.js';

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

	const canBackup = $derived(hasPermission('gitops:backup', environmentId));

	const RECENT_LIMIT = 5;
	const history = createQuery(() => ({
		queryKey: queryKeys.gitOpsSyncs.backupHistory(environmentId, sync?.id ?? '', RECENT_LIMIT),
		queryFn: () => gitOpsSyncService.getBackupHistory(environmentId, sync!.id, RECENT_LIMIT),
		enabled: !!sync && canBackup
	}));
	const recentEntries = $derived<GitOpsBackupHistoryEntry[]>(history.data?.entries ?? []);
	// Tracked snapshot paths from the last successful backup, not the commit's changed files.
	const snapshotFiles = $derived(sync ? syncedPaths(sync).length : 0);
	const fileLimit = $derived(sync?.maxSyncFiles ?? 0);
	const filePercent = $derived(fileLimit > 0 ? (snapshotFiles / fileLimit) * 100 : 0);

	const canCreate = $derived(canBackup && hasPermission('gitops:create', environmentId));
	const canRun = $derived(canBackup && hasPermission('gitops:sync', environmentId));
	const canEdit = $derived(canBackup && hasPermission('gitops:update', environmentId));
	const canDisconnect = $derived(canBackup && hasPermission('gitops:delete', environmentId));
	const backupState = $derived<GitOpsBackupState>(sync?.backupState ?? 'never');
	const needsAttention = $derived(backupState === 'needs_attention');
	const showError = $derived(!!sync?.lastSyncError && (backupState === 'failed' || needsAttention));
	const createHref = $derived(`/environments/${environmentId}/gitops?action=create&mode=backup&projectId=${projectId}`);
	const destination = $derived.by(() => {
		if (!sync) return '';
		const repository = sync.repository?.name ?? sync.repositoryId;
		const directory = sync.backupDirectory ? ` / ${sync.backupDirectory}` : '';
		return `${repository} · ${sync.branch}${directory}`;
	});

	const stateVisual = $derived.by(() => {
		switch (backupState) {
			case 'backed_up':
				return { icon: CheckIcon, color: 'text-success', bg: 'bg-success/10 ring-success/30' };
			case 'pending':
			case 'backing_up':
				return { icon: UploadIcon, color: 'text-warning', bg: 'bg-warning/10 ring-warning/30' };
			case 'failed':
			case 'needs_attention':
				return { icon: AlertIcon, color: 'text-destructive', bg: 'bg-destructive/10 ring-destructive/30' };
			case 'paused':
				return { icon: PauseIcon, color: 'text-muted-foreground', bg: 'bg-muted ring-border' };
			default:
				return { icon: GitBranchIcon, color: 'text-muted-foreground', bg: 'bg-muted ring-border' };
		}
	});

	function firstLine(message: string): string {
		return message.split('\n')[0] ?? message;
	}

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

{#if !sync}
	<div class="rounded-xl border border-dashed border-border/70">
		<Empty.Root>
			<Empty.Header>
				<Empty.Media variant="icon">
					<GitBranchIcon />
				</Empty.Media>
				<Empty.Title>{m.not_backed_up_to_git()}</Empty.Title>
				<Empty.Description>{m.back_up_to_git_description()}</Empty.Description>
			</Empty.Header>
			{#if canCreate}
				<Empty.Content>
					<ArcaneButton
						action="base"
						tone="outline-primary"
						href={createHref}
						icon={UploadIcon}
						customLabel={m.back_up_to_git()}
					/>
				</Empty.Content>
			{/if}
		</Empty.Root>
	</div>
{:else}
	{@const StateIcon = stateVisual.icon}
	<div class="space-y-4">
		<div class="flex flex-wrap items-center gap-4 rounded-xl border border-border/70 bg-card/60 p-4 backdrop-blur-md">
			<div class={cn('flex size-12 shrink-0 items-center justify-center rounded-full ring-1 ring-inset', stateVisual.bg)}>
				<StateIcon class={cn('size-6', stateVisual.color)} />
			</div>
			<div class="min-w-0 flex-1 space-y-1">
				<div class="flex flex-wrap items-center gap-2">
					<h3 class="text-base font-semibold">{m.git_backup()}</h3>
					<BackupStateBadge {sync} />
				</div>
				<p class="flex items-center gap-1.5 truncate font-mono text-xs text-muted-foreground" title={destination}>
					<GitBranchIcon class="size-3.5 shrink-0" />
					{destination}
				</p>
			</div>
			<div class="flex items-center gap-2">
				{#if canRun}
					<ArcaneButton
						action="base"
						tone="outline-primary"
						size="sm"
						icon={UploadIcon}
						customLabel={m.back_up_now()}
						loading={backingUp}
						disabled={backingUp || disconnecting}
						onclick={backUpNow}
					/>
				{/if}

				{#if canBackup}
					<RowActionsMenu>
						<DropdownMenu.Item onclick={() => (historyOpen = true)}>
							<ClockIcon class="size-4" />
							{m.history()}
						</DropdownMenu.Item>

						{#if canEdit}
							<DropdownMenu.Item onclick={() => goto(`/environments/${environmentId}/gitops?action=edit&syncId=${sync.id}`)}>
								<SettingsIcon class="size-4" />
								{m.settings()}
							</DropdownMenu.Item>
						{/if}

						{#if needsAttention}
							<DropdownMenu.Item onclick={() => (resolveOpen = true)}>
								<ShieldAlertIcon class="size-4" />
								{m.resolve()}
							</DropdownMenu.Item>
						{/if}

						{#if canDisconnect}
							<DropdownMenu.Separator />

							<DropdownMenu.Item variant="destructive" disabled={disconnecting} onclick={disconnect}>
								<TrashIcon class="size-4" />
								{m.common_disconnect()}
							</DropdownMenu.Item>
						{/if}
					</RowActionsMenu>
				{/if}
			</div>
		</div>

		{#if showError}
			<p class="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm break-words text-destructive">
				{sync.lastSyncError}
			</p>
		{/if}

		<div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
			<div
				class="hover-lift flex items-center justify-between gap-3 rounded-xl border border-border/70 bg-card/60 p-4 backdrop-blur-md"
			>
				<div class="space-y-2">
					<p class="text-sm font-medium tracking-wide text-muted-foreground">{m.files_in_last_backup()}</p>
					<h3 class="text-2xl font-semibold tracking-tight tabular-nums">{snapshotFiles}</h3>
					{#if fileLimit > 0}
						<p class="text-xs text-muted-foreground">{m.file_limit({ limit: fileLimit })}</p>
					{/if}
				</div>
				<MetricRing variant="backup" percent={filePercent} size={44} stroke={5} />
			</div>
			<StatCard
				title={m.last_backup()}
				value={sync.lastBackupAt ? formatRelativeTime(sync.lastBackupAt) : m.common_never()}
				subtitle={sync.lastBackupAt ? formatDateTimeShort(sync.lastBackupAt) : undefined}
				icon={UploadIcon}
				iconColor="text-success"
				bgColor="bg-success/10"
			/>
			<StatCard
				title={m.last_check()}
				value={sync.lastSyncAt ? formatRelativeTime(sync.lastSyncAt) : m.common_never()}
				subtitle={sync.lastSyncAt ? formatDateTimeShort(sync.lastSyncAt) : undefined}
				icon={ClockIcon}
			/>
			<StatCard
				title={m.common_mode()}
				value={sync.backupOnSave ? m.on_save() : m.backups_trigger_manual()}
				icon={SaveIcon}
				iconColor="text-info"
				bgColor="bg-info/10"
			/>
		</div>

		{#if canBackup}
			<div class="rounded-xl border border-border/70 bg-card/60 backdrop-blur-md">
				<div class="flex items-center justify-between gap-2 border-b border-border/50 px-4 py-3">
					<h3 class="flex items-center gap-2 text-sm font-medium">
						<ClockIcon class="size-4" />
						{m.recent_backups()}
					</h3>
					{#if recentEntries.length > 0}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							customLabel={m.common_view_all()}
							onclick={() => (historyOpen = true)}
						/>
					{/if}
				</div>
				{#if history.isPending}
					<div class="flex items-center gap-2 px-4 py-6 text-sm text-muted-foreground">
						<Spinner class="size-4" />
						{m.common_loading()}
					</div>
				{:else if history.isError}
					<p class="px-4 py-6 text-sm text-destructive">{m.backup_history_load_failed()}</p>
				{:else if recentEntries.length === 0}
					<p class="px-4 py-6 text-sm text-muted-foreground">{m.backup_history_empty()}</p>
				{:else}
					<ul class="divide-y divide-border/50">
						{#each recentEntries as entry (entry.commit)}
							<li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 text-sm">
								<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{entry.commit.slice(0, 7)}</code>
								<span class="min-w-0 flex-1 truncate font-medium">{firstLine(entry.message)}</span>
								<span class="flex items-center gap-1 text-xs text-muted-foreground">
									<FileTextIcon class="size-3.5" />
									{m.changed_files_count({ count: entry.files.length })}
								</span>
								<span class="text-xs text-muted-foreground" title={formatDateTimeShort(entry.date)}
									>{formatRelativeTime(entry.date)}</span
								>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		{/if}
	</div>
{/if}

<BackupHistoryDialog bind:open={historyOpen} {environmentId} {sync} />
<BackupResolveDialog bind:open={resolveOpen} {environmentId} {sync} onResolved={invalidate} />
