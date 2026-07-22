<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import DashTableCardHeader from './dash-table-card-header.svelte';
	import { getStatusVariant } from '$lib/utils/docker';
	import { capitalizeFirstLetter } from '$lib/utils/formatting';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/shared';
	import type { ContainerSummaryDto } from '$lib/types/docker';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { containerService } from '$lib/services/container-service';
	import { goto } from '$app/navigation';
	import { mode } from 'mode-watcher';
	import { useResponsiveTableLimit } from '$lib/hooks/use-responsive-table-limit.svelte';
	import { ContainersIcon } from '$lib/icons';
	import IconImage from '$lib/components/icon-image.svelte';
	import { getThemedIconUrl } from '$lib/utils/docker';

	let {
		containers = $bindable(),
		isLoading
	}: {
		containers: Paginated<ContainerSummaryDto>;
		isLoading: boolean;
	} = $props();

	let selectedIds = $state<string[]>([]);
	const tableLimit = useResponsiveTableLimit({
		initialLimit: containers.pagination?.itemsPerPage ?? 5,
		sort: { column: 'created', direction: 'desc' },
		getTotalItems: () => containers.pagination?.totalItems ?? 0
	});

	async function refreshContainers(options: SearchPaginationSortRequest) {
		tableLimit.requestOptions = options;
		const result = await containerService.getContainers(options);
		containers = result;
		return result;
	}

	const columns = [
		{ accessorKey: 'names', title: m.common_name(), cell: NameCell },
		{ accessorKey: 'image', title: m.common_image() },
		{ accessorKey: 'state', title: m.common_state(), cell: StateCell },
		{ accessorKey: 'status', title: m.common_status() }
	] satisfies ColumnSpec<ContainerSummaryDto>[];
</script>

{#snippet NameCell({ item }: { item: ContainerSummaryDto })}
	{@const firstName = item.names?.[0] ?? ''}
	{@const displayName = firstName ? (firstName.startsWith('/') ? firstName.substring(1) : firstName) : item.id.substring(0, 12)}
	{@const iconUrl = getThemedIconUrl(item, mode.current)}
	<div class="flex items-center gap-2">
		<IconImage src={iconUrl} alt={displayName} fallback={ContainersIcon} class="size-6" containerClass="size-8" />
		<a class="font-medium hover:underline" href="/containers/{item.id}">{displayName}</a>
	</div>
{/snippet}

{#snippet StateCell({ item }: { item: ContainerSummaryDto })}
	<Badge variant={getStatusVariant(item.state)} minWidth="20">{capitalizeFirstLetter(item.state)}</Badge>
{/snippet}

{#snippet DashContainerMobileCard({ item }: { item: ContainerSummaryDto })}
	<UniversalMobileCard
		{item}
		icon={(item) => {
			const iconUrl = getThemedIconUrl(item, mode.current);
			const state = item.state;
			return {
				component: ContainersIcon,
				variant: state === 'running' ? 'emerald' : state === 'exited' ? 'red' : 'amber',
				imageUrl: iconUrl ?? undefined,
				alt: item.names?.[0] ? item.names[0].replace(/^\//, '') : item.id.substring(0, 12)
			};
		}}
		title={(item) => {
			const first = item.names?.[0];
			if (first) {
				return first.startsWith('/') ? first.substring(1) : first;
			}
			return item.id.substring(0, 12);
		}}
		badges={[
			(item: ContainerSummaryDto) => ({
				variant: item.state === 'running' ? 'green' : item.state === 'exited' ? 'red' : 'amber',
				text: capitalizeFirstLetter(item.state)
			})
		]}
		fields={[
			{
				label: m.common_status(),
				getValue: (item: ContainerSummaryDto) => item.status,
				show: item.status !== undefined
			}
		]}
		compact
		onclick={(item: ContainerSummaryDto) => goto(`/containers/${item.id}`)}
	/>
{/snippet}

<div class="flex flex-col lg:h-full lg:min-h-0" bind:clientHeight={tableLimit.measuredHeight}>
	<Card.Root class="flex flex-col lg:h-full lg:min-h-0">
		<DashTableCardHeader
			icon={ContainersIcon}
			href="/containers"
			title={m.containers()}
			description={m.containers_recent()}
			{isLoading}
		/>
		<Card.Content class="px-0 lg:flex lg:min-h-0 lg:flex-1 lg:flex-col">
			<ArcaneTable
				items={{ ...containers, data: containers.data.slice(0, tableLimit.displayLimit) }}
				bind:requestOptions={tableLimit.requestOptions}
				bind:selectedIds
				onRefresh={refreshContainers}
				{columns}
				mobileCard={DashContainerMobileCard}
				withoutSearch
				withoutPagination
				selectionDisabled
				unstyled
			/>
		</Card.Content>
		{#if tableLimit.shouldShowFooter(containers.data.length)}
			<Card.Footer class="border-t px-6 py-3">
				<span class="text-xs text-muted-foreground">
					{m.containers_showing_of_total({ shown: tableLimit.displayLimit, total: containers.pagination.totalItems })}
				</span>
			</Card.Footer>
		{/if}
	</Card.Root>
</div>
