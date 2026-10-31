<script lang="ts">
	import AutomationsTab from '#lib/components/settings/environment/automations-tab.svelte';
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import { JobsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.automations()}
	description={m.settings_category_automations_description()}
	icon={JobsIcon}
	permissions={['jobs:manage']}
	keys={[
		'pollingEnabled',
		'imageEventWatcherEnabled',
		'autoUpdate',
		'autoUpdateDelay',
		'autoUpdateExcludedContainers',
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
