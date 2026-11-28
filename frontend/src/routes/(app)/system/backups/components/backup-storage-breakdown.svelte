<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { CloudStorageIcon, EnvironmentsIcon, LocalFolderComputerIcon, RefreshIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { BackupStorageStats } from '#lib/types/backup.js';
	import { cn } from '#lib/utils.js';
	import {
		backupDestinationName,
		backupRepositoryKindLabel,
		backupStorageRatios,
		backupStorageSegments,
		backupStorageTotals
	} from '#lib/utils/backups.js';
	import type { EnvironmentFailure } from '#lib/utils/environments.js';
	import { bytes, formatRelativeTime } from '#lib/utils/formatting.js';

	import BackupStorageBar from './backup-storage-bar.svelte';

	interface Props {
		open: boolean;
		environments: { environment: EnvironmentRef; data: BackupStorageStats }[];
		failures: EnvironmentFailure[];
		updatedAt?: string;
		refreshing: boolean;
		onRefresh: () => void;
	}

	let { open = $bindable(), environments, failures, updatedAt, refreshing, onRefresh }: Props = $props();

	const largestTotal = $derived(
		Math.max(
			1,
			...environments.flatMap(({ data }) => data.repositories.map((repository) => backupStorageSegments(repository).totalBytes))
		)
	);

	function snapshotCount(count: number): string {
		return count === 1 ? m.snapshot_count_one() : m.snapshot_count({ count: count.toLocaleString() });
	}
</script>

{#snippet environmentHeader(environment: EnvironmentRef, online: boolean, summary: string)}
	<div class="mb-2 flex items-center justify-between gap-2">
		<span class="flex min-w-0 items-center gap-1.5 text-sm font-semibold">
			<span class={cn('size-1.5 shrink-0 rounded-full', online ? 'bg-success' : 'bg-muted-foreground')}></span>
			<span class="truncate">{environment.name}</span>
		</span>
		<span class="shrink-0 text-xs text-muted-foreground tabular-nums">{summary}</span>
	</div>
{/snippet}

<ResponsiveDialog bind:open variant="sheet" title={m.storage_by_repository()} description={m.storage_by_repository_description()}>
	<div class="space-y-5 pt-1 pb-6">
		<div class="flex items-center gap-2">
			<ArcaneButton
				action="refresh"
				size="sm"
				icon={RefreshIcon}
				loading={refreshing}
				disabled={refreshing}
				onclick={onRefresh}
				data-testid="backup-storage-breakdown-refresh"
			/>
			{#if updatedAt}
				<span class="text-xs text-muted-foreground">{m.updated_time({ time: formatRelativeTime(updatedAt) })}</span>
			{/if}
		</div>

		{#each environments as { environment, data } (environment.id)}
			{@const totals = backupStorageTotals(data.repositories)}
			<section>
				{@render environmentHeader(
					environment,
					true,
					`${bytes.format(totals.storedBytes, { unitSeparator: ' ' })} ${m.of_size({ size: bytes.format(backupStorageSegments(totals).totalBytes, { unitSeparator: ' ' }) ?? '-' })}`
				)}
				<div class="divide-y divide-border/50 overflow-hidden rounded-lg border border-border/60">
					{#each data.repositories as repository (repository.id)}
						{@const local = repository.destination === 'local'}
						{@const Icon = local ? LocalFolderComputerIcon : CloudStorageIcon}
						{@const segments = backupStorageSegments(repository)}
						{@const ratios = backupStorageRatios(segments)}
						<div class="flex gap-2.5 p-3">
							<span
								class={cn(
									'flex size-7 shrink-0 items-center justify-center rounded-md ring-1 ring-foreground/5 ring-inset',
									local ? 'bg-muted text-muted-foreground' : 'bg-info/10 text-info'
								)}
							>
								<Icon class="size-3.5" />
							</span>
							<div class="min-w-0 flex-1 space-y-1.5">
								<div class="flex min-w-0 items-center gap-1.5">
									<span class="min-w-0 truncate text-sm font-medium">{local ? m.local() : backupDestinationName(repository)}</span
									>
									<Badge variant="outline" size="xs" class="shrink-0">{backupRepositoryKindLabel(repository.kind)}</Badge>
								</div>
								<div class="w-(--total-share)" style:--total-share="{Math.max((segments.totalBytes / largestTotal) * 100, 8)}%">
									<BackupStorageBar stats={repository} size="slim" />
								</div>
								<p class="text-xs-plus tabular-nums">
									<span class="font-semibold">{bytes.format(repository.storedBytes, { unitSeparator: ' ' })}</span>
									<span class="text-muted-foreground">
										{m.of_size({
											size: bytes.format(segments.totalBytes, { unitSeparator: ' ' }) ?? '-'
										})}
									</span>
								</p>
								<div class="flex flex-wrap gap-x-2.5 gap-y-0.5 text-xs text-muted-foreground tabular-nums">
									<span>{snapshotCount(repository.snapshotCount)}</span>
									<span>{m.compression_ratio_detail({ ratio: ratios.compression.toFixed(1) })}</span>
									<span>{m.deduplication_ratio_detail({ ratio: ratios.deduplication.toFixed(1) })}</span>
									<span>{m.updated_time({ time: formatRelativeTime(repository.updatedAt) })}</span>
								</div>
							</div>
						</div>
					{:else}
						<p class="p-3 text-sm text-muted-foreground">{m.no_storage_figures_yet()}</p>
					{/each}
				</div>
			</section>
		{/each}

		{#each failures as failure (failure.environment.id)}
			<section>
				{@render environmentHeader(failure.environment, false, m.not_reporting())}
				<div class="flex items-start gap-2.5 rounded-lg border border-border/60 p-3">
					<span class="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
						<EnvironmentsIcon class="size-3.5" />
					</span>
					<p class="min-w-0 text-sm break-words text-muted-foreground">{failure.message}</p>
				</div>
			</section>
		{/each}
	</div>
</ResponsiveDialog>
