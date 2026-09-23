<script lang="ts" module>
	import type { ActionButton } from '#lib/components/action-button-group/index.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { DashboardLiveStatsStatus } from '#lib/types/shared.js';

	export interface EnvironmentTableRow {
		environment: Environment;
		isCurrent: boolean;
		online: boolean;
		loading: boolean;
		running: number;
		total: number;
		images: number;
		updates: number;
		vulnerabilities: number;
		cpu: number | null;
		memory: number | null;
		disk: number | null;
		statsStatus: DashboardLiveStatsStatus;
		versionText: string | null;
		updateAvailable: boolean;
		useButton?: ActionButton;
		menuButtons: ActionButton[];
	}
</script>

<script lang="ts">
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EnvironmentsIcon, ShieldAlertIcon, UpdateIcon, VerifiedCheckIcon, type IconType } from '#lib/icons/index.js';
	import MetricRing, { type MetricRingVariant } from '#lib/components/metric-ring.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	let { rows }: { rows: EnvironmentTableRow[] } = $props();
	const showVulnerabilities = $derived(rows.some((row) => featureStore.isEnabled('vulnerabilityManagement', row.environment.id)));
</script>

{#snippet actionCount(count: number, href: string, icon: IconType, tone: 'amber' | 'red')}
	{@const Icon = count > 0 ? icon : VerifiedCheckIcon}
	<a
		{href}
		class={cn(
			'inline-flex items-center gap-1.5 text-sm tabular-nums transition-colors hover:underline',
			count === 0 && 'text-muted-foreground',
			count > 0 && tone === 'amber' && 'text-warning',
			count > 0 && tone === 'red' && 'text-destructive'
		)}
	>
		<Icon class={cn('size-3.5 shrink-0', count === 0 && 'text-success')} />
		{count}
	</a>
{/snippet}

{#snippet metric(value: number | null, variant: MetricRingVariant, status: DashboardLiveStatsStatus)}
	{#if status === 'loading'}
		<Skeleton class="ml-auto h-4 w-12" />
	{:else if status === 'denied'}
		<span class="text-xs text-muted-foreground" title={m.common_access_denied()}>—</span>
	{:else if value === null || Number.isNaN(value)}
		<span class="text-xs text-muted-foreground" title={status === 'unavailable' ? m.stats_unavailable() : undefined}>—</span>
	{:else}
		<span
			class={cn('flex items-center justify-end gap-2', status === 'stale' && 'opacity-60')}
			title={status === 'stale' ? m.stats_stale() : undefined}
		>
			<MetricRing percent={value} {variant} />
			<span class="text-xs font-medium text-foreground tabular-nums">{Math.round(value)}%</span>
		</span>
	{/if}
{/snippet}

<div class="overflow-x-auto rounded-lg border border-border/60">
	<Table.Root>
		<Table.Header>
			<Table.Row>
				<Table.Head>{m.common_name()}</Table.Head>
				<Table.Head>{m.common_status()}</Table.Head>
				<Table.Head>{m.containers()}</Table.Head>
				<Table.Head>{m.images()}</Table.Head>
				<Table.Head>{m.updates()}</Table.Head>
				{#if showVulnerabilities}<Table.Head>{m.vuln_title()}</Table.Head>{/if}
				<Table.Head class="text-right">{m.cpu_usage()}</Table.Head>
				<Table.Head class="text-right">{m.memory_usage()}</Table.Head>
				<Table.Head class="text-right">{m.dashboard_meter_disk()}</Table.Head>
				<Table.Head></Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each rows as row (row.environment.id)}
				<Table.Row data-state={row.isCurrent ? 'selected' : undefined}>
					<Table.Cell>
						<div class="flex min-w-0 items-center gap-2">
							<a class="truncate font-medium hover:underline" href="/environments/{row.environment.id}">
								{row.environment.name}
							</a>
							{#if row.versionText}
								<Badge variant="gray" size="sm" mono>
									{row.versionText}
									{#if row.updateAvailable}
										<span class="ml-1.5 inline-flex h-2 w-2 rounded-full bg-warning"></span>
									{/if}
								</Badge>
							{/if}
						</div>
					</Table.Cell>
					<Table.Cell>
						<span class="flex items-center gap-2">
							<span class={cn('size-2 rounded-full', row.online ? 'bg-success' : 'bg-muted-foreground/40')}></span>
							<span class="text-sm text-muted-foreground">{row.online ? m.common_online() : m.common_offline()}</span>
						</span>
					</Table.Cell>
					{#if row.loading}
						<Table.Cell colspan={showVulnerabilities ? 7 : 6}><Skeleton class="h-4 w-full max-w-md" /></Table.Cell>
					{:else}
						<Table.Cell><span class="tabular-nums">{row.running}/{row.total}</span></Table.Cell>
						<Table.Cell><span class="tabular-nums">{row.images}</span></Table.Cell>
						<Table.Cell>{@render actionCount(row.updates, '/updates', UpdateIcon, 'amber')}</Table.Cell>
						{#if showVulnerabilities}
							<Table.Cell>
								{#if featureStore.isEnabled('vulnerabilityManagement', row.environment.id)}
									{@render actionCount(row.vulnerabilities, '/security', ShieldAlertIcon, 'red')}
								{/if}
							</Table.Cell>
						{/if}
						<Table.Cell class="text-right">{@render metric(row.cpu, 'cpu', row.statsStatus)}</Table.Cell>
						<Table.Cell class="text-right">{@render metric(row.memory, 'memory', row.statsStatus)}</Table.Cell>
						<Table.Cell class="text-right">{@render metric(row.disk, 'disk', row.statsStatus)}</Table.Cell>
					{/if}
					<Table.Cell class="text-right">
						<div class="flex items-center justify-end gap-1">
							{#if row.useButton}
								<ArcaneButton
									action="base"
									size="sm"
									tone="ghost"
									icon={EnvironmentsIcon}
									customLabel={row.isCurrent ? m.common_current() : row.useButton.label}
									loading={row.useButton.loading}
									disabled={row.useButton.disabled}
									onclick={row.useButton.onclick}
									class={cn(row.isCurrent && 'disabled:opacity-100 [&_svg]:text-primary!')}
								/>
							{/if}
							<RowActionsMenu>
								{#each row.menuButtons as btn (btn.id)}
									<DropdownMenu.Item
										disabled={!!(btn.disabled || btn.loading)}
										onclick={btn.onclick}
										variant={btn.action === 'prune' ? 'destructive' : 'default'}
									>
										{#if btn.icon}
											<btn.icon class="size-4" />
										{/if}
										{btn.label}
									</DropdownMenu.Item>
								{/each}
							</RowActionsMenu>
						</div>
					</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
</div>
