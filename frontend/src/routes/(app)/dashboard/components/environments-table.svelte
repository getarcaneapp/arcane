<script lang="ts" module>
	import type { ActionButton } from '#lib/components/action-button-group/index.js';
	import type { Environment, VulnerabilityRiskBand, VulnerabilityScoreStatus } from '#lib/types/environment.js';
	import type { DashboardLiveStatsStatus, SystemStats } from '#lib/types/shared.js';

	export interface EnvironmentTableRow {
		environment: Environment;
		online: boolean;
		loading: boolean;
		running: number;
		total: number;
		images: number;
		podCount: number | null;
		updates: number;
		reclaimable: number | null;
		risk: { score: number; band: VulnerabilityRiskBand; status: VulnerabilityScoreStatus } | null;
		versionText: string | null;
		updateAvailable: boolean;
		menuButtons: ActionButton[];
	}
</script>

<script lang="ts">
	import { resolve } from '$app/paths';

	import { ActionButtonGroup } from '#lib/components/action-button-group/index.js';
	import MemoryRing from '#lib/components/memory-ring.svelte';
	import MetricRing, { type MetricRingVariant } from '#lib/components/metric-ring.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { UpdateIcon, VerifiedCheckIcon, type IconType } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { cn } from '#lib/utils.js';
	import { bytes, formatRelativeTime } from '#lib/utils/formatting.js';
	import { getRiskBandDisplay } from '#lib/utils/vulnerability.js';

	import { getCpuMetric, getDiskMetric, getMemoryMetric } from '../overview.helpers';

	let {
		rows,
		getLiveStatsState,
		getLiveStatsStatus
	}: {
		rows: EnvironmentTableRow[];
		getLiveStatsState: (environmentId: string) => { stats: SystemStats | null } | null;
		getLiveStatsStatus: (environment: Environment) => DashboardLiveStatsStatus;
	} = $props();
	const showVulnerabilities = $derived(rows.some((row) => featureStore.isEnabled('vulnerabilityManagement', row.environment.id)));
	const showPods = $derived(rows.some((row) => row.podCount !== null));
	const headCell =
		'h-9 px-3 text-left align-middle text-xs font-medium whitespace-nowrap text-muted-foreground first:pl-2 last:pr-2';
	const bodyCell = 'px-3 py-2.5 align-middle first:pl-2 last:pr-2';
</script>

