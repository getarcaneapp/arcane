<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import CreatedAtCell from '$lib/components/arcane-table/cells/created-at-cell.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import type { GlobalVariable } from '$lib/types/variable';
	import type { Environment } from '$lib/types/environment';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import { VariableIcon, LockIcon, EditIcon, TrashIcon, ClockIcon, GlobeIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { formatDateTime } from '$lib/utils/formatting';

	let {
		variables = $bindable(),
		environments,
		onEdit,
		onDelete
	}: {
		variables: GlobalVariable[];
		environments: Environment[];
		onEdit: (variable: GlobalVariable) => void;
		onDelete: (variable: GlobalVariable) => void;
	} = $props();

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	const envNameById = $derived(new Map(environments.map((environment) => [environment.id, environment.name])));

	function updatedTimestamp(variable: GlobalVariable): string {
		return variable.updatedAt ?? variable.createdAt;
	}

	function scopeNames(variable: GlobalVariable): string[] {
		return variable.environmentIds.map((id) => envNameById.get(id) ?? id);
	}

	function scopeLabel(variable: GlobalVariable): string {
		if (variable.allEnvironments || variable.environmentIds.length === 0) return m.all_environments();
		return scopeNames(variable).join(', ');
	}

	const filteredVariables = $derived.by(() => {
		const query = (requestOptions.search ?? '').trim().toLowerCase();
		let list = query
			? variables.filter(
					(variable) =>
						variable.key.toLowerCase().includes(query) || (!variable.isSecret && variable.value.toLowerCase().includes(query))
				)
			: [...variables];

		const sort = requestOptions.sort;
		if (sort?.column === 'key') {
			list.sort((a, b) => a.key.localeCompare(b.key));
			if (sort.direction === 'desc') list.reverse();
		} else if (sort?.column === 'updatedAt') {
			list.sort((a, b) => new Date(updatedTimestamp(a)).getTime() - new Date(updatedTimestamp(b)).getTime());
			if (sort.direction === 'desc') list.reverse();
		}
		return list;
	});

	function buildVariableTableData(items: GlobalVariable[]): Paginated<GlobalVariable> {
		return {
			data: items,
			pagination: {
				totalPages: 1,
				totalItems: items.length,
				currentPage: 1,
				itemsPerPage: Math.max(items.length, 1)
			}
		};
	}

	const tableData = $derived(buildVariableTableData(filteredVariables));

	const columns = [
		{ accessorKey: 'key', title: m.key(), sortable: true, cell: KeyCell },
		{ accessorKey: 'value', title: m.value(), cell: ValueCell },
		{ id: 'scope', accessorFn: (row) => row.id, title: m.common_scope(), cell: ScopeCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: UpdatedCell }
	] satisfies ColumnSpec<GlobalVariable>[];

	const mobileFields = [
		{ id: 'value', label: m.value(), defaultVisible: true },
		{ id: 'scope', label: m.common_scope(), defaultVisible: true },
		{ id: 'updatedAt', label: m.common_updated(), defaultVisible: true }
	];
</script>

{#if variables.length === 0}
	<div class="flex flex-col items-center justify-center py-12 text-sm text-muted-foreground">
		<VariableIcon class="mb-3 size-10 opacity-40" />
		<p class="font-medium">{m.variables_no_variables_title()}</p>
		<p class="mt-1">{m.variables_no_variables_description()}</p>
	</div>
{:else}
	<ArcaneTable
		persistKey="arcane-variables-table"
		items={tableData}
		bind:requestOptions
		bind:mobileFieldVisibility
		selectionDisabled={true}
		withoutPagination
		onRefresh={async () => tableData}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={VariableMobileCardSnippet}
	/>
{/if}

{#snippet KeyCell({ item }: { item: GlobalVariable })}
	<div class="flex items-center gap-2">
		<span class="font-mono text-sm font-medium">{item.key}</span>
		{#if item.isSecret}
			<Badge variant="amber" size="sm">
				<LockIcon class="size-3" />
				{m.secret()}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet ValueCell({ item }: { item: GlobalVariable })}
	{#if item.isSecret}
		<span class="font-mono text-muted-foreground select-none">••••••••</span>
	{:else}
		<div class="flex items-center gap-2">
			<span class="max-w-[280px] truncate font-mono text-sm">{item.value}</span>
			<CopyButton text={item.value} class="size-6" />
		</div>
	{/if}
{/snippet}

{#snippet ScopeCell({ item }: { item: GlobalVariable })}
	{#if item.allEnvironments || item.environmentIds.length === 0}
		<Badge variant="outline" size="sm">{m.all_environments()}</Badge>
	{:else}
		{@const names = scopeNames(item)}
		<div class="flex flex-wrap items-center gap-1">
			{#each names.slice(0, 2) as name, index (index)}
				<Badge variant="outline" size="sm" class="max-w-40 truncate">{name}</Badge>
			{/each}
			{#if names.length > 2}
				<Badge variant="gray" size="sm">{m.plus_count({ count: names.length - 2 })}</Badge>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet UpdatedCell({ item }: { item: GlobalVariable })}
	<CreatedAtCell value={updatedTimestamp(item)} />
{/snippet}

{#snippet VariableMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: GlobalVariable;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: VariableIcon, variant: 'blue' }}
		title={(item: GlobalVariable) => item.key}
		badges={item.isSecret ? [{ variant: 'amber' as const, text: m.secret() }] : []}
		fields={[
			{
				label: m.value(),
				getValue: (item: GlobalVariable) => (item.isSecret ? '••••••••' : item.value),
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['value'] ?? true
			},
			{
				label: m.common_scope(),
				getValue: (item: GlobalVariable) => scopeLabel(item),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['scope'] ?? true
			},
			{
				label: m.common_updated(),
				getValue: (item: GlobalVariable) => formatDateTime(updatedTimestamp(item)) || updatedTimestamp(item),
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['updatedAt'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: GlobalVariable })}
	<RowActionsMenu>
		<IfPermitted perm="templates:update">
			<DropdownMenu.Item onclick={() => onEdit(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => onDelete(item)}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}
