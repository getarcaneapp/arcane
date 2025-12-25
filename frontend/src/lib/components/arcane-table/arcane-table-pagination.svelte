<script lang="ts">
	import { type Table as TableType } from '@tanstack/table-core';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { m } from '$lib/paraglide/messages';
	import { ArrowRightIcon, ArrowLeftIcon, DoubleArrowRightIcon, DoubleArrowLeftIcon } from '$lib/icons';
	import type { Paginated } from '$lib/types/pagination.type';

	let {
		table,
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
		table: TableType<any>;
		items: Paginated<any>;
		currentPage: number;
		totalPages: number;
		totalItems: number;
		pageSize: number;
		canPrev: boolean;
		canNext: boolean;
		setPage: (page: number) => void;
		setPageSize: (limit: number) => void;
	} = $props();
</script>

<div class="flex w-full flex-col gap-4 px-2 sm:flex-row sm:items-center sm:justify-between">
	<div class="text-muted-foreground order-2 text-sm sm:order-1">
		{m.common_showing_of_total({ shown: items.data.length, total: totalItems })}
	</div>
	<div class="order-1 flex flex-col gap-4 sm:order-2 sm:flex-row sm:items-center sm:space-x-6 lg:space-x-8">
		<div class="flex items-center justify-between space-x-2 sm:justify-start">
			<p class="text-sm font-medium">{m.common_rows_per_page()}</p>
			<Select.Root
				allowDeselect={false}
				type="single"
				value={`${pageSize}`}
				onValueChange={(value) => setPageSize(Number(value))}
			>
				<Select.Trigger class="h-8 w-[70px]">
					{String(pageSize)}
				</Select.Trigger>
				<Select.Content side="top">
					{#each [10, 20, 30, 40, 50] as size (size)}
						<Select.Item value={`${size}`}>
							{size}
						</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
		<div class="flex items-center justify-between sm:justify-center">
			<div class="flex items-center justify-center text-sm font-medium sm:w-[100px]">
				{m.common_page_of({ page: currentPage, total: totalPages })}
			</div>
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
					class="size-8"
					onclick={() => setPage(currentPage - 1)}
					disabled={!canPrev}
					aria-label={m.common_go_prev_page()}
				/>
				<ArcaneButton
					action="base"
					tone="outline"
					size="icon"
					icon={ArrowRightIcon}
					class="size-8"
					onclick={() => setPage(currentPage + 1)}
					disabled={!canNext}
					aria-label={m.common_go_next_page()}
				/>
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
			</div>
		</div>
	</div>
</div>
