<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { m } from '#lib/paraglide/messages';
	import { customizeSearchService } from '#lib/services/customize-search';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import type { CustomizeCategory } from '#lib/types/shared';
	import { canReachAccessSurfaceUrl } from '#lib/utils/access-policy';
	import { getCustomizeSubpageUrlsInNavOrder } from '#lib/config/navigation-config';
	import { useCategorySearch } from '#lib/hooks/use-category-search.svelte';
	import { getCategoryIcon, orderCategoriesByNav } from '#lib/utils/category-page';
	import { TemplateIcon, FileTextIcon, RegistryIcon, VariableIcon, CustomizeIcon, GitBranchIcon } from '#lib/icons';
	import CategoryIndexPage from '#lib/components/category-index-page.svelte';
	import type { NormalizedCategory } from '#lib/components/category-index-page.types';

	let { data }: PageProps = $props();
	let customizeCategories = $state<CustomizeCategory[]>([]);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const categorySearch = useCategorySearch<CustomizeCategory>({
		search: (query) => customizeSearchService.search(query),
		filter: isAccessibleCategory,
		onError: (error) => console.error('Search failed:', error)
	});

	const iconMap: Record<string, any> = {
		'file-text': FileTextIcon,
		layers: TemplateIcon,
		package: RegistryIcon,
		code: VariableIcon,
		'git-branch': GitBranchIcon
	};

	const categoryMessages = {
		templates: {
			title: m.templates_title,
			description: m.templates_subtitle
		},
		registries: {
			title: m.registries_title,
			description: m.registries_subtitle
		},
		variables: {
			title: m.variables_title,
			description: m.variables_subtitle
		},
		'git-repositories': {
			title: m.repositories,
			description: m.git_repositories_subtitle
		}
	} as const;

	function isAccessibleCategory(category: CustomizeCategory) {
		if (!permissionsManifest?.accessSurfaces?.length) return false;
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user, environmentStore.selected?.id);
	}

	onMount(async () => {
		try {
			customizeCategories = orderCategoriesByNav(
				await customizeSearchService.getCategories(),
				getCustomizeSubpageUrlsInNavOrder()
			);
		} catch (error) {
			console.error('Failed to load categories:', error);
		}
	});

	function navigateToCategory(categoryUrl: string) {
		goto(categoryUrl);
	}

	function getIconComponent(iconName: string) {
		return getCategoryIcon(iconMap, iconName, CustomizeIcon);
	}

	function normalize(category: CustomizeCategory): NormalizedCategory {
		// Category IDs are the stable API contract; backend text remains the fallback for future categories.
		const messages = categoryMessages[category.id as keyof typeof categoryMessages];
		return {
			id: category.id,
			title: messages?.title() ?? category.title,
			description: messages?.description() ?? category.description,
			icon: getIconComponent(category.icon),
			href: category.url,
			matchingItems: category.matchingCustomizations
		};
	}

	const normalizedCategories = $derived(customizeCategories.filter(isAccessibleCategory).map(normalize));
	const searchAdapter = {
		get searchQuery() {
			return categorySearch.searchQuery;
		},
		set searchQuery(value: string) {
			categorySearch.searchQuery = value;
		},
		get showSearchResults() {
			return categorySearch.showSearchResults;
		},
		get searchResults() {
			return categorySearch.searchResults.filter(isAccessibleCategory).map(normalize);
		},
		get isSearching() {
			return categorySearch.isSearching;
		},
		performSearch: categorySearch.performSearch,
		debouncedSearch: categorySearch.debouncedSearch,
		clearSearch: categorySearch.clearSearch
	};
</script>

<CategoryIndexPage
	headerIcon={CustomizeIcon}
	title={m.deployment()}
	subtitle={m.deployment_description()}
	searchPlaceholder={m.customize_search_placeholder()}
	clearSearchLabel={m.common_clear_search()}
	searchingLabel={m.searching()}
	noResultsTitle={m.customize_no_options()}
	noResultsDescription={m.customize_try_adjusting()}
	matchingItemsLabel={m.customize_available_options()}
	goToPageLabel={m.customize_button()}
	categories={normalizedCategories}
	categorySearch={searchAdapter}
	navigate={navigateToCategory}
>
	{#snippet resultsHeading()}
		{m.customize_search_results({ query: categorySearch.searchQuery })} ({searchAdapter.searchResults.length}
		{searchAdapter.searchResults.length === 1 ? m.customize_result() : m.customize_results()})
	{/snippet}
	{#snippet moreKeywords(count: number)}
		+{count}
		{m.customize_more()}
	{/snippet}
</CategoryIndexPage>