{#snippet actionCount(count: number, icon: IconType)}
	{@const Icon = count > 0 ? icon : VerifiedCheckIcon}
	<a
		href={resolve('/(app)/updates')}
		class={cn(
			'inline-flex items-center gap-1.5 text-sm tabular-nums transition-colors hover:underline',
			count === 0 ? 'text-muted-foreground' : 'text-warning'
		)}
	>
		<Icon class={cn('size-3.5 shrink-0', count === 0 && 'text-success')} />
		{count}
	</a>
{/snippet}

{#snippet riskScore(risk: EnvironmentTableRow['risk'])}
	{#if risk}
		{@const band = getRiskBandDisplay(risk.band, risk.status)}
		<a
			href={resolve('/(app)/security')}
			class="inline-flex items-center gap-2 text-sm tabular-nums hover:underline"
			title={band.label}
		>
			<span class="size-2 shrink-0 rounded-full {band.barClass}"></span>
			{risk.status === 'unavailable' ? '–' : risk.score}
		</a>
	{:else}
		<span class="text-xs text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet metric(
	value: number | null,
	variant: MetricRingVariant,
	status: DashboardLiveStatsStatus,
	memoryUsed?: number,
	memoryTotal?: number
)}
	{#if status === 'loading'}
		<div class="ml-auto size-9.5 animate-pulse rounded-full bg-muted"></div>
	{:else if status === 'denied'}
		<span class="text-xs text-muted-foreground" title={m.common_access_denied()}>—</span>
	{:else if value === null || Number.isNaN(value)}
		<span class="text-xs text-muted-foreground" title={status === 'unavailable' ? m.stats_unavailable() : undefined}>—</span>
	{:else}
		<span
			class={cn('flex justify-end', status === 'stale' && 'opacity-60')}
			title={status === 'stale' ? m.stats_stale() : undefined}
		>
			{#if memoryUsed !== undefined}
				<MemoryRing used={memoryUsed} total={memoryTotal} />
			{:else}
				<MetricRing percent={value} {variant} size={38} stroke={3}>
					<span class="text-2xs font-medium text-foreground tabular-nums">{Math.round(value)}%</span>
				</MetricRing>
			{/if}
		</span>
	{/if}
{/snippet}

<div class="overflow-x-auto">
	<table class="w-full text-sm">
		<thead>
			<tr class="border-b border-border/40">
				<th class={headCell}>{m.common_name()}</th>
				<th class={headCell}>{m.common_version()}</th>
				<th class={headCell}>{m.common_status()}</th>
				<th class={headCell}>{m.containers()}</th>
				<th class={headCell}>{m.images()}</th>
				{#if showPods}<th class={headCell}>{m.podman_pods_title()}</th>{/if}
				<th class={headCell}>{m.updates()}</th>
				{#if showVulnerabilities}<th class={headCell}>{m.risk()}</th>{/if}
				<th class={headCell}>{m.reclaimable()}</th>
				<th class={cn(headCell, 'text-right')}>{m.cpu_usage()}</th>
				<th class={cn(headCell, 'text-right')}>{m.memory_usage()}</th>
				<th class={cn(headCell, 'text-right')}>{m.disk()}</th>
				<th class={headCell}></th>
			</tr>
		</thead>
		<tbody>
			{#each rows as row (row.environment.id)}
				{@const stats = getLiveStatsState(row.environment.id)?.stats ?? null}
				{@const statsStatus = getLiveStatsStatus(row.environment)}
				<tr class="border-b border-border/40 transition-colors last:border-0 hover:bg-muted/30">
					<td class={bodyCell}>
						<div class="flex min-w-0 items-center gap-2">
							<a
								class="truncate font-medium hover:underline"
								href={resolve('/(app)/environments/[id]', { id: row.environment.id })}
							>
								{row.environment.name}
							</a>
							{#if row.podCount !== null}
								<Badge variant="purple" size="sm">{m.podman()}</Badge>
							{/if}
						</div>
					</td>
					<td class={bodyCell}>
						{#if row.versionText}
							<Badge variant="gray" size="sm" mono>
								{row.versionText}
								{#if row.updateAvailable}
									<span class="ml-1.5 inline-flex h-2 w-2 rounded-full bg-warning"></span>
								{/if}
							</Badge>
						{:else if row.loading}
							<Skeleton class="h-4 w-20" />
						{:else}
							<span class="text-xs text-muted-foreground">—</span>
						{/if}
					</td>
					<td class={bodyCell}>
						<span class="flex items-center gap-2">
							<span class={cn('size-2 rounded-full', row.online ? 'bg-success' : 'bg-muted-foreground/40')}></span>
							<span class="text-sm text-muted-foreground">{row.online ? m.common_online() : m.common_offline()}</span>
						</span>
					</td>
					{#if !row.online}
						<td class={bodyCell} colspan={7 + Number(showVulnerabilities) + Number(showPods)}>
							<span class="text-sm text-muted-foreground">
								{row.environment.lastSeen
									? m.offline_last_seen({ time: formatRelativeTime(row.environment.lastSeen) })
									: m.common_offline()}
							</span>
						</td>
					{:else if row.loading}
						<td class={bodyCell} colspan={7 + Number(showVulnerabilities) + Number(showPods)}
							><Skeleton class="h-4 w-full max-w-md" /></td
						>
					{:else}
						<td class={bodyCell}><span class="tabular-nums">{row.running}/{row.total}</span></td>
						<td class={bodyCell}><span class="tabular-nums">{row.images}</span></td>
						{#if showPods}
							<td class={bodyCell}>
								{#if row.podCount === null}
									<span class="text-xs text-muted-foreground">—</span>
								{:else}
									<span class="tabular-nums">{row.podCount}</span>
								{/if}
							</td>
						{/if}
						<td class={bodyCell}>{@render actionCount(row.updates, UpdateIcon)}</td>
						{#if showVulnerabilities}
							<td class={bodyCell}>
								{#if featureStore.isEnabled('vulnerabilityManagement', row.environment.id)}
									{@render riskScore(row.risk)}
								{/if}
							</td>
						{/if}
						<td class={bodyCell}>
							{#if row.reclaimable === null}
								<span class="text-xs text-muted-foreground">—</span>
							{:else}
								<span class="text-sm whitespace-nowrap tabular-nums"
									>{bytes.format(row.reclaimable, { unitSeparator: ' ' }) ?? '-'}</span
								>
							{/if}
						</td>
						<td class={cn(bodyCell, 'text-right')}>{@render metric(getCpuMetric(stats), 'cpu', statsStatus)}</td>
						<td class={cn(bodyCell, 'text-right')}
							>{@render metric(getMemoryMetric(stats), 'memory', statsStatus, stats?.memoryUsage, stats?.memoryTotal)}</td
						>
						<td class={cn(bodyCell, 'text-right')}>{@render metric(getDiskMetric(stats), 'disk', statsStatus)}</td>
					{/if}
					<td class={cn(bodyCell, 'text-right')}>
						<div class="flex items-center justify-end gap-1">
							<ActionButtonGroup buttons={row.menuButtons} size="sm" />
						</div>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
