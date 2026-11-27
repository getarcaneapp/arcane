<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import {
		AlertIcon,
		ArrowDownIcon,
		ArrowRightIcon,
		BackupIcon,
		ClockIcon,
		CopyIcon,
		EnvironmentsIcon,
		InfoIcon,
		LayersIcon,
		RefreshIcon,
		type IconType
	} from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { backupStorageStatsQuery } from '#lib/query/backups.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { systemBackupService } from '#lib/services/system-backup-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { BackupStorageStats, SystemVolumeBackupHistoryEntry } from '#lib/types/backup.js';
	import { cn } from '#lib/utils.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import {
		backupDestinationDisplay,
		backupManagementLabel,
		backupStorageSegments,
		backupStorageTotals
	} from '#lib/utils/backups.js';
	import { readEachEnvironment } from '#lib/utils/environments.js';
	import { bytes, formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import BackupStorageBar from './backup-storage-bar.svelte';
	import BackupStorageBreakdown from './backup-storage-breakdown.svelte';

	interface Props {
		environments: EnvironmentRef[];
	}

	let { environments }: Props = $props();

	const queryClient = useQueryClient();
	const scope = $derived(environments.map((environment) => environment.id).join(','));
	const statsQuery = createQuery(() => ({
		...backupStorageStatsQuery(scope, environments),
		enabled: environments.length > 0,
		refetchInterval: 60_000
	}));
	const report = $derived(statsQuery.data);
	const failures = $derived(report?.failedEnvironments ?? []);
	const repositories = $derived(report?.byEnvironment.flatMap(({ data }) => data.repositories) ?? []);
	const totals = $derived(backupStorageTotals(repositories));
	const segments = $derived(backupStorageSegments(totals));
	const percentages = $derived.by(() => {
		if (segments.totalBytes <= 0) return { stored: 0, compression: 0, deduplication: 0 };
		const stored = Math.round((segments.storedBytes / segments.totalBytes) * 100);
		const compression = Math.round((segments.compressionBytes / segments.totalBytes) * 100);
		return { stored, compression, deduplication: Math.max(100 - stored - compression, 0) };
	});
	const compressionRatio = $derived(
		segments.storedBytes > 0 ? (segments.storedBytes + segments.compressionBytes) / segments.storedBytes : 1
	);
	const deduplicationRatio = $derived(
		segments.totalBytes > segments.deduplicationBytes
			? segments.totalBytes / (segments.totalBytes - segments.deduplicationBytes)
			: 1
	);
	const updatedAt = $derived(
		repositories.reduce<string | undefined>(
			(latest, repository) => (!latest || repository.updatedAt > latest ? repository.updatedAt : latest),
			undefined
		)
	);
	const lastBackup = $derived(
		(report?.byEnvironment ?? []).reduce<SystemVolumeBackupHistoryEntry | undefined>((latest, { data }) => {
			if (!data.lastBackup) return latest;
			return !latest || data.lastBackup.createdAt > latest.createdAt ? data.lastBackup : latest;
		}, undefined)
	);
	const legend = $derived([
		{ label: m.stored(), value: segments.storedBytes, percent: percentages.stored, swatch: 'bg-primary' },
		{
			label: m.saved_by_compression(),
			value: segments.compressionBytes,
			percent: percentages.compression,
			swatch: 'bg-saved-hatch'
		},
		{
			label: m.saved_by_deduplication(),
			value: segments.deduplicationBytes,
			percent: percentages.deduplication,
			swatch: 'bg-deduplicated-hatch'
		},
		{ label: m.backed_up(), value: segments.totalBytes, percent: undefined, swatch: 'border-[1.5px] border-muted-foreground' }
	]);
	let refreshing = $state(false);
	let breakdownOpen = $state(false);

	async function refresh() {
		refreshing = true;
		const result = await tryCatch(
			readEachEnvironment(environments, (id, config) => systemBackupService.refreshStorageStats(id, config), {
				repositories: []
			} satisfies BackupStorageStats)
		);
		refreshing = false;
		const failed = result.error === null ? result.data.failedEnvironments : [];
		if (result.error !== null || failed.length > 0) {
			toast.error(m.common_refresh_failed({ resource: m.backup_storage() }), {
				description:
					result.error === null
						? failed.map(({ environment, message }) => `${environment.name}: ${message}`).join('\n')
						: extractApiErrorMessage(result.error)
			});
		}
		await queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.storageStats(scope) });
	}
