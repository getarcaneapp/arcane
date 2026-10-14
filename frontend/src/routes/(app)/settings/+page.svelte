<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	import CategoryIndexPage from '#lib/components/category-index-page.svelte';
	import type { NormalizedCategory } from '#lib/components/category-index-page.types.js';
	import { filterByPermissions, getSettingsSubpageUrlsInNavOrder, navigationItems } from '#lib/config/navigation-config.js';
	import { useCategorySearch } from '#lib/hooks/use-category-search.svelte.js';
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
		ActivityIcon,
		RemoteEnvironmentIcon,
		BackupIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { settingsSearchService } from '#lib/services/settings-search.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { SettingsCategory } from '#lib/types/shared.js';
	import { canReachAccessSurfaceUrl } from '#lib/utils/access-policy.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { normalizeCategory, orderCategoriesByNav } from '#lib/utils/category-page.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let settingsCategories = $state<SettingsCategory[]>([]);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const categorySearch = useCategorySearch<SettingsCategory>({
		search: (query) => settingsSearchService.search(query),
		filter: isAccessibleCategory
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
		activity: ActivityIcon,
		remoteenvironment: RemoteEnvironmentIcon,
		backup: BackupIcon
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
		},
		s3destinations: {
			title: m.s3_destinations_title,
			description: m.s3_destinations_description
		},
		systembackups: {
			title: m.system_backups_title,
			description: m.system_backups_description
		}
	} as const;

	onMount(async () => {
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsSearchService.getCategories()),
			message: m.common_load_failed({ resource: m.settings() }),
			onSuccess: (categories) => {
				settingsCategories = orderCategoriesByNav(categories.filter(isAccessibleCategory), getSettingsSubpageUrlsInNavOrder());
			}
		});
	});

	function navigateToCategory(categoryUrl: string) {
		goto(categoryUrl);
	}

	function isAccessibleCategory(category: SettingsCategory) {
		if (category.id === 'systembackups' && !user?.isGlobalAdmin) return false;
		if (!permissionsManifest?.accessSurfaces?.length) return true;
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user, environmentStore.selected?.id);
	}

	function normalize(category: SettingsCategory): NormalizedCategory {
		return {
			...normalizeCategory(category, categoryMessages[category.id as keyof typeof categoryMessages], iconMap, SettingsIcon),
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

	// Roles and Diagnostics have no backend settings fields, so their cards reuse the gated sidebar entries.
	const navOnlyCardDescriptions: Record<string, () => string> = {
		'/settings/roles': m.roles_subtitle,
		'/settings/diagnostics': m.diagnostics_description
	};
	const navOnlyCategories = $derived(
		filterByPermissions(
			navigationItems.settingsItems.find((item) => item.url === '/settings')?.items,
			user ?? null,
			environmentStore.selected?.id,
			permissionsManifest
		).flatMap((item): NormalizedCategory[] => {
			const description = navOnlyCardDescriptions[item.url];
			if (!description) return [];
			return [{ id: item.url, title: item.title, description: description(), icon: item.icon, href: item.url }];
		})
	);

	const normalizedCategories = $derived([...settingsCategories.map(normalize), ...navOnlyCategories, accountPreferencesCategory]);
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
		get searchError() {
			return categorySearch.searchError;
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
	searchFailedLabel={m.common_search_failed()}
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
