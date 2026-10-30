<script lang="ts">
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import JobsTab from '#lib/components/settings/environment/jobs-tab.svelte';
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
	{#snippet content({ formInputs, mixed, environmentId })}
		<JobsTab {formInputs} {mixed} {environmentId} />
	{/snippet}
</EnvironmentSettingsPage>
