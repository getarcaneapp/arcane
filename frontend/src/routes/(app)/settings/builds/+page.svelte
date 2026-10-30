<script lang="ts">
	import BuildsTab from '#lib/components/settings/environment/builds-tab.svelte';
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import { CodeIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.builds()}
	description={m.build_settings_page_description()}
	icon={CodeIcon}
	keys={['buildProvider', 'buildsDirectory', 'buildTimeout', 'buildHistoryRetentionDays', 'depotProjectId', 'depotToken']}
>
	{#snippet content({ formInputs, mixed, lock, settings })}
		<BuildsTab {formInputs} {mixed} {lock} {settings} />
	{/snippet}
</EnvironmentSettingsPage>
