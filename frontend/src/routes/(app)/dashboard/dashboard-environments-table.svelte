<script lang="ts" module>
	import type { ActionButton } from '#lib/components/action-button-group/index.js';
	import type { Environment } from '#lib/types/environment';

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
		versionText: string | null;
		updateAvailable: boolean;
		useButton?: ActionButton;
		menuButtons: ActionButton[];
	}
</script>

<script lang="ts">
	import * as Table from '#lib/components/ui/table/index.js';
	import { Badge } from '#lib/components/ui/badge';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EnvironmentsIcon, ShieldAlertIcon, UpdateIcon, VerifiedCheckIcon, type IconType } from '#lib/icons';
	import MetricRing, { type MetricRingVariant } from '#lib/components/metric-ring.svelte';
	import { m } from '#lib/paraglide/messages';
	import { cn } from '#lib/utils';

	let { rows }: { rows: EnvironmentTableRow[] } = $props();
</script>

{#snippet actionCount(count: number, href: string, icon: IconType, tone: 'amber' | 'red')}
	{@const Icon = count > 0 ? icon : VerifiedCheckIcon}
	<a
		{href}
		class={cn(
			'inline-flex items-center gap-1.5 text-sm tabular-nums transition-colors hover:underline',
			count === 0 && 'text-muted-foreground',
			count > 0 && tone === 'amber' && 'text-amber-600 dark:text-amber-400',
			count > 0 && tone === 'red' && 'text-red-600 dark:text-red-400'
		)}
	>
		<Icon class={cn('size-3.5 shrink-0', count === 0 && 'text-emerald-600 dark:text-emerald-400')} />
		{count}
	</a>
{/snippet}

{#snippet metric(value: number | null, variant: MetricRingVariant)}
	{#if value === null || Number.isNaN(value)}
		<span class="text-xs text-muted-foreground">—</span>
	{:else}
		<span class="flex items-center justify-end gap-2">
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
				<Table.Head>{m.vuln_title()}</Table.Head>
				<Table.Head class="text-right">{m.cpu_usage()}</Table.Head>
				<Table.Head class="text-right">{m.memory_usage()}</Table.Head>
				<Table.Head class="text-right">{m.dashboard_meter_disk()}</Table.Head>
				<Table.Head></Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each rows as row (row.environment.id)}
				<Table.Row class={cn(row.isCurrent && 'bg-primary/5')}>
					<Table.Cell>
						<div class="flex min-w-0 items-center gap-2">
							<a class="truncate font-medium hover:underline" href="/environments/{row.environment.id}">
								{row.environment.name}
							</a>
							{#if row.versionText}
								<Badge variant="gray" size="sm" class="font-mono">
									{row.versionText}
									{#if row.updateAvailable}
										<span class="ml-1.5 inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
									{/if}
								</Badge>
							{/if}
						</div>
					</Table.Cell>
					<Table.Cell>
						<span class="flex items-center gap-2">
							<span class={cn('size-2 rounded-full', row.online ? 'bg-emerald-500' : 'bg-muted-foreground/40')}></span>
							<span class="text-sm text-muted-foreground">{row.online ? m.common_online() : m.common_offline()}</span>
						</span>
					</Table.Cell>
					{#if row.loading}
						<Table.Cell colspan={7}><Skeleton class="h-4 w-full max-w-md" /></Table.Cell>
					{:else}
						<Table.Cell class="tabular-nums">{row.running}/{row.total}</Table.Cell>
						<Table.Cell class="tabular-nums">{row.images}</Table.Cell>
						<Table.Cell>{@render actionCount(row.updates, '/updates', UpdateIcon, 'amber')}</Table.Cell>
						<Table.Cell>
							{@render actionCount(row.vulnerabilities, '/images/vulnerabilities', ShieldAlertIcon, 'red')}
						</Table.Cell>
						<Table.Cell class="text-right">{@render metric(row.cpu, 'cpu')}</Table.Cell>
						<Table.Cell class="text-right">{@render metric(row.memory, 'memory')}</Table.Cell>
						<Table.Cell class="text-right">{@render metric(row.disk, 'disk')}</Table.Cell>
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
										class={cn(btn.action === 'prune' && 'text-destructive data-highlighted:text-destructive')}
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
