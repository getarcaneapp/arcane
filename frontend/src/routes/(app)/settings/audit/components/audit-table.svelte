<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RelativeTimeCell from '#lib/components/arcane-table/cells/relative-time-cell.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { EnvironmentsIcon, ShieldCheckIcon, TagIcon, UserIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { AuditLog } from '#lib/types/auth.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';

	import AuditDetailPanel from './audit-detail-panel.svelte';
	import { auditActorLabel, auditOutcomeLabel, auditOutcomeVariant } from './audit-labels.js';

	let {
		logs,
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRefreshData
	}: {
		logs: Paginated<AuditLog>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	function environmentName(environmentId: string): string {
		return environmentStore.available.find((environment) => environment.id === environmentId)?.name || environmentId;
	}

	function targetLabel(log: AuditLog): string {
		return log.targetName || log.targetId || '';
	}

	const columns = [
		{ accessorKey: 'action', title: m.audit_col_action(), sortable: true, cell: ActionCell },
		{ accessorKey: 'outcome', title: m.audit_col_outcome(), sortable: true, cell: OutcomeCell },
		{ accessorKey: 'actorUsername', title: m.audit_col_actor(), sortable: true, cell: ActorCell },
		{ id: 'target', accessorKey: 'targetName', title: m.audit_col_target(), cell: TargetCell },
		{ id: 'environment', accessorKey: 'environmentId', title: m.resource_environment_cap(), cell: EnvironmentCell },
		{ accessorKey: 'createdAt', title: m.events_col_time(), sortable: true, cellComponent: RelativeTimeCell }
	] satisfies ColumnSpec<AuditLog>[];

	const mobileFields = [
		{ id: 'outcome', label: m.audit_col_outcome(), defaultVisible: true },
		{ id: 'actorUsername', label: m.audit_col_actor(), defaultVisible: true },
		{ id: 'target', label: m.audit_col_target(), defaultVisible: true },
		{ id: 'environment', label: m.resource_environment_cap(), defaultVisible: true },
		{ id: 'createdAt', label: m.events_col_time(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet ActionCell({ item }: { item: AuditLog })}
	<div class="min-w-0">
		<code class="text-xs font-semibold">{item.action}</code>
		{#if item.permission}
			<p class="text-xs text-muted-foreground">{item.permission}</p>
		{/if}
	</div>
{/snippet}

{#snippet OutcomeCell({ item }: { item: AuditLog })}
	<Badge variant={auditOutcomeVariant(item.outcome)} minWidth="20">{auditOutcomeLabel(item.outcome)}</Badge>
{/snippet}

{#snippet ActorCell({ item }: { item: AuditLog })}
	<span class={item.actorUsername ? 'text-sm' : 'text-sm text-muted-foreground italic'}>{auditActorLabel(item)}</span>
{/snippet}

{#snippet TargetCell({ item }: { item: AuditLog })}
	{#if item.targetType || targetLabel(item)}
		<div class="min-w-0">
			<p class="truncate text-sm" title={targetLabel(item)}>{targetLabel(item) || '—'}</p>
			{#if item.targetType}
				<p class="text-xs text-muted-foreground">{item.targetType}</p>
			{/if}
		</div>
	{:else}
		<span class="text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet EnvironmentCell({ item }: { item: AuditLog })}
	{#if item.environmentId}
		<Badge variant="gray">{environmentName(item.environmentId)}</Badge>
	{:else}
		<span class="text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet ExpandedLog({ item }: { item: AuditLog })}
	<AuditDetailPanel log={item} />
{/snippet}

{#snippet AuditMobileCard({ item, mobileFieldVisibility }: { item: AuditLog; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: ShieldCheckIcon, variant: 'gray' }}
		title={(item: AuditLog) => item.action}
		subtitle={(item: AuditLog) => ((mobileFieldVisibility['createdAt'] ?? true) ? formatRelativeTime(item.createdAt) : null)}
		badges={[
			(item: AuditLog) =>
				(mobileFieldVisibility['outcome'] ?? true)
					? { variant: auditOutcomeVariant(item.outcome), text: auditOutcomeLabel(item.outcome) }
					: null
		]}
		fields={[
			{
				label: m.audit_col_actor(),
				getValue: (item: AuditLog) => auditActorLabel(item),
				icon: UserIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['actorUsername'] ?? true
			},
			{
				label: m.audit_col_target(),
				getValue: (item: AuditLog) => targetLabel(item) || null,
				icon: TagIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility['target'] ?? true) && !!targetLabel(item)
			},
			{
				label: m.resource_environment_cap(),
				getValue: (item: AuditLog) => (item.environmentId ? environmentName(item.environmentId) : null),
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility['environment'] ?? true) && !!item.environmentId
			}
		]}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-audit-table"
	items={logs}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	selectionDisabled
	onRefresh={async (options) => {
		requestOptions = options;
		await onRefreshData(options);
		return logs;
	}}
	{columns}
	{mobileFields}
	mobileCard={AuditMobileCard}
	expandedRowContent={ExpandedLog}
/>
