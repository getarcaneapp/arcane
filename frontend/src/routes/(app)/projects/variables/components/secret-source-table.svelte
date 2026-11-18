<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import ActionMenuItem from '#lib/components/arcane-table/cells/action-menu-item.svelte';
	import EnabledStatusCell from '#lib/components/arcane-table/cells/enabled-status-cell.svelte';
	import RelativeTimeCell from '#lib/components/arcane-table/cells/relative-time-cell.svelte';
	import RemoveMenuItem from '#lib/components/arcane-table/cells/remove-menu-item.svelte';
	import EnvironmentScopeCell, { scopeLabel } from '#lib/components/arcane-table/cells/scope-cell.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ApiKeyIcon, ClockIcon, ContainersIcon, EditIcon, GlobeIcon, RefreshIcon, VariableIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { Environment } from '#lib/types/environment.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SecretSource } from '#lib/types/secretsource.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { hasAnyPermission } from '#lib/utils/auth.js';
	import { formatDateTime, instantEpochMilliseconds } from '#lib/utils/formatting.js';

	let {
		sources,
		environments,
		refreshingIds,
		onEdit,
		onRefresh,
		onDelete
	}: {
		sources: SecretSource[];
		environments: Environment[];
		refreshingIds: Record<string, boolean>;
		onEdit: (source: SecretSource) => void;
		onRefresh: (source: SecretSource) => void;
		onDelete: (source: SecretSource) => void;
	} = $props();

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	const canMutateSources = $derived(
		hasAnyPermission(['secret-sources:update', 'secret-sources:refresh', 'secret-sources:delete'], GLOBAL_SCOPE)
	);

	const envNameById = $derived(new Map(environments.map((environment) => [environment.id, environment.name])));

	function providerLabel(source: SecretSource): string {
		return source.definition.name || source.presetId || m.custom_definition();
	}

	const filteredSources = $derived.by(() => {
		const query = (requestOptions.search ?? '').trim().toLowerCase();
		const list = query
			? sources.filter(
					(source) => source.name.toLowerCase().includes(query) || providerLabel(source).toLowerCase().includes(query)
				)
			: [...sources];

		const sort = requestOptions.sort;
		if (sort?.column === 'name') {
			list.sort((a, b) => a.name.localeCompare(b.name));
			if (sort.direction === 'desc') list.reverse();
		} else if (sort?.column === 'lastKeyCount') {
			list.sort((a, b) => a.lastKeyCount - b.lastKeyCount);
			if (sort.direction === 'desc') list.reverse();
		} else if (sort?.column === 'lastFetchedAt') {
			const fetchedMs = new Map(list.map((s) => [s, s.lastFetchedAt ? (instantEpochMilliseconds(s.lastFetchedAt) ?? 0) : 0]));
			list.sort((a, b) => (fetchedMs.get(a) ?? 0) - (fetchedMs.get(b) ?? 0));
			if (sort.direction === 'desc') list.reverse();
		}
		return list;
	});

	const tableData = $derived<Paginated<SecretSource>>({
		data: filteredSources,
		pagination: {
			totalPages: 1,
			totalItems: filteredSources.length,
			currentPage: 1,
			itemsPerPage: Math.max(filteredSources.length, 1)
		}
	});

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ id: 'preset', accessorFn: (row) => row.id, title: m.preset(), cell: PresetCell },
		{ id: 'scope', accessorFn: (row) => row.id, title: m.common_scope(), cell: ScopeCell },
		{ accessorKey: 'lastKeyCount', title: m.keys_fetched(), sortable: true, cell: KeysCell },
		{ accessorKey: 'lastFetchedAt', title: m.last_fetched(), sortable: true, cell: FetchedCell },
		{ accessorKey: 'enabled', title: m.common_enabled(), cell: EnabledCell }
	] satisfies ColumnSpec<SecretSource>[];

	const mobileFields = [
		{ id: 'preset', label: m.preset(), defaultVisible: true },
		{ id: 'scope', label: m.common_scope(), defaultVisible: true },
		{ id: 'lastKeyCount', label: m.keys_fetched(), defaultVisible: true },
		{ id: 'lastFetchedAt', label: m.last_fetched(), defaultVisible: true }
	];
</script>

