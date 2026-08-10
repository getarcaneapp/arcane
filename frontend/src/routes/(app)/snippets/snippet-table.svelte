<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import { Badge } from '#lib/components/ui/badge';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import type { Snippet } from '#lib/types/snippet';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import { CodeIcon, ClockIcon, PlayIcon, MonitorIcon, BoxIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';

	let {
		snippets = $bindable(),
		requestOptions = $bindable(),
		environmentId,
		onRefreshData,
		onRun,
		onEdit
	}: {
		snippets: Paginated<Snippet>;
		requestOptions: SearchPaginationSortRequest;
		environmentId: string;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
		onRun: (snippet: Snippet) => void;
		onEdit: (snippet: Snippet) => void;
	} = $props();

	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	function lastRunVariant(status?: string): 'green' | 'red' | 'amber' | 'gray' {
		if (status === 'success') return 'green';
		if (status === 'failed' || status === 'timeout') return 'red';
		if (!status) return 'gray';
		return 'amber';
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ id: 'target', accessorFn: (row) => row.id, title: m.snippets_target(), cell: TargetCell },
		{ id: 'parameters', accessorFn: (row) => row.id, title: m.snippets_parameters(), cell: ParametersCell },
		{ id: 'schedule', accessorFn: (row) => row.id, title: m.snippets_schedule(), cell: ScheduleCell },
		{ id: 'lastRun', accessorFn: (row) => row.id, title: m.snippets_last_run(), cell: LastRunCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<Snippet>[];

	const mobileFields = [
		{ id: 'target', label: m.snippets_target(), defaultVisible: true },
		{ id: 'parameters', label: m.snippets_parameters(), defaultVisible: true },
		{ id: 'schedule', label: m.snippets_schedule(), defaultVisible: true },
		{ id: 'lastRun', label: m.snippets_last_run(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item }: { item: Snippet })}
	<div class="min-w-0">
		<span class="font-medium">{item.name}</span>
		{#if item.description}
			<p class="max-w-[320px] truncate text-xs text-muted-foreground">{item.description}</p>
		{/if}
	</div>
{/snippet}

{#snippet TargetCell({ item }: { item: Snippet })}
	<Badge variant="outline" size="sm" class="gap-1">
		{#if item.target === 'container'}
			<BoxIcon class="size-3" />
			{m.snippets_target_container()}
		{:else}
			<MonitorIcon class="size-3" />
			{m.snippets_target_host()}
		{/if}
	</Badge>
{/snippet}

{#snippet ParametersCell({ item }: { item: Snippet })}
	{#if item.parameters && item.parameters.length > 0}
		<Badge variant="outline" size="sm">{m.snippets_parameter_count({ count: item.parameters.length })}</Badge>
	{:else}
		<span class="text-sm text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet ScheduleCell({ item }: { item: Snippet })}
	{#if item.schedule}
		<div class="flex items-center gap-1.5">
			<Badge variant={item.scheduleEnabled ? 'blue' : 'gray'} size="sm">
				{item.scheduleEnabled ? m.snippets_schedule_active() : m.snippets_schedule_paused()}
			</Badge>
			<span class="font-mono text-xs text-muted-foreground">{item.schedule}</span>
		</div>
	{:else}
		<span class="text-sm text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet LastRunCell({ item }: { item: Snippet })}
	{#if item.lastRunAt}
		<div class="flex items-center gap-1.5">
			<Badge variant={lastRunVariant(item.lastRunStatus)} size="sm">{item.lastRunStatus ?? m.common_unknown()}</Badge>
			<CreatedAtCell value={item.lastRunAt} />
		</div>
	{:else}
		<span class="text-sm text-muted-foreground">{m.snippets_never_run()}</span>
	{/if}
{/snippet}

{#snippet CreatedCell({ item }: { item: Snippet })}
	<CreatedAtCell value={item.createdAt} />
{/snippet}

{#snippet RowActions({ item }: { item: Snippet })}
	<IfPermitted perm="snippets:run" envId={environmentId}>
		<ArcaneButton action="base" tone="ghost" size="icon" class="size-8" onclick={() => onRun(item)}>
			<span class="sr-only">{m.snippets_run()}</span>
			<PlayIcon class="size-4" />
		</ArcaneButton>
	</IfPermitted>
{/snippet}

{#snippet SnippetMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: Snippet;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: CodeIcon, variant: 'blue' }}
		title={(item: Snippet) => item.name}
		subtitle={(item: Snippet) => item.description ?? null}
		fields={[
			{
				label: m.snippets_target(),
				getValue: (item: Snippet) => (item.target === 'container' ? m.snippets_target_container() : m.snippets_target_host()),
				icon: item.target === 'container' ? BoxIcon : MonitorIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['target'] ?? true
			},
			{
				label: m.snippets_parameters(),
				getValue: (item: Snippet) => (item.parameters?.length ? String(item.parameters.length) : '—'),
				icon: CodeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['parameters'] ?? true
			},
			{
				label: m.snippets_schedule(),
				getValue: (item: Snippet) => item.schedule ?? '—',
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['schedule'] ?? true
			},
			{
				label: m.snippets_last_run(),
				getValue: (item: Snippet) => item.lastRunStatus ?? m.snippets_never_run(),
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['lastRun'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-snippets-table"
	items={snippets}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => {
		await onRefreshData(options);
		return snippets;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={SnippetMobileCardSnippet}
	onRowClick={onEdit}
/>
