<script lang="ts">
	import type { Component, Snippet } from 'svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Card } from '#lib/components/ui/card/index.js';
	import { UiConfigDisabledTag } from '#lib/components/badges/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import HeaderCard from '#lib/components/header-card.svelte';
	import { SearchIcon, ArrowRightIcon, CloseIcon } from '#lib/icons/index.js';
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
		categories,
		categorySearch,
		navigate,
		resultsHeading,
		moreKeywords
	}: Props = $props();

	const HeaderIcon = $derived(headerIcon);
</script>

<div class="space-y-6 pb-5 md:space-y-8 md:pb-5">
	<HeaderCard>
		<div class="flex items-center justify-between gap-4">
			<div class="flex min-w-64 flex-1 items-center gap-3 sm:gap-4">
				<div
					class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20 sm:size-10"
				>
					<HeaderIcon class="size-4 sm:size-5" />
				</div>
				<div class="min-w-0">
					<h1 class="text-3xl font-semibold tracking-tight">{title}</h1>
					<p class="mt-1 text-sm text-muted-foreground sm:text-base">{subtitle}</p>
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
						<SearchIcon class="size-4" />
					{/if}
				</InputGroup.Addon>
			</InputGroup.Root>
		</div>
	</HeaderCard>

	{#if !categorySearch.showSearchResults}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-3">
			{#each categories as category (category.id)}
				{@const Icon = category.icon}
				<Card class="hover-lift h-full hover:border-primary/30">
					<button
						onclick={() => navigate(category.href)}
						class="relative flex h-full w-full cursor-pointer items-center gap-3 p-4 text-left focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none focus-visible:ring-inset sm:gap-4 sm:p-5"
					>
						<span
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-primary/8 to-transparent opacity-0 transition-opacity duration-200 group-hover:opacity-100"
						></span>
						<span
							class="relative flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20 transition-colors ring-inset group-hover:bg-primary/15 group-hover:ring-primary/30"
						>
							<Icon class="size-5" />
						</span>
						<span class="relative min-w-0 flex-1">
							<span class="block truncate text-sm leading-tight font-semibold sm:text-base">{category.title}</span>
							<span class="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground sm:text-sm">
								{category.description}
							</span>
						</span>
						<ArrowRightIcon
							class="relative size-4 shrink-0 text-muted-foreground/60 transition-all duration-200 group-hover:translate-x-0.5 group-hover:text-primary"
						/>
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
					<Spinner class="mx-auto mb-3 size-8 text-primary sm:mb-4 sm:size-12" />
					<p class="text-sm text-muted-foreground sm:text-base">{searchingLabel}</p>
				</div>
			{:else if categorySearch.searchResults.length === 0}
				<div class="py-8 text-center sm:py-12">
					<SearchIcon class="mx-auto mb-3 size-8 text-muted-foreground sm:mb-4 sm:size-12" />
					<h3 class="mb-2 text-base font-medium sm:text-lg">{noResultsTitle}</h3>
					<p class="text-sm text-muted-foreground sm:text-base">{noResultsDescription}</p>
				</div>
			{:else}
				<div class="space-y-4">
					{#each categorySearch.searchResults as result (result.id)}
						{@const Icon = result.icon}
						<Card>
							<div class="flex items-center justify-between gap-3 border-b border-border/70 p-4 sm:p-5">
								<div class="flex min-w-0 items-center gap-3 sm:gap-4">
									<span
										class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20 ring-inset"
									>
										<Icon class="size-5" />
									</span>
									<div class="min-w-0">
										<h3 class="truncate text-sm leading-tight font-semibold sm:text-base">{result.title}</h3>
										<p class="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground sm:text-sm">
											{result.description}
										</p>
									</div>
								</div>
								<ArcaneButton
									action="base"
									tone="outline"
									size="sm"
									onclick={() => navigate(result.href)}
									class="shrink-0"
									customLabel={goToPageLabel}
								/>
							</div>

							<!-- Show matching items with descriptions -->
							{#if result.matchingItems && result.matchingItems.length > 0}
								<div class="space-y-2 p-4 sm:p-5">
									<h4 class="text-[11px] font-medium tracking-[0.08em] text-muted-foreground uppercase">
										{matchingItemsLabel}
									</h4>
									{#each result.matchingItems as item (item.key)}
										<div class="rounded-lg border border-border/70 bg-muted/30 p-3 dark:bg-surface/30">
											<div class="flex items-start justify-between gap-3">
												<div class="min-w-0 flex-1">
													<h5 class="text-sm font-medium">{item.label}</h5>
													{#if item.description}
														<p class="mt-1 text-xs text-muted-foreground">{item.description}</p>
													{/if}
													{#if item.keywords && item.keywords.length > 0}
														<div class="mt-2 flex flex-wrap gap-1">
															{#each item.keywords.slice(0, 6) as keyword (keyword)}
																<span class="rounded-md bg-foreground/5 px-2 py-0.5 text-xs text-muted-foreground">
																	{keyword}
																</span>
															{/each}
															{#if item.keywords.length > 6}
																<span class="px-2 py-0.5 text-xs text-muted-foreground">
																	{@render moreKeywords(item.keywords.length - 6)}
																</span>
															{/if}
														</div>
													{/if}
												</div>
												<span
													class="shrink-0 rounded-md bg-foreground/5 px-2 py-1 font-mono text-[11px] text-muted-foreground ring-1 ring-border/70 ring-inset"
												>
													{item.type}
												</span>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</Card>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
