<script lang="ts">
	import type { Component, ComponentProps, Snippet } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Card } from '$lib/components/ui/card';
	import { UiConfigDisabledTag } from '$lib/components/badges/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import HeaderCard from '$lib/components/header-card.svelte';
	import { SearchIcon, ArrowRightIcon, CloseIcon } from '$lib/icons';
	import type { NormalizedCategory } from './category-index-page.types';

	interface CategorySearch {
		searchQuery: string;
		showSearchResults: boolean;
		searchResults: NormalizedCategory[];
		isSearching: boolean;
		performSearch: (query: string) => void | Promise<void>;
		debouncedSearch: (query: string) => void;
		clearSearch: () => void;
	}

	interface Props {
		// Header
		headerIcon: Component;
		title: string;
		subtitle: string;
		// Search input
		searchPlaceholder: string;
		clearSearchLabel: string;
		// Search-results empty/loading states
		searchingLabel: string;
		noResultsTitle: string;
		noResultsDescription: string;
		// Matching-items section
		matchingItemsLabel: string;
		// Go-to-page button on each result card
		goToPageLabel: string;
		goToPageButtonTone?: ComponentProps<typeof ArcaneButton>['tone'];
		// Class deltas between the two pages
		rootClass: string;
		cardClass: string;
		resultCardClass: string;
		searchIconClass?: string;
		// Data
		categories: NormalizedCategory[];
		categorySearch: CategorySearch;
		navigate: (href: string) => void;
		// Structurally-different regions rendered by the parent page
		resultsHeading: Snippet;
		moreKeywords: Snippet<[number]>;
	}

	let {
		headerIcon,
		title,
		subtitle,
		searchPlaceholder,
		clearSearchLabel,
		searchingLabel,
		noResultsTitle,
		noResultsDescription,
		matchingItemsLabel,
		goToPageLabel,
		goToPageButtonTone,
		rootClass,
		cardClass,
		resultCardClass,
		searchIconClass = 'size-4',
		categories,
		categorySearch,
		navigate,
		resultsHeading,
		moreKeywords
	}: Props = $props();

	const HeaderIcon = $derived(headerIcon);
</script>

