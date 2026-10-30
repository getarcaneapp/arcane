<script lang="ts">
	import { resolve } from '$app/paths';

	import SettingsIndexPage from '#lib/components/settings/settings-index-page.svelte';
	import { MonitorIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
</script>

<SettingsIndexPage
	rootUrl={resolve('/(app)/system')}
	headerIcon={MonitorIcon}
	title={m.system()}
	subtitle={m.system_subtitle()}
	searchPlaceholder={m.search_system_settings()}
	groups={[
		{
			id: 'access',
			title: m.access(),
			urls: [
				resolve('/(app)/system/users'),
				resolve('/(app)/system/roles'),
				resolve('/(app)/system/api-keys'),
				resolve('/(app)/system/authentication')
			]
		},
		{
			id: 'integrations',
			title: m.integrations(),
			urls: [resolve('/(app)/system/notifications'), resolve('/(app)/system/webhooks')]
		},
		{
			id: 'backups',
			title: m.backups(),
			urls: [resolve('/(app)/system/backups'), resolve('/(app)/system/backups/s3')]
		},
		{ id: 'diagnostics', title: m.diagnostics(), urls: [resolve('/(app)/system/diagnostics')] }
	]}
	navOnlyDescriptions={{
		[resolve('/(app)/system/roles')]: m.roles_subtitle,
		[resolve('/(app)/system/diagnostics')]: m.diagnostics_description
	}}
	user={data.user}
	permissionsManifest={data.permissionsManifest}
/>
