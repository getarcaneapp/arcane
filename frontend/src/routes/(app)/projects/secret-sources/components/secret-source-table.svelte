<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RemoveMenuItem from '#lib/components/arcane-table/cells/remove-menu-item.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import ProviderIcon from '#lib/components/secret-sources/provider-icon.svelte';
	import { providerLabel } from '#lib/components/secret-sources/providers.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EditIcon, GlobeIcon, LockIcon, ClockIcon, TestIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { SecretSource } from '#lib/types/secretsource.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';

	let {
		sources,
		onEdit,
		onDelete,
		onTest
	}: {
		sources: SecretSource[];
		onEdit: (source: SecretSource) => void;
		onDelete: (source: SecretSource) => void;
		onTest: (source: SecretSource) => void;
	} = $props();

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	const filteredSources = $derived.by(() => {
		const query = (requestOptions.search ?? '').trim().toLowerCase();
		const list = query
			? sources.filter((source) => source.name.toLowerCase().includes(query) || endpointOf(source).toLowerCase().includes(query))
			: [...sources];
		if (requestOptions.sort?.column === 'name') {
			list.sort((a, b) => a.name.localeCompare(b.name));
			if (requestOptions.sort.direction === 'desc') list.reverse();
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

	// Where Arcane connects for this source. An empty Infisical site URL means Infisical Cloud.
	function endpointOf(source: SecretSource): string {
		return source.settings.infisical?.siteUrl || 'https://app.infisical.com';
	}

	const providerBadges: Record<SecretSource['provider'], BadgeVariant> = {
		infisical: 'violet'
	};

	function testLabel(source: SecretSource): string {
		if (!source.lastTestedAt) return m.secret_sources_never_tested();
		return m.secret_sources_last_tested({ time: formatRelativeTime(source.lastTestedAt) });
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ id: 'endpoint', accessorFn: (source) => endpointOf(source), title: m.secret_sources_endpoint(), cell: SiteCell },
		{ accessorKey: 'bindingCount', title: m.projects_title(), cell: UsageCell },
		{ accessorKey: 'lastTestedAt', title: m.common_status(), cell: StatusCell }
	] satisfies ColumnSpec<SecretSource>[];

	const mobileFields = [
		{ id: 'endpoint', label: m.secret_sources_endpoint(), defaultVisible: true },
		{ id: 'lastTestedAt', label: m.common_status(), defaultVisible: true }
	];
</script>

<ArcaneTable
	persistKey="arcane-secret-sources-table"
	items={tableData}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	withoutPagination
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={SourceMobileCard}
/>

{#snippet NameCell({ item }: { item: SecretSource })}
	<div class="flex items-center gap-2">
		<ProviderIcon provider={item.provider} class="size-6 rounded-md p-1" />
		<span class="font-medium">{item.name}</span>
		<Badge variant={providerBadges[item.provider]} size="sm">{providerLabel(item.provider)}</Badge>
	</div>
{/snippet}

{#snippet SiteCell({ item }: { item: SecretSource })}
	<span class="max-w-70 truncate font-mono text-sm text-muted-foreground">{endpointOf(item)}</span>
{/snippet}

{#snippet UsageCell({ item }: { item: SecretSource })}
	<Badge variant={item.bindingCount > 0 ? 'blue' : 'gray'} size="sm">
		{item.bindingCount === 1 ? m.secret_sources_used_by_one() : m.secret_sources_used_by({ count: item.bindingCount })}
	</Badge>
{/snippet}

{#snippet StatusCell({ item }: { item: SecretSource })}
	{#if item.lastTestError}
		<Badge variant="red" size="sm" title={item.lastTestError}>{m.secret_sources_test_failed()}</Badge>
	{:else}
		<span class="text-sm text-muted-foreground">{testLabel(item)}</span>
	{/if}
{/snippet}

{#snippet SourceMobileCard({ item, mobileFieldVisibility }: { item: SecretSource; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: LockIcon, variant: 'purple' }}
		title={(item: SecretSource) => item.name}
		badges={item.lastTestError ? [{ variant: 'red' as const, text: m.secret_sources_test_failed() }] : []}
		fields={[
			{
				label: m.secret_sources_endpoint(),
				getValue: (item: SecretSource) => endpointOf(item),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['endpoint'] ?? true
			},
			{
				label: m.common_status(),
				getValue: (item: SecretSource) => testLabel(item),
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['lastTestedAt'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SecretSource })}
	<RowActionsMenu>
		<IfPermitted perm="secret-sources:test">
			<DropdownMenu.Item onclick={() => onTest(item)}>
				<TestIcon class="size-4" />
				{m.test_connection()}
			</DropdownMenu.Item>
		</IfPermitted>
		<IfPermitted perm="secret-sources:update">
			<DropdownMenu.Item onclick={() => onEdit(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>
		</IfPermitted>
		<IfPermitted perm="secret-sources:delete">
			<RemoveMenuItem onclick={() => onDelete(item)} label={m.common_delete()} />
		</IfPermitted>
	</RowActionsMenu>
{/snippet}
