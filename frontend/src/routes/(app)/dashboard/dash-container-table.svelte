<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
	import type { ContainerSummaryDto } from '$lib/types/container.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { containerService } from '$lib/services/container-service';
	import { goto } from '$app/navigation';
	import { untrack } from 'svelte';
	import { IsMobile } from '$lib/hooks';
	import { ContainersIcon, ArrowRightIcon } from '$lib/icons';

	let {
		containers = $bindable(),
		isLoading
	}: {
		containers: Paginated<ContainerSummaryDto>;
		isLoading: boolean;
	} = $props();

	const isMobile = new IsMobile();
	let selectedIds = $state<string[]>([]);
	let contentHeight = $state(0);
	let lastFetchedLimit = $state(5);

	// Estimate row height: ~57px per row (including borders/padding), plus ~145px for header
	const ROW_HEIGHT = 57;
	const HEADER_HEIGHT = 145;
	const FOOTER_HEIGHT = 48; // Reserve space for the "showing" footer overlay
	const MIN_ROWS = 3;
	const MAX_ROWS = 50;

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 5 },
		sort: { column: 'created', direction: 'desc' }
	});

	const shouldReserveFooter = $derived.by(() => {
		const limit = requestOptions.pagination?.limit ?? containers.pagination?.itemsPerPage ?? MIN_ROWS;
		const dataLength = containers.data?.length ?? 0;
		const totalItems = containers.pagination?.totalItems ?? 0;
		return dataLength >= limit && totalItems > limit;
	});

	const calculatedLimit = $derived.by(() => {
		if (isMobile.current) return 10;
		if (contentHeight <= 0) return 5;
		let availableHeight = contentHeight - HEADER_HEIGHT;
		if (shouldReserveFooter) {
			availableHeight -= FOOTER_HEIGHT;
		}
		const rows = Math.floor(Math.max(0, availableHeight) / ROW_HEIGHT);
		return Math.max(MIN_ROWS, Math.min(MAX_ROWS, rows));
	});

	$effect(() => {
		const limit = calculatedLimit;
		const currentLimit = containers.pagination?.itemsPerPage;

		const tid = untrack(() => {
			if (requestOptions.pagination && (limit !== lastFetchedLimit || (currentLimit !== undefined && currentLimit !== limit))) {
				return setTimeout(() => {
					untrack(() => {
						lastFetchedLimit = limit;
						if (requestOptions.pagination) {
							requestOptions.pagination.limit = limit;
							containerService.getContainers(requestOptions).then((result) => {
								untrack(() => {
									containers = result;
								});
							});
						}
					});
				}, 300);
			}
		});

		if (tid) return () => clearTimeout(tid);
	});

	const columns = [
		{ accessorKey: 'names', title: m.common_name(), cell: NameCell },
		{ accessorKey: 'image', title: m.common_image() },
		{ accessorKey: 'state', title: m.common_state(), cell: StateCell },
		{ accessorKey: 'status', title: m.common_status() }
	] satisfies ColumnSpec<ContainerSummaryDto>[];
</script>

{#snippet NameCell({ item }: { item: ContainerSummaryDto })}
	<a class="font-medium hover:underline" href="/containers/{item.id}">
		{#if item.names && item.names.length > 0}
			{item.names[0].startsWith('/') ? item.names[0].substring(1) : item.names[0]}
		{:else}
			{item.id.substring(0, 12)}
		{/if}
	</a>
{/snippet}

{#snippet StateCell({ item }: { item: ContainerSummaryDto })}
	<StatusBadge variant={getStatusVariant(item.state)} text={capitalizeFirstLetter(item.state)} />
{/snippet}

{#snippet DashContainerMobileCard({ row, item }: { row: any; item: ContainerSummaryDto })}
	<UniversalMobileCard
		{item}
		icon={(item) => {
			const state = item.state;
			return {
				component: ContainersIcon,
				variant: state === 'running' ? 'emerald' : state === 'exited' ? 'red' : 'amber'
			};
		}}
		title={(item) => {
			if (item.names && item.names.length > 0) {
				return item.names[0].startsWith('/') ? item.names[0].substring(1) : item.names[0];
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

<div class="flex h-full min-h-0 flex-col" bind:clientHeight={contentHeight}>
	<Card.Root class="flex h-full min-h-0 flex-col">
		<Card.Header icon={ContainersIcon} class="shrink-0">
			<div class="flex flex-1 items-center justify-between">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.containers_title()}</h2>
					</Card.Title>
					<Card.Description>{m.containers_recent()}</Card.Description>
				</div>
				<ArcaneButton action="base" tone="ghost" size="sm" href="/containers" disabled={isLoading}>
					{m.common_view_all()}
					<ArrowRightIcon class="size-4" />
				</ArcaneButton>
			</div>
		</Card.Header>
		<Card.Content class="flex min-h-0 flex-1 flex-col px-0">
			<ArcaneTable
				items={{ ...containers, data: containers.data.slice(0, calculatedLimit) }}
				bind:requestOptions
				bind:selectedIds
				onRefresh={async (options) => (containers = await containerService.getContainers(options))}
				withoutSearch={true}
				withoutPagination={true}
				selectionDisabled={true}
				unstyled={true}
				{columns}
				mobileCard={DashContainerMobileCard}
			/>
		</Card.Content>
		{#if containers.data.length >= calculatedLimit && containers.pagination.totalItems > calculatedLimit}
			<Card.Footer class="border-t px-6 py-3">
				<span class="text-muted-foreground text-xs">
					{m.containers_showing_of_total({ shown: calculatedLimit, total: containers.pagination.totalItems })}
				</span>
			</Card.Footer>
		{/if}
	</Card.Root>
</div>
