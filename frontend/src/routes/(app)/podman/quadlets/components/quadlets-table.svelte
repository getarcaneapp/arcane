<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { FileTextIcon, InspectIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { PodmanQuadletDetail, PodmanStatus } from '#lib/types/podman.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { goto } from '#lib/utils/base-path.js';
	import { environmentHref, hasShownPermission, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';

	import { confirmRemoveQuadlet, quadletActionLabels, quadletActions, runQuadletAction } from '../quadlet-actions';

	type QuadletRow = EnvironmentRow<PodmanQuadletDetail> & { id: string };

	let {
		quadlets = $bindable(),
		requestOptions = $bindable(),
		podmanStatuses,
		onRefreshData
	}: {
		quadlets: EnvironmentPage<Paginated<PodmanQuadletDetail>>;
		requestOptions: SearchPaginationSortRequest;
		podmanStatuses: Record<string, PodmanStatus>;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	const canManageQuadlets = $derived.by(() => {
		userStore.current;
		return hasShownPermission('podman:quadlets');
	});
	const canRunLifecycle = $derived.by(() => {
		userStore.current;
		return hasShownPermission('podman:quadlets:lifecycle');
	});

	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	function withRowIds(page: EnvironmentPage<Paginated<PodmanQuadletDetail>>) {
		return { ...page, data: page.data.map((quadlet): QuadletRow => ({ ...quadlet, id: quadlet.name })) };
	}

	const rows = $derived(withRowIds(quadlets));

	const quadletHref = (quadlet: QuadletRow) =>
		environmentHref(`/podman/quadlets/${encodeURIComponent(quadlet.name)}`, quadlet.environmentId);

	const columns = $derived([
		...environmentColumns<QuadletRow>(),
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'type', title: m.common_type(), sortable: true, cell: TypeCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'unitName', title: m.podman_unit(), sortable: true, cell: UnitCell },
		{ accessorKey: 'app', title: m.podman_quadlet_app(), sortable: true, cell: AppCell }
	] satisfies ColumnSpec<QuadletRow>[]);

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'unitName', label: m.podman_unit(), defaultVisible: true },
		{ id: 'app', label: m.podman_quadlet_app(), defaultVisible: false }
	];
</script>

{#snippet NameCell({ item }: { item: QuadletRow })}
	<div class="flex items-center gap-2">
		<a class="font-medium hover:underline" href={quadletHref(item)}>{item.name}</a>
		{#if item.template}
			<Badge variant="gray" size="sm">{m.podman_quadlet_template()}</Badge>
		{/if}
	</div>
{/snippet}

{#snippet TypeCell({ item }: { item: QuadletRow })}
	<Badge variant="gray" size="sm">.{item.type}</Badge>
{/snippet}

{#snippet StatusCell({ item }: { item: QuadletRow })}
	<Badge variant={podmanStateVariant(item.status)} minWidth="20">{item.status}</Badge>
{/snippet}

{#snippet UnitCell({ item }: { item: QuadletRow })}
	<span class="font-mono text-xs text-muted-foreground">{item.unitName || '-'}</span>
{/snippet}

{#snippet AppCell({ item }: { item: QuadletRow })}
	{item.app || '-'}
{/snippet}

{#snippet QuadletMobileCard({ item, mobileFieldVisibility }: { item: QuadletRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: FileTextIcon, variant: podmanStateVariant(item.status) === 'green' ? 'emerald' : 'gray' }}
		title={(item: QuadletRow) => item.name}
		badges={(mobileFieldVisibility['status'] ?? true)
			? [(item: QuadletRow) => ({ variant: podmanStateVariant(item.status), text: item.status })]
			: []}
		fields={[
			{
				label: m.common_type(),
				getValue: (item: QuadletRow) => `.${item.type}`,
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
			},
			{
				label: m.podman_quadlet_app(),
				getValue: (item: QuadletRow) => item.app || '-',
				icon: FileTextIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['app'] ?? false
			}
		]}
		rowActions={RowActions}
		onclick={() => goto(quadletHref(item))}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: QuadletRow })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(quadletHref(item))}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>
		{#if canRunLifecycle && hasPermission('podman:quadlets:lifecycle', item.environmentId) && !item.template}
			{@const status = podmanStatuses[item.environmentId]}
			<DropdownMenu.Separator />
			{#each quadletActions as action (action)}
				<DropdownMenu.Item
					disabled={!status?.systemdAvailable}
					title={status?.systemdAvailable ? undefined : status?.systemdReason}
					onclick={() =>
						runQuadletAction(item.name, action, item.environmentId, async () => {
							await onRefreshData(requestOptions);
						})}
				>
					{quadletActionLabels[action]()}
				</DropdownMenu.Item>
			{/each}
		{/if}
		{#if canManageQuadlets && hasPermission('podman:quadlets', item.environmentId)}
			<DropdownMenu.Separator />
			<DropdownMenu.Item
				variant="destructive"
				onclick={() =>
					confirmRemoveQuadlet(item.name, item.environmentId, async () => {
						await onRefreshData(requestOptions);
					})}
			>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		{/if}
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-podman-quadlets-table"
	items={rows}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => {
		requestOptions = options;
		await onRefreshData(options);
		return withRowIds(quadlets);
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={QuadletMobileCard}
/>
