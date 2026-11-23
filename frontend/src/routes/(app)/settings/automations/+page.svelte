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
		'autoUpdateMode',
		'autoUpdateExcludedContainers',
		'githubToken',
		'scheduledPruneEnabled',
		'vulnerabilityScanEnabled',
		'autoHealEnabled',
		'autoHealMode',
		'autoHealExcludedContainers',
		'autoHealMaxRestarts',
		'autoHealRestartWindow'
	]}
>
	{#snippet content({ formInputs, mixed, lock }, scope)}
		{@const values = scope.entries.map(({ settings }) => settings.values)}
		<AutomationsTab
			{formInputs}
			{mixed}
			{lock}
			environments={scope.entries.map(({ environment }) => environment)}
			containerModes={{
				every: values.every((value) => 'autoUpdateMode' in value),
				some: values.some((value) => 'autoUpdateMode' in value),
				autoUpdateInclude: values.some((value) => value.autoUpdateMode === 'include'),
				autoHealInclude: values.some((value) => value.autoHealMode === 'include')
			}}
		/>
	{/snippet}
</EnvironmentSettingsPage>
