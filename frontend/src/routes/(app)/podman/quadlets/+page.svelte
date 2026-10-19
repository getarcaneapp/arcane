<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { FileTextIcon, StartIcon, TrashIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanQuadletDetail } from '#lib/types/podman.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';

	import { confirmRemoveQuadlet, quadletActionLabels, quadletActions, runQuadletAction } from './quadlet-actions';

	let { data } = $props();

	const queryClient = useQueryClient();
	const currentEnvId = $derived(environmentStore.selected?.id ?? '0');
	const status = $derived(data.podmanStatus);
	const quadletsSupported = $derived(status?.quadletsSupported === true);
	const systemdAvailable = $derived(status?.systemdAvailable === true);
	const canInstall = $derived(hasPermission('podman:quadlets', currentEnvId));

	const quadletsQuery = createQuery(() => ({
		queryKey: queryKeys.podman.quadlets(currentEnvId),
		queryFn: () => podmanService.getQuadlets(),
		enabled: quadletsSupported,
		refetchInterval: 15000,
		retry: false
	}));

	const quadlets = $derived(quadletsQuery.data ?? []);
	const activeQuadlets = $derived(quadlets.filter((quadlet) => podmanStateVariant(quadlet.status) === 'green').length);

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	type QuadletRow = PodmanQuadletDetail & { id: string };

	function buildTableData(items: PodmanQuadletDetail[]): Paginated<QuadletRow> {
		return {
			data: items.map((item) => ({ ...item, id: item.name })),
			pagination: {
				totalPages: 1,
				totalItems: items.length,
				currentPage: 1,
				itemsPerPage: Math.max(items.length, 1)
			}
		};
	}

	const tableData = $derived(buildTableData(quadlets));

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.quadlets(currentEnvId) });
	}

	const actionButtons: ActionButton[] = $derived([
		...(canInstall && quadletsSupported
			? [
					{
						id: 'install',
						action: 'create' as const,
						placement: 'primary' as const,
						label: m.podman_quadlet_install(),
						href: '/podman/quadlets/new'
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: quadletsQuery.isFetching,
			disabled: quadletsQuery.isFetching || !quadletsSupported
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{ title: m.podman_quadlets_total(), value: quadlets.length, icon: FileTextIcon, iconColor: 'text-info' },
		{ title: m.podman_quadlets_active(), value: activeQuadlets, icon: StartIcon, iconColor: 'text-success' }
	]);

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'type', title: m.common_type(), sortable: true, cell: TypeCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'unitName', title: m.podman_unit(), sortable: true, cell: UnitCell },
		{ accessorKey: 'app', title: m.podman_quadlet_app(), sortable: true, cell: AppCell }
	] satisfies ColumnSpec<QuadletRow>[];

	const mobileFields = [
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'unitName', label: m.podman_unit(), defaultVisible: true }
	];
</script>

<ResourcePageLayout
	title={m.podman_quadlets_title()}
	subtitle={m.podman_quadlets_subtitle()}
	icon={FileTextIcon}
	{actionButtons}
	{statCards}
	environmentPicker
>
	{#snippet mainContent()}
		{#if !quadletsSupported}
			<EmptyState icon={FileTextIcon} title={m.common_unavailable()} description={m.podman_quadlets_unsupported()} />
		{:else if quadletsQuery.isError}
			<EmptyState
				icon={FileTextIcon}
				title={m.common_load_failed({ resource: m.podman_quadlets_title() })}
				description={extractApiErrorMessage(quadletsQuery.error)}
				actionLabel={m.common_retry()}
				onAction={() => quadletsQuery.refetch()}
			/>
		{:else if !quadletsQuery.isPending && quadlets.length === 0}
			<EmptyState icon={FileTextIcon} title={m.podman_quadlets_empty()} />
		{:else}
			<ArcaneTable
				persistKey="arcane-podman-quadlets-table"
				items={tableData}
				bind:requestOptions
				bind:mobileFieldVisibility
				loading={quadletsQuery.isPending}
				selectionDisabled
				withoutSearch
				withoutPagination
				onRefresh={async () => {
					await refresh();
					return buildTableData(quadlets);
				}}
				{columns}
				{mobileFields}
				rowActions={RowActions}
				mobileCard={QuadletMobileCard}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>

{#snippet NameCell({ item }: { item: QuadletRow })}
	<div class="flex items-center gap-2">
		<a class="font-medium hover:underline" href={`/podman/quadlets/${encodeURIComponent(item.name)}`}>{item.name}</a>
		{#if item.template}
			<Badge variant="gray" size="sm">{m.podman_quadlet_template()}</Badge>
		{/if}
	</div>
{/snippet}

{#snippet TypeCell({ item }: { item: QuadletRow })}
	<span class="rounded bg-muted px-2 py-1 text-xs font-medium">{item.type}</span>
{/snippet}

{#snippet StatusCell({ item }: { item: QuadletRow })}
	<Badge variant={podmanStateVariant(item.status)} minWidth="20">{item.status}</Badge>
{/snippet}

{#snippet UnitCell({ item }: { item: QuadletRow })}
	<span class="font-mono text-xs text-muted-foreground">{item.unitName || '-'}</span>
{/snippet}

{#snippet AppCell({ item }: { item: QuadletRow })}
	<span class="text-muted-foreground">{item.app || '-'}</span>
{/snippet}

{#snippet QuadletMobileCard({ item, mobileFieldVisibility }: { item: QuadletRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: FileTextIcon, variant: 'blue' }}
		title={(item: QuadletRow) => item.name}
		badges={[(item: QuadletRow) => ({ variant: podmanStateVariant(item.status), text: item.status })]}
		fields={[
			{
				label: m.common_type(),
				getValue: (item: QuadletRow) => item.type,
				icon: FileTextIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['type'] ?? true
			},
			{
				label: m.podman_unit(),
				getValue: (item: QuadletRow) => item.unitName || '-',
				icon: FileTextIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['unitName'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: QuadletRow })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(`/podman/quadlets/${encodeURIComponent(item.name)}`)}>
			{m.common_inspect()}
		</DropdownMenu.Item>
		{#if !item.template}
			<IfPermitted perm="podman:quadlets:lifecycle">
				<DropdownMenu.Separator />
				{#each quadletActions as action (action)}
					<DropdownMenu.Item
						disabled={!systemdAvailable}
						title={systemdAvailable ? undefined : status?.systemdReason}
						onclick={() => runQuadletAction(item.name, action, refresh)}
					>
						{quadletActionLabels[action]()}
					</DropdownMenu.Item>
				{/each}
			</IfPermitted>
		{/if}
		<IfPermitted perm="podman:quadlets">
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => confirmRemoveQuadlet(item.name, refresh)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}
