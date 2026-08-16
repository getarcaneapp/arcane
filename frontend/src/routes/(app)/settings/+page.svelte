<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import {
		SettingsIcon,
		UserIcon,
		SecurityIcon,
		LockIcon,
		NotificationsIcon,
		DockerBrandIcon,
		ApiKeyIcon,
		JobsIcon,
		CodeIcon,
		GlobeIcon,
		ActivityIcon
	} from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { settingsSearchService } from '#lib/services/settings-search';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import type { SettingsCategory } from '#lib/types/shared';
	import { canReachAccessSurfaceUrl } from '#lib/utils/access-policy';
	import { getSettingsSubpageUrlsInNavOrder } from '#lib/config/navigation-config';
	import { useCategorySearch } from '#lib/hooks/use-category-search.svelte';
	import { getCategoryIcon, orderCategoriesByNav } from '#lib/utils/category-page';
	import CategoryIndexPage from '#lib/components/category-index-page.svelte';
	import type { NormalizedCategory } from '#lib/components/category-index-page.types';

	let { data }: PageProps = $props();

	let settingsCategories = $state<SettingsCategory[]>([]);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const categorySearch = useCategorySearch<SettingsCategory>({
		search: (query) => settingsSearchService.search(query),
		filter: isAccessibleCategory,
		onError: (error) => console.error('Search failed:', error)
	});

	const iconMap: Record<string, any> = {
		settings: SettingsIcon,
		database: DockerBrandIcon,
		lock: LockIcon,
		shield: SecurityIcon,
		bell: NotificationsIcon,
		user: UserIcon,
		apikey: ApiKeyIcon,
		jobs: JobsIcon,
		code: CodeIcon,
		globe: GlobeIcon,
		activity: ActivityIcon
	};

	const categoryMessages = {
		timeouts: {
			title: m.timeouts_settings,
			description: m.timeouts_settings_description
		},
		build: {
			title: m.builds,
			description: m.build_settings_page_description
		},
		activity: {
			title: m.activity,
			description: m.activity_settings_description
		},
		authentication: {
			title: m.authentication,
			description: m.authentication_description
		},
		notifications: {
			title: m.notifications_title,
			description: m.settings_category_notifications_description
		},
		users: {
			title: m.users_title,
			description: m.users_subtitle
		},
		apikeys: {
			title: m.api_key_page_title,
			description: m.settings_category_api_keys_description
		},
		webhooks: {
			title: m.webhook_page_title,
			description: m.webhook_page_description
		},
		jobschedule: {
			title: m.automations,
			description: m.settings_category_automations_description
		},
		security: {
			title: m.security,
			description: m.settings_category_security_description
		}
	} as const;

	onMount(async () => {
		try {
			settingsCategories = orderCategoriesByNav(
				(await settingsSearchService.getCategories()).filter(isAccessibleCategory),
				getSettingsSubpageUrlsInNavOrder()
			);
		} catch (error) {
			console.error('Failed to load categories:', error);
		}
	});

	function navigateToCategory(categoryUrl: string) {
		goto(categoryUrl);
	}

	function isAccessibleCategory(category: SettingsCategory) {
		if (!permissionsManifest?.accessSurfaces?.length) return true;
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user, environmentStore.selected?.id);
	}

	function getIconComponent(iconName: string) {
		return getCategoryIcon(iconMap, iconName, SettingsIcon);
	}

	function normalize(category: SettingsCategory): NormalizedCategory {
		// Category IDs are the stable API contract; backend text remains the fallback for future categories.
		const messages = categoryMessages[category.id as keyof typeof categoryMessages];
		return {
			id: category.id,
			title: messages?.title() ?? category.title,
			description: messages?.description() ?? category.description,
			icon: getIconComponent(category.icon),
			href: category.url,
			matchingItems: category.matchingSettings
		};
	}

	// Personal preferences (theme, navigation, shortcuts, landing page) live on
	// /account but users look for them here; surface a card for every user.
	const accountPreferencesCategory: NormalizedCategory = {
		id: 'account-preferences',
		title: m.account_preferences(),
		description: m.account_preferences_desc(),
		icon: UserIcon,
		href: '/account?tab=preferences'
	};

	const normalizedCategories = $derived([...settingsCategories.map(normalize), accountPreferencesCategory]);
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
			return categorySearch.searchResults.map(normalize);
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
	headerIcon={SettingsIcon}
	title={m.settings()}
	subtitle={m.settings_subtitle()}
	searchPlaceholder={m.settings_search_placeholder()}
	clearSearchLabel={m.common_clear_search()}
	searchingLabel={m.searching()}
	noResultsTitle={m.settings_no_results()}
	noResultsDescription={m.settings_no_results_description()}
	matchingItemsLabel={m.settings_matching_settings()}
	goToPageLabel={m.settings_go_to_page()}
	categories={normalizedCategories}
	categorySearch={searchAdapter}
	navigate={navigateToCategory}
>
	{#snippet resultsHeading()}
		{m.settings_search_results({ query: categorySearch.searchQuery, count: categorySearch.searchResults.length })}
	{/snippet}
	{#snippet moreKeywords(count: number)}
		{m.count_more({ count })}
	{/snippet}
</CategoryIndexPage>
