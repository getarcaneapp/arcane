<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { TABLE_PAGE_SIZE_ALL, TABLE_PAGE_SIZE_OPTIONS } from '#lib/constants/table-pagination.js';
	import { ArrowRightIcon, ArrowLeftIcon, DoubleArrowRightIcon, DoubleArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Paginated } from '#lib/types/shared.js';

	let {
		items,
		currentPage,
		totalPages,
		totalItems,
		pageSize,
		canPrev,
		canNext,
		setPage,
		setPageSize
	}: {
		items: Paginated<unknown>;
		currentPage: number;
		totalPages: number;
		totalItems: number;
		pageSize: number;
		canPrev: boolean;
		canNext: boolean;
		setPage: (page: number) => void;
		setPageSize: (limit: number) => void;
	} = $props();

	const uid = $props.id();
	const totalsKnown = $derived(totalItems >= 0 && totalPages >= 0);
	const isAllSelected = $derived(pageSize === TABLE_PAGE_SIZE_ALL);
	const displayValue = $derived(isAllSelected ? m.common_all() : String(pageSize));
	const selectValue = $derived(isAllSelected ? 'all' : String(pageSize));

	function handlePageSizeChange(value: string) {
		if (value === 'all') {
			setPageSize(TABLE_PAGE_SIZE_ALL);
		} else {
			setPageSize(Number(value));
		}
	}
</script>

<div class="flex w-full items-center justify-between gap-2 px-2 sm:gap-4">
	<div class="order-2 hidden text-sm text-muted-foreground sm:order-1 sm:block">
		{#if totalsKnown}
			{m.common_showing_of_total({ shown: items.data.length, total: totalItems })}
		{/if}
	</div>
	<div class="order-1 flex w-full items-center justify-between gap-2 sm:order-2 sm:w-auto sm:gap-4 sm:space-x-6 lg:space-x-8">
		<div class="flex items-center justify-between space-x-2 sm:justify-start">
			<p id="{uid}-rows-per-page" class="sr-only text-sm font-medium sm:not-sr-only">{m.common_rows_per_page()}</p>
			<Select.Root allowDeselect={false} type="single" bind:value={() => selectValue, handlePageSizeChange}>
				<Select.Trigger id="{uid}-page-size" aria-labelledby="{uid}-rows-per-page {uid}-page-size" class="h-11 w-17.5 sm:h-8">
					{displayValue}
				</Select.Trigger>
				<Select.Content side="top">
					{#each TABLE_PAGE_SIZE_OPTIONS as size (size)}
						<Select.Item value={`${size}`}>
							{size}
						</Select.Item>
					{/each}
					<Select.Item value="all">
						{m.common_all()}
					</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>
		<div class="flex items-center justify-between gap-2 sm:justify-center sm:gap-0">
			{#if totalsKnown}
				<div class="flex items-center justify-center text-sm font-medium sm:w-25">
					{m.common_page_of({ page: currentPage, total: totalPages })}
				</div>
			{/if}
			<div class="flex items-center space-x-1 sm:space-x-2">
				<ArcaneButton
					action="base"
					tone="outline"
					size="icon"
					icon={DoubleArrowLeftIcon}
					class="hidden size-8 lg:flex"
					onclick={() => setPage(1)}
					disabled={!canPrev}
					aria-label={m.common_go_first_page()}
				/>
				<ArcaneButton
					action="base"
					tone="outline"
					size="icon"
					icon={ArrowLeftIcon}
					class="size-11 sm:size-8"
					onclick={() => setPage(currentPage - 1)}
					disabled={!canPrev}
					aria-label={m.common_go_prev_page()}
				/>
				<ArcaneButton
					action="base"
					tone="outline"
					size="icon"
					icon={ArrowRightIcon}
					class="size-11 sm:size-8"
					onclick={() => setPage(currentPage + 1)}
					disabled={!canNext}
					aria-label={m.common_go_next_page()}
				/>
				{#if totalsKnown}
					<ArcaneButton
						action="base"
						tone="outline"
						size="icon"
						icon={DoubleArrowRightIcon}
						class="hidden size-8 lg:flex"
						onclick={() => setPage(totalPages)}
						disabled={!canNext}
						aria-label={m.common_go_last_page()}
					/>
				{/if}
			</div>
		</div>
	</div>
</div>
