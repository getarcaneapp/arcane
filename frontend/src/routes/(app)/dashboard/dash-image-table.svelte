<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
	import type { ImageSummaryDto } from '$lib/types/image.type';
	import bytes from '$lib/utils/bytes';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { goto } from '$app/navigation';
	import { IsMobile } from '$lib/hooks';
	import { ImagesIcon, ArrowRightIcon } from '$lib/icons';

	let {
		images = $bindable(),
		isLoading
	}: {
		images: Paginated<ImageSummaryDto>;
		isLoading: boolean;
	} = $props();

	const isMobile = new IsMobile();
	let displayLimit = $state(images.pagination?.itemsPerPage ?? 5);
	let lastMeasuredHeight = $state(0);

	const MOBILE_ROWS = 4;
	const ROW_HEIGHT = 57;
	const HEADER_HEIGHT = 145;
	const FOOTER_HEIGHT = 48;
	const MIN_ROWS = 3;
	const MAX_ROWS = 50;

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 5 },
		sort: { column: 'size', direction: 'desc' }
	});

	let selectedIds = $state<string[]>([]);

	function shouldReserveFooter(limit: number) {
		const totalItems = images.pagination?.totalItems ?? 0;
		return totalItems > limit;
	}

	function calculateLimitForHeight(height: number) {
		if (isMobile.current) return MOBILE_ROWS;
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

	function updateRequestLimit(limit: number) {
		requestOptions = {
			...requestOptions,
			pagination: {
				page: requestOptions.pagination?.page ?? 1,
				limit
			}
		};
	}

	function handleLayoutChange(height: number) {
		lastMeasuredHeight = height;
		const nextLimit = calculateLimitForHeight(height);
		displayLimit = nextLimit;
		updateRequestLimit(nextLimit);
	}

	async function refreshImages(options: SearchPaginationSortRequest) {
		requestOptions = options;
		const result = await imageService.getImages(options);
		images = result;
		displayLimit = result.pagination?.itemsPerPage ?? displayLimit;
		handleLayoutChange(lastMeasuredHeight);
		return result;
	}

	const columns = [
		{ accessorKey: 'repoTags', title: m.images_repository(), cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), cell: StatusCell },
		{ id: 'tag', title: m.images_tag(), cell: TagCell },
		{ accessorKey: 'size', title: m.common_size(), cell: SizeCell }
	] satisfies ColumnSpec<ImageSummaryDto>[];
</script>

{#snippet NameCell({ item }: { item: ImageSummaryDto })}
	<div class="flex items-center gap-2">
		<div class="flex flex-1 items-center">
			<a class="shrink truncate font-medium hover:underline" href="/images/{item.id}">
				{#if item.repo && item.repo !== '<none>'}
					{item.repo}
				{:else}
					<span class="text-muted-foreground italic">{m.images_untagged()}</span>
				{/if}
			</a>
		</div>
	</div>
{/snippet}

{#snippet StatusCell({ item }: { item: ImageSummaryDto })}
	{#if item.inUse}
		<StatusBadge text={m.common_in_use()} variant="green" />
	{:else}
		<StatusBadge text={m.common_unused()} variant="amber" />
	{/if}
{/snippet}

{#snippet TagCell({ item }: { item: ImageSummaryDto })}
	{#if item.tag && item.tag !== '<none>'}
		{item.tag}
	{:else}
		<span class="text-muted-foreground italic">{m.images_none_label()}</span>
	{/if}
{/snippet}

{#snippet SizeCell({ item }: { item: ImageSummaryDto })}
	{bytes.format(item.size)}
{/snippet}

{#snippet DashImageMobileCard({ row, item }: { row: any; item: ImageSummaryDto })}
	<UniversalMobileCard
		{item}
		icon={(item: ImageSummaryDto) => ({
			component: ImagesIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item: ImageSummaryDto) => {
			if (item.repo && item.repo !== '<none>') {
				return item.repo;
			}
			return m.images_untagged();
		}}
		badges={[
			(item: ImageSummaryDto) =>
				item.inUse ? { variant: 'green', text: m.common_in_use() } : { variant: 'amber', text: m.common_unused() }
		]}
		fields={[
			{
				label: m.common_size(),
				getValue: (item: ImageSummaryDto) => bytes.format(item.size)
			}
		]}
		compact
		onclick={(item: ImageSummaryDto) => goto(`/images/${item.id}`)}
	/>
{/snippet}

<div
	class="flex flex-col lg:h-full lg:min-h-0"
	bind:clientHeight={() => lastMeasuredHeight, (value) => handleLayoutChange(value)}
>
	<Card.Root class="flex flex-col lg:h-full lg:min-h-0">
		<Card.Header icon={ImagesIcon} class="shrink-0">
			<div class="flex flex-1 items-center justify-between">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2><a class="hover:underline" href="/images">{m.images_title()}</a></h2>
					</Card.Title>
					<Card.Description>{m.images_top_largest()}</Card.Description>
				</div>
				<ArcaneButton action="base" tone="ghost" size="sm" href="/images" disabled={isLoading}>
					{m.common_view_all()}
					<ArrowRightIcon class="size-4" />
				</ArcaneButton>
			</div>
		</Card.Header>
		<Card.Content class="px-0 lg:flex lg:min-h-0 lg:flex-1 lg:flex-col">
			<ArcaneTable
				items={{ ...images, data: images.data.slice(0, displayLimit) }}
				bind:requestOptions
				bind:selectedIds
				onRefresh={refreshImages}
				{columns}
				mobileCard={DashImageMobileCard}
				withoutSearch
				selectionDisabled
				withoutPagination
				unstyled
			/>
		</Card.Content>
		{#if images.data.length >= displayLimit && images.pagination.totalItems > displayLimit}
			<Card.Footer class="border-t px-6 py-3">
				<span class="text-muted-foreground text-xs">
					{m.images_showing_of_total({ shown: displayLimit, total: images.pagination.totalItems })}
				</span>
			</Card.Footer>
		{/if}
	</Card.Root>
</div>
