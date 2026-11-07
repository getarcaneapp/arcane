<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { ArrowRightIcon, ArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let {
		shown,
		currentPage,
		totalPages,
		totalItems,
		pageSize,
		canPrev,
		canNext,
		setPage
	}: {
		shown: number;
		currentPage: number;
		totalPages: number;
		totalItems: number;
		pageSize: number;
		canPrev: boolean;
		canNext: boolean;
		setPage: (page: number) => void;
	} = $props();

	const start = $derived(pageSize > 0 ? (currentPage - 1) * pageSize + 1 : 1);
	const end = $derived(start + shown - 1);
	const singlePage = $derived(totalPages >= 0 && totalPages <= 1 && currentPage <= 1);
</script>

<nav aria-label={m.table_pagination()} class="flex items-center gap-1 text-xs whitespace-nowrap text-muted-foreground sm:text-sm">
	<span class="px-1 tabular-nums">
		{#if totalItems >= 0}
			{m.table_range_of_total({ start, end, total: totalItems })}
		{:else}
			{m.table_range({ start, end })}
		{/if}
	</span>
	{#if !singlePage}
		<ArcaneButton
			action="base"
			tone="ghost"
			size="icon"
			icon={ArrowLeftIcon}
			class="size-11 sm:size-9"
			onclick={() => setPage(currentPage - 1)}
			disabled={!canPrev}
			aria-label={m.common_go_prev_page()}
		/>
		<ArcaneButton
			action="base"
			tone="ghost"
			size="icon"
			icon={ArrowRightIcon}
			class="size-11 sm:size-9"
			onclick={() => setPage(currentPage + 1)}
			disabled={!canNext}
			aria-label={m.common_go_next_page()}
		/>
	{/if}
</nav>
