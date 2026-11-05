<script lang="ts">
	import AutomationsTab from '#lib/components/settings/environment/automations-tab.svelte';
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.automations()}
	description={m.settings_category_automations_description()}
	permissions={['jobs:read']}
	keys={[
		'pollingEnabled',
		'imageEventWatcherEnabled',
		'autoUpdate',
		'autoUpdateDelay',
		'autoUpdateExcludedContainers',
		'githubToken',
		'scheduledPruneEnabled',
		'vulnerabilityScanEnabled',
		'autoHealEnabled',
		'autoHealExcludedContainers',
		'autoHealMaxRestarts',
		'autoHealRestartWindow'
	]}
>
	{#snippet content({ formInputs, mixed, lock }, scope)}
		<AutomationsTab {formInputs} {mixed} {lock} environments={scope.entries.map(({ environment }) => environment)} />
	{/snippet}
</EnvironmentSettingsPage>
