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
	import { IsMobile } from '$lib/hooks';
	import { ContainersIcon, ArrowRightIcon } from '$lib/icons';
	import IconImage from '$lib/components/icon-image.svelte';
	import { getArcaneIconUrlFromLabels } from '$lib/utils/arcane-labels';

	let {
		containers = $bindable(),
		isLoading
	}: {
		containers: Paginated<ContainerSummaryDto>;
		isLoading: boolean;
	} = $props();

	const isMobile = new IsMobile();
	let selectedIds = $state<string[]>([]);
	let displayLimit = $state(containers.pagination?.itemsPerPage ?? 5);
	let lastFetchedLimit = $state(containers.pagination?.itemsPerPage ?? 5);
	let resizeDebounceTimer: ReturnType<typeof setTimeout> | null = null;
	let lastMeasuredHeight = $state(0);

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

	function shouldReserveFooter(limit: number) {
		const totalItems = containers.pagination?.totalItems ?? 0;
		return totalItems > limit;
	}

	function calculateLimitForHeight(height: number) {
		if (isMobile.current) return 10;
		if (height <= 0) return 5;

		let availableHeight = height - HEADER_HEIGHT;
		const initialRows = Math.floor(Math.max(0, availableHeight) / ROW_HEIGHT);
		const footerLimit = Math.max(MIN_ROWS, Math.min(MAX_ROWS, initialRows));
		if (shouldReserveFooter(footerLimit)) {
			availableHeight -= FOOTER_HEIGHT;
		}

		const rows = Math.floor(Math.max(0, availableHeight) / ROW_HEIGHT);
		return Math.max(MIN_ROWS, Math.min(MAX_ROWS, rows));
	}

	async function syncLimit(limit: number) {
		displayLimit = limit;

		const pagination = requestOptions.pagination;
		const currentLimit = containers.pagination?.itemsPerPage;
		if (!pagination || (limit === lastFetchedLimit && (currentLimit === undefined || currentLimit === limit))) {
			return;
		}

		if (resizeDebounceTimer) {
			clearTimeout(resizeDebounceTimer);
		}

		resizeDebounceTimer = setTimeout(async () => {
			try {
				lastFetchedLimit = limit;
				requestOptions = {
					...requestOptions,
					pagination: {
						page: pagination.page ?? 1,
						limit
					}
				};
				containers = await containerService.getContainers(requestOptions);
				void handleLayoutChange(lastMeasuredHeight);
			} catch (error) {
				console.error('Failed to sync container limit:', error);
			} finally {
				resizeDebounceTimer = null;
			}
		}, 300);
	}

	async function handleLayoutChange(height: number) {
		lastMeasuredHeight = height;
		const nextLimit = calculateLimitForHeight(height);
		await syncLimit(nextLimit);
	}

	async function refreshContainers(options: SearchPaginationSortRequest) {
		requestOptions = options;
		const result = await containerService.getContainers(options);
		containers = result;
		await handleLayoutChange(lastMeasuredHeight);
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
	{@const displayName =
		item.names && item.names.length > 0
			? item.names[0].startsWith('/')
				? item.names[0].substring(1)
				: item.names[0]
			: item.id.substring(0, 12)}
	{@const iconUrl = getArcaneIconUrlFromLabels(item.labels)}
	<div class="flex items-center gap-2">
		<IconImage src={iconUrl} alt={displayName} fallback={ContainersIcon} class="size-4" containerClass="size-7" />
		<a class="font-medium hover:underline" href="/containers/{item.id}">{displayName}</a>
	</div>
{/snippet}

{#snippet StateCell({ item }: { item: ContainerSummaryDto })}
	<StatusBadge variant={getStatusVariant(item.state)} text={capitalizeFirstLetter(item.state)} />
{/snippet}

{#snippet DashContainerMobileCard({ row, item }: { row: any; item: ContainerSummaryDto })}
	<UniversalMobileCard
		{item}
		icon={(item) => {
			const iconUrl = getArcaneIconUrlFromLabels(item.labels);
			const state = item.state;
			return {
				component: ContainersIcon,
				variant: state === 'running' ? 'emerald' : state === 'exited' ? 'red' : 'amber',
				imageUrl: iconUrl ?? undefined,
				alt: item.names?.[0] ? item.names[0].replace(/^\//, '') : item.id.substring(0, 12)
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

<div class="flex h-full min-h-0 flex-col" bind:clientHeight={() => lastMeasuredHeight, (value) => void handleLayoutChange(value)}>
	<Card.Root class="flex h-full min-h-0 flex-col">
		<Card.Header icon={ContainersIcon} class="shrink-0">
			<div class="flex flex-1 items-center justify-between">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2><a class="hover:underline" href="/containers">{m.containers_title()}</a></h2>
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
				items={{ ...containers, data: containers.data.slice(0, displayLimit) }}
				bind:requestOptions
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
		{#if containers.data.length >= displayLimit && containers.pagination.totalItems > displayLimit}
			<Card.Footer class="border-t px-6 py-3">
				<span class="text-muted-foreground text-xs">
					{m.containers_showing_of_total({ shown: displayLimit, total: containers.pagination.totalItems })}
				</span>
			</Card.Footer>
		{/if}
	</Card.Root>
</div>
