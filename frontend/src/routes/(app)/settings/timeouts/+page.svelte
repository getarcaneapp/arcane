<script lang="ts">
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import TimeoutsTab from '#lib/components/settings/environment/timeouts-tab.svelte';
	import { ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.timeouts_settings()}
	description={m.timeouts_settings_description()}
	icon={ClockIcon}
	keys={[
		'dockerApiTimeout',
		'dockerImagePullTimeout',
		'deployWaitTimeout',
		'trivyScanTimeout',
		'gitOperationTimeout',
		'httpClientTimeout',
		'registryTimeout',
		'registryTagTimeout',
		'proxyRequestTimeout'
	]}
>
	{#snippet content({ formInputs, mixed, lock })}
		<TimeoutsTab {formInputs} {mixed} {lock} />
	{/snippet}
</EnvironmentSettingsPage>