<div class={rootClass}>
	<HeaderCard>
		<div class="flex items-center justify-between gap-4">
			<div class="flex min-w-64 flex-1 items-center gap-3 sm:gap-4">
				<div
					class="bg-primary/10 text-primary ring-primary/20 flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
				>
					<HeaderIcon class="size-4 sm:size-5" />
				</div>
				<div class="min-w-0">
					<h1 class="text-3xl font-semibold tracking-tight">{title}</h1>
					<p class="text-muted-foreground mt-1 text-sm sm:text-base">{subtitle}</p>
				</div>
			</div>
			<div class="flex items-center gap-3">
				<UiConfigDisabledTag />
			</div>
		</div>

		<div class="relative mt-4 w-full sm:mt-6 sm:max-w-md">
			<InputGroup.Root>
				<InputGroup.Input
					placeholder={searchPlaceholder}
					value={categorySearch.searchQuery}
					oninput={(e) => {
						categorySearch.searchQuery = e.currentTarget.value;
						categorySearch.debouncedSearch(e.currentTarget.value);
					}}
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							categorySearch.performSearch((e.currentTarget as HTMLInputElement).value);
						}
					}}
				/>
				<InputGroup.Addon>
					{#if categorySearch.showSearchResults}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							onclick={categorySearch.clearSearch}
							class="h-6 w-6 p-0"
							icon={CloseIcon}
							showLabel={false}
							customLabel={clearSearchLabel}
						/>
					{:else}
						<SearchIcon class={searchIconClass} />
					{/if}
				</InputGroup.Addon>
			</InputGroup.Root>
		</div>
	</HeaderCard>

	{#if !categorySearch.showSearchResults}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-6 xl:grid-cols-3">
			{#each categories as category (category.id)}
				{@const Icon = category.icon}
				<Card class={cardClass}>
					<button onclick={() => navigate(category.href)} class="w-full p-4 text-left sm:p-6">
						<div class="flex items-start justify-between gap-3">
							<div class="flex min-w-0 flex-1 items-start gap-3 sm:gap-4">
								<div
									class="bg-primary/5 text-primary ring-primary/10 group-hover:bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg ring-1 transition-colors sm:size-12"
								>
									<Icon class="size-5 sm:size-6" />
								</div>
								<div class="min-w-0 flex-1">
									<h2 class="text-sm leading-tight font-semibold sm:text-base">{category.title}</h2>
									<p class="text-muted-foreground mt-1 text-xs leading-relaxed sm:text-sm">{category.description}</p>
								</div>
							</div>
							<ArrowRightIcon class="text-muted-foreground group-hover:text-foreground mt-1 size-4 shrink-0 transition-colors" />
						</div>
					</button>
				</Card>
			{/each}
		</div>
	{:else}
		<div class="space-y-6 sm:space-y-8">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<h2 class="text-base font-semibold sm:text-lg">
					{@render resultsHeading()}
				</h2>
			</div>

			{#if categorySearch.isSearching}
				<div class="py-8 text-center sm:py-12">
					<Spinner class="text-primary mx-auto mb-3 size-8 sm:mb-4 sm:size-12" />
					<p class="text-muted-foreground text-sm sm:text-base">{searchingLabel}</p>
				</div>
			{:else if categorySearch.searchResults.length === 0}
				<div class="py-8 text-center sm:py-12">
					<SearchIcon class="text-muted-foreground mx-auto mb-3 size-8 sm:mb-4 sm:size-12" />
					<h3 class="mb-2 text-base font-medium sm:text-lg">{noResultsTitle}</h3>
					<p class="text-muted-foreground text-sm sm:text-base">{noResultsDescription}</p>
				</div>
			{:else}
				<div class="space-y-4 sm:space-y-6">
					{#each categorySearch.searchResults as result (result.id)}
						{@const Icon = result.icon}
						<div class={resultCardClass}>
							<div class="border-b p-4 sm:p-6">
								<div class="flex items-center justify-between">
									<div class="flex items-center gap-3">
										<Icon class="text-primary size-4 shrink-0 sm:size-5" />
										<div>
											<h3 class="text-base font-semibold sm:text-lg">{result.title}</h3>
											<p class="text-muted-foreground text-xs sm:text-sm">{result.description}</p>
										</div>
									</div>
									<ArcaneButton
										action="base"
										tone={goToPageButtonTone}
										size="sm"
										onclick={() => navigate(result.href)}
										class="shrink-0"
										customLabel={goToPageLabel}
									/>
								</div>
							</div>

							<!-- Show matching items with descriptions -->
							{#if result.matchingItems && result.matchingItems.length > 0}
								<div class="space-y-3 p-4 sm:p-6">
									<h4 class="text-muted-foreground mb-3 text-sm font-medium">{matchingItemsLabel}</h4>
									{#each result.matchingItems as item (item.key)}
										<div class="bg-background/60 border-primary/20 rounded-md border-l-2 p-3">
											<div class="flex items-start justify-between gap-3">
												<div class="min-w-0 flex-1">
													<h5 class="text-sm font-medium">{item.label}</h5>
													{#if item.description}
														<p class="text-muted-foreground mt-1 text-xs">{item.description}</p>
													{/if}
													{#if item.keywords && item.keywords.length > 0}
														<div class="mt-2 flex flex-wrap gap-1">
															{#each item.keywords.slice(0, 6) as keyword (keyword)}
																<span class="bg-muted/50 text-muted-foreground rounded px-2 py-0.5 text-xs">
																	{keyword}
																</span>
															{/each}
															{#if item.keywords.length > 6}
																<span class="text-muted-foreground px-2 py-0.5 text-xs">
																	{@render moreKeywords(item.keywords.length - 6)}
																</span>
															{/if}
														</div>
													{/if}
												</div>
												<div class="bg-muted/30 text-muted-foreground shrink-0 rounded px-2 py-1 font-mono text-xs">
													{item.type}
												</div>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