{#if sources.length === 0}
	<div class="flex flex-col items-center justify-center py-12 text-sm text-muted-foreground">
		<ApiKeyIcon class="mb-3 size-10 opacity-40" />
		<p class="font-medium">{m.no_external_secrets()}</p>
		<p class="mt-1">{m.no_external_secrets_description()}</p>
	</div>
{:else}
	<ArcaneTable
		persistKey="arcane-secret-sources-table"
		items={tableData}
		bind:requestOptions
		bind:mobileFieldVisibility
		selectionDisabled={true}
		withoutPagination
		{columns}
		{mobileFields}
		rowActions={canMutateSources ? RowActions : undefined}
		mobileCard={SourceMobileCardSnippet}
	/>
{/if}

{#snippet NameCell({ item }: { item: SecretSource })}
	<div class="flex min-w-0 flex-col gap-1">
		<div class="flex items-center gap-2">
			<span class="text-sm font-medium">{item.name}</span>
			{#if item.injectIntoContainers}
				<Badge variant="blue" size="sm" title={m.inject_into_all_containers()}>
					<ContainersIcon class="size-3" />
					{m.injected()}
				</Badge>
			{/if}
		</div>
		{#if item.lastError}
			<p class="line-clamp-2 max-w-100 text-xs text-destructive" title={item.lastError}>{item.lastError}</p>
		{/if}
	</div>
{/snippet}

{#snippet PresetCell({ item }: { item: SecretSource })}
	<div class="flex items-center gap-2">
		<span class="text-sm">{providerLabel(item)}</span>
		<Badge variant="gray" size="sm">{item.kind}</Badge>
	</div>
{/snippet}

{#snippet ScopeCell({ item }: { item: SecretSource })}
	<EnvironmentScopeCell allEnvironments={item.allEnvironments} environmentIds={item.environmentIds} {envNameById} />
{/snippet}

{#snippet KeysCell({ item }: { item: SecretSource })}
	<div class="flex flex-col">
		<span class="text-sm tabular-nums">{item.lastKeyCount}</span>
		{#if item.lastSkippedKeyCount > 0}
			<span class="text-xs text-muted-foreground">{m.count_keys_skipped({ count: item.lastSkippedKeyCount })}</span>
		{/if}
	</div>
{/snippet}

{#snippet FetchedCell({ item }: { item: SecretSource })}
	<RelativeTimeCell value={item.lastFetchedAt} />
{/snippet}

{#snippet EnabledCell({ item }: { item: SecretSource })}
	<EnabledStatusCell value={item.enabled} />
{/snippet}

{#snippet SourceMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SecretSource;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: ApiKeyIcon, variant: 'blue' }}
		title={(item: SecretSource) => item.name}
		subtitle={(item: SecretSource) => item.lastError ?? null}
		badges={[
			(item: SecretSource) =>
				item.enabled
					? { variant: 'green' as const, text: m.common_enabled() }
					: { variant: 'red' as const, text: m.common_disabled() },
			(item: SecretSource) => (item.injectIntoContainers ? { variant: 'blue' as const, text: m.injected() } : null)
		]}
		fields={[
			{
				label: m.preset(),
				getValue: (item: SecretSource) => providerLabel(item),
				icon: ApiKeyIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['preset'] ?? true
			},
			{
				label: m.common_scope(),
				getValue: (item: SecretSource) => scopeLabel(item.allEnvironments, item.environmentIds, envNameById),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['scope'] ?? true
			},
			{
				label: m.keys_fetched(),
				getValue: (item: SecretSource) => String(item.lastKeyCount),
				icon: VariableIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['lastKeyCount'] ?? true
			},
			{
				label: m.last_fetched(),
				getValue: (item: SecretSource) =>
					item.lastFetchedAt ? formatDateTime(item.lastFetchedAt) || item.lastFetchedAt : m.common_never(),
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['lastFetchedAt'] ?? true
			}
		]}
		rowActions={canMutateSources ? RowActions : undefined}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SecretSource })}
	<RowActionsMenu name={item.name}>
		<IfPermitted perm="secret-sources:update" envId={GLOBAL_SCOPE}>
			<DropdownMenu.Item onclick={() => onEdit(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>
		</IfPermitted>
		<IfPermitted perm="secret-sources:refresh" envId={GLOBAL_SCOPE}>
			<ActionMenuItem
				icon={RefreshIcon}
				label={m.common_refresh()}
				loading={refreshingIds[item.id] ?? false}
				onclick={() => onRefresh(item)}
			/>
		</IfPermitted>
		<IfPermitted perm="secret-sources:delete" envId={GLOBAL_SCOPE}>
			<RemoveMenuItem onclick={() => onDelete(item)} label={m.common_delete()} />
		</IfPermitted>
	</RowActionsMenu>
{/snippet}
