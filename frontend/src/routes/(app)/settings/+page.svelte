<script lang="ts">
	import { resolve } from '$app/paths';

	import SettingsIndexPage from '#lib/components/settings/settings-index-page.svelte';
	import { getSettingsSubpageUrlsInNavOrder } from '#lib/config/navigation-config.js';
	import { SettingsIcon, UserIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const accountPreferencesUrl = '/account?tab=preferences';
</script>

<SettingsIndexPage
	rootUrl={resolve('/(app)/settings')}
	headerIcon={SettingsIcon}
	title={m.settings()}
	subtitle={m.settings_subtitle()}
	searchPlaceholder={m.settings_search_placeholder()}
	groups={[
		{ id: 'environment', title: m.resource_environment_cap(), urls: getSettingsSubpageUrlsInNavOrder() },
		{ id: 'personal', title: m.personal(), urls: [accountPreferencesUrl] }
	]}
	extraCategories={[
		{
			id: 'account-preferences',
			title: m.account_preferences(),
			description: m.account_preferences_desc(),
			icon: UserIcon,
			href: accountPreferencesUrl
		}
	]}
	user={data.user}
	permissionsManifest={data.permissionsManifest}
/>
