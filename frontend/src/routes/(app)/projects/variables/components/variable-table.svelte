<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import RemoveMenuItem from '#lib/components/arcane-table/cells/remove-menu-item.svelte';
	import EnvironmentScopeCell, { scopeLabel } from '#lib/components/arcane-table/cells/scope-cell.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { VariableIcon, LockIcon, EditIcon, ClockIcon, GlobeIcon, ContainersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { GlobalVariable } from '#lib/types/env.js';
	import type { Environment } from '#lib/types/environment.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { hasAnyPermission } from '#lib/utils/auth.js';
	import { formatDateTime, instantEpochMilliseconds } from '#lib/utils/formatting.js';

	let {
		variables,
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

	const canMutateVariables = $derived(hasAnyPermission(['variables:update', 'variables:delete'], GLOBAL_SCOPE));

	const envNameById = $derived(new Map(environments.map((environment) => [environment.id, environment.name])));

	function updatedTimestamp(variable: GlobalVariable): string {
		return variable.updatedAt ?? variable.createdAt;
	}

	const filteredVariables = $derived.by(() => {
		const query = (requestOptions.search ?? '').trim().toLowerCase();
		const list = query
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
			const updatedMs = new Map(list.map((v) => [v, instantEpochMilliseconds(updatedTimestamp(v)) ?? 0]));
			list.sort((a, b) => (updatedMs.get(a) ?? 0) - (updatedMs.get(b) ?? 0));
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
		{columns}
		{mobileFields}
		rowActions={canMutateVariables ? RowActions : undefined}
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
		{#if item.injectIntoContainers}
			<Badge variant="blue" size="sm" title={m.inject_into_all_containers()}>
				<ContainersIcon class="size-3" />
				{m.injected()}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet ValueCell({ item }: { item: GlobalVariable })}
	{#if item.isSecret}
		<span class="font-mono text-muted-foreground select-none">••••••••</span>
	{:else}
		<div class="flex items-center gap-2">
			<span class="max-w-70 truncate font-mono text-sm">{item.value}</span>
			<CopyButton text={item.value} class="size-6" />
		</div>
	{/if}
{/snippet}

{#snippet ScopeCell({ item }: { item: GlobalVariable })}
	<EnvironmentScopeCell allEnvironments={item.allEnvironments} environmentIds={item.environmentIds} {envNameById} />
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
		badges={[
			(item: GlobalVariable) => (item.isSecret ? { variant: 'amber' as const, text: m.secret() } : null),
			(item: GlobalVariable) => (item.injectIntoContainers ? { variant: 'blue' as const, text: m.injected() } : null)
		]}
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
				getValue: (item: GlobalVariable) => scopeLabel(item.allEnvironments, item.environmentIds, envNameById),
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
		rowActions={canMutateVariables ? RowActions : undefined}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: GlobalVariable })}
	<RowActionsMenu name={item.key}>
		<IfPermitted perm="variables:update" envId={GLOBAL_SCOPE}>
			<DropdownMenu.Item onclick={() => onEdit(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>
		</IfPermitted>
		<IfPermitted perm="variables:delete" envId={GLOBAL_SCOPE}>
			<RemoveMenuItem onclick={() => onDelete(item)} label={m.common_delete()} />
		</IfPermitted>
	</RowActionsMenu>
{/snippet}
