<script lang="ts">
	import DockerTab from '#lib/components/settings/environment/docker-tab.svelte';
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import { DockerBrandIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.docker_title()}
	description={m.settings_category_docker_description()}
	icon={DockerBrandIcon}
	keys={[
		'defaultShell',
		'defaultDeployPullPolicy',
		'toolsImageRegistry',
		'updateCheckRegistry',
		'baseServerUrl',
		'autoInjectEnv',
		'pruneContainerMode',
		'pruneContainerUntil',
		'pruneImageMode',
		'pruneImageUntil',
		'pruneVolumeMode',
		'pruneNetworkMode',
		'pruneNetworkUntil',
		'pruneBuildCacheMode',
		'pruneBuildCacheUntil'
	]}
>
	{#snippet content({ formInputs, mixed, environmentId })}
		<DockerTab {formInputs} {mixed} {environmentId} />
	{/snippet}
</EnvironmentSettingsPage>
