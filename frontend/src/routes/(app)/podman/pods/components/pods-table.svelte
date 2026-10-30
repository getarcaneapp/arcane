<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { BoxIcon, CalendarIcon, ContainersIcon, InspectIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import type { PodmanPodAction } from '#lib/services/podman-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanPod } from '#lib/types/podman.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { hasShownPermission, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';

	import { confirmRemovePod, podActionLabels, runPodAction } from '../pod-actions';

	type PodRow = EnvironmentRow<PodmanPod>;

	let {
		pods,
		requestOptions = $bindable(),
		onRetry
	}: {
		pods: EnvironmentPage<Paginated<PodmanPod>>;
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
	} = $props();

	const queryClient = useQueryClient();
	const scope = $derived(environmentStore.shownKey);

	async function invalidatePods() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.podsPrefix(scope) });
	}

	const canManagePods = $derived(hasShownPermission('podman:pods'));
	const lifecycleActions: PodmanPodAction[] = ['start', 'stop', 'restart', 'pause', 'unpause', 'kill'];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	const columns = $derived([
		...environmentColumns<PodRow>(),
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'containers', title: m.containers(), cell: ContainersCell },
		{ accessorKey: 'networks', title: m.resource_networks_cap(), cell: NetworksCell },
		{ accessorKey: 'created', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<PodRow>[]);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'containers', label: m.containers(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item }: { item: PodRow })}
	<a
		class="font-medium hover:underline"
		href={resolve(`/(app)/podman/pods/[id]?env=${encodeURIComponent(item.environmentId)}`, { id: encodeURIComponent(item.id) })}
		>{item.name}</a
	>
{/snippet}

{#snippet StatusCell({ item }: { item: PodRow })}
	<Badge variant={podmanStateVariant(item.status)} minWidth="20">{item.status}</Badge>
{/snippet}

{#snippet ContainersCell({ item }: { item: PodRow })}
	{item.containers?.length ?? 0}
{/snippet}

{#snippet NetworksCell({ item }: { item: PodRow })}
	{item.networks?.join(', ') || '-'}
{/snippet}

{#snippet CreatedCell({ item }: { item: PodRow })}
	{formatDateTimeShort(item.created)}
{/snippet}

{#snippet PodMobileCard({ item, mobileFieldVisibility }: { item: PodRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: BoxIcon, variant: podmanStateVariant(item.status) === 'green' ? 'emerald' : 'gray' }}
		title={(item: PodRow) => item.name}
		subtitle={(item: PodRow) => ((mobileFieldVisibility['id'] ?? false) ? item.id : null)}
		badges={(mobileFieldVisibility['status'] ?? true)
			? [(item: PodRow) => ({ variant: podmanStateVariant(item.status), text: item.status })]
			: []}
		fields={[
			{
				label: m.containers(),
				getValue: (item: PodRow) => String(item.containers?.length ?? 0),
				icon: ContainersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['containers'] ?? true
			}
		]}
		footer={(mobileFieldVisibility['created'] ?? true)
			? {
					label: m.common_created(),
					getValue: (item: PodRow) => formatDateTimeShort(item.created),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() =>
			goto(resolve(`/(app)/podman/pods/[id]?env=${encodeURIComponent(item.environmentId)}`, { id: encodeURIComponent(item.id) }))}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: PodRow })}
	<RowActionsMenu>
		<DropdownMenu.Item
			onclick={() =>
				goto(
					resolve(`/(app)/podman/pods/[id]?env=${encodeURIComponent(item.environmentId)}`, { id: encodeURIComponent(item.id) })
				)}
		>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>
		{#if canManagePods && hasPermission('podman:pods', item.environmentId)}
			<DropdownMenu.Separator />
			{#each lifecycleActions as action (action)}
				<DropdownMenu.Item onclick={() => runPodAction(item, action, invalidatePods)}>
					{podActionLabels[action]()}
				</DropdownMenu.Item>
			{/each}
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => confirmRemovePod(item, invalidatePods)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		{/if}
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-podman-pods-table"
	items={pods}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	{onRetry}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={PodMobileCard}
/>