</script>

{#snippet tile(Icon: IconType, tone: string, label: string, value: string, detail: string)}
	<div
		class="flex min-w-0 flex-col gap-2 rounded-lg border border-border/45 bg-muted/20 p-2.5 @md:flex-row @md:items-start @md:gap-2.5 @md:p-3"
	>
		<span class={cn('flex size-7 shrink-0 items-center justify-center rounded-lg ring-1 ring-foreground/5 ring-inset', tone)}>
			<Icon class="size-3.5" />
		</span>
		<div class="min-w-0">
			<p class="truncate text-xs text-muted-foreground">{label}</p>
			<p class="mt-0.5 truncate text-lg leading-tight font-semibold tabular-nums">{value}</p>
			<p class="truncate text-xs text-muted-foreground">{detail}</p>
		</div>
	</div>
{/snippet}

<section class="@container overflow-hidden rounded-xl border border-border/70 bg-card/10" data-testid="backup-storage-card">
	<div class="p-4 @lg:px-5 @lg:pt-5">
		<div class="flex items-center justify-between gap-3">
			<h2 class="flex min-w-0 items-center gap-2 text-sm font-semibold">
				<span
					class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-foreground/5 ring-inset"
				>
					<BackupIcon class="size-3.5" />
				</span>
				<span class="truncate">{m.backup_storage()}</span>
			</h2>
			<div class="flex shrink-0 items-center gap-2">
				{#if updatedAt}
					<span class="hidden text-xs text-muted-foreground @lg:inline">
						{m.updated_time({ time: formatRelativeTime(updatedAt) })}
					</span>
				{/if}
				<ArcaneButton
					action="refresh"
					size="icon"
					showLabel={false}
					icon={RefreshIcon}
					loading={refreshing}
					disabled={refreshing || environments.length === 0}
					onclick={refresh}
					class="size-8"
					data-testid="backup-storage-refresh"
				/>
			</div>
		</div>

		{#if statsQuery.isPending}
			<Skeleton class="mt-4 h-7 w-40" />
			<Skeleton class="mt-3 h-3 w-full" />
			<div class="mt-4 grid grid-cols-2 gap-2.5 @2xl:grid-cols-4">
				{#each { length: 4 } as _, index (index)}
					<Skeleton class="h-19" />
				{/each}
			</div>
		{:else if statsQuery.isError}
			<div class="mt-4 flex items-start gap-2 text-sm text-muted-foreground">
				<AlertIcon class="mt-0.5 size-4 shrink-0 text-warning" />
				<div class="min-w-0">
					<p class="text-foreground">{m.storage_figures_unavailable()}</p>
					<p class="break-words">{extractApiErrorMessage(statsQuery.error)}</p>
				</div>
			</div>
		{:else if repositories.length === 0}
			<div class="mt-4 mb-3 flex items-baseline gap-2.5">
				<span class="text-3xl leading-none font-semibold text-muted-foreground">–</span>
				<span class="text-sm text-muted-foreground">{m.no_storage_figures_yet()}</span>
			</div>
			<BackupStorageBar stats={totals} />
			<p class="mt-3 flex items-start gap-2 text-sm text-muted-foreground">
				<InfoIcon class="mt-0.5 size-4 shrink-0" />
				{m.storage_figures_recorded_after_backup()}
			</p>
		{:else}
			<div class="mt-4 mb-3 flex flex-wrap items-baseline gap-x-2.5 gap-y-1.5">
				<span class="text-2xl leading-none font-semibold tracking-tight tabular-nums @lg:text-3xl">
					{bytes.format(segments.storedBytes, { unitSeparator: ' ' })}
				</span>
				<span class="text-sm text-muted-foreground">
					{m.stored_of_backed_up({ total: bytes.format(segments.totalBytes, { unitSeparator: ' ' }) ?? '-' })}
				</span>
				{#if segments.totalBytes > segments.storedBytes}
					<Badge variant="green" size="sm" class="self-center @lg:ml-auto">
						<ArrowDownIcon />
						{m.size_saved({ size: bytes.format(segments.totalBytes - segments.storedBytes, { unitSeparator: ' ' }) ?? '-' })}
					</Badge>
				{/if}
			</div>
			<BackupStorageBar stats={totals} />
			<dl class="mt-2.5 grid gap-1.5 text-xs-plus @lg:flex @lg:flex-wrap @lg:gap-x-5">
				{#each legend as item (item.label)}
					<div class="flex items-center gap-2">
						<span class={cn('size-2.5 shrink-0 rounded-full', item.swatch)}></span>
						<dt class="min-w-0 flex-1 truncate text-muted-foreground @lg:flex-none">{item.label}</dt>
						<dd class="font-semibold tabular-nums">{bytes.format(item.value, { unitSeparator: ' ' })}</dd>
						<dd class="w-9 text-right text-muted-foreground tabular-nums @lg:w-auto @lg:text-left">
							{item.percent === undefined ? '' : `${item.percent}%`}
						</dd>
					</div>
				{/each}
			</dl>
			<div class="mt-4 grid grid-cols-2 gap-2.5 @2xl:grid-cols-4 @2xl:gap-3">
				{@render tile(
					LayersIcon,
					'bg-primary/10 text-primary',
					m.snapshots(),
					totals.snapshotCount.toLocaleString(),
					totals.repositoryCount === 1
						? m.in_repository_count_one()
						: m.in_repository_count({ count: totals.repositoryCount.toLocaleString() })
				)}
				{@render tile(
					ArrowDownIcon,
					'bg-success/10 text-success',
					m.compression(),
					m.ratio_times({ ratio: compressionRatio.toFixed(2) }),
					m.size_saved({ size: bytes.format(segments.compressionBytes, { unitSeparator: ' ' }) ?? '-' })
				)}
				{@render tile(
					CopyIcon,
					'bg-info/10 text-info',
					m.deduplication(),
					m.ratio_times({ ratio: deduplicationRatio.toFixed(2) }),
					m.size_saved({ size: bytes.format(segments.deduplicationBytes, { unitSeparator: ' ' }) ?? '-' })
				)}
				{@render tile(
					ClockIcon,
					'bg-warning/10 text-warning',
					m.last_backup(),
					lastBackup ? formatRelativeTime(lastBackup.createdAt) : m.common_never(),
					lastBackup ? `${backupManagementLabel(lastBackup.type)} · ${backupDestinationDisplay(lastBackup)}` : ''
				)}
			</div>
		{/if}
	</div>

	{#if report && (repositories.length > 0 || failures.length > 0)}
		<div class="flex items-center justify-between gap-3 border-t border-border/55 bg-muted/20 px-4 py-2.5 text-xs-plus @lg:px-5">
			{#if failures.length > 0}
				<span class="flex min-w-0 items-center gap-1.5 text-warning">
					<AlertIcon class="size-3.5 shrink-0" />
					<span class="truncate">
						{failures.length === 1
							? m.environments_not_reporting_one()
							: m.environments_not_reporting({ count: failures.length })}
					</span>
				</span>
			{:else}
				<span class="flex min-w-0 items-center gap-1.5 text-muted-foreground">
					<EnvironmentsIcon class="size-3.5 shrink-0" />
					<span class="truncate">
						{totals.repositoryCount === 1
							? m.repository_count_one()
							: m.repository_count({ count: totals.repositoryCount.toLocaleString() })}
						{#if report.byEnvironment.length > 1}
							<span class="hidden @md:inline">{m.across_environment_count({ count: report.byEnvironment.length })}</span>
						{/if}
					</span>
				</span>
			{/if}
			<button
				type="button"
				class="pressable flex shrink-0 items-center gap-1 rounded-sm font-medium text-primary-text hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
				onclick={() => (breakdownOpen = true)}
				data-testid="backup-storage-breakdown"
			>
				{m.view_breakdown()}
				<ArrowRightIcon class="size-3.5" />
			</button>
		</div>
	{/if}
</section>

<BackupStorageBreakdown
	bind:open={breakdownOpen}
	environments={report?.byEnvironment ?? []}
	{failures}
	{updatedAt}
	{refreshing}
	onRefresh={refresh}
/>
