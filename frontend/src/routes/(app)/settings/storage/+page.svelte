<script lang="ts">
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import StorageTab from '#lib/components/settings/environment/storage-tab.svelte';
	import { VolumesIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.storage_limits()}
	description={m.settings_category_storage_description()}
	icon={VolumesIcon}
	keys={[
		'projectsDirectory',
		'templatesDirectory',
		'swarmStackSourcesDirectory',
		'diskUsagePath',
		'followProjectSymlinks',
		'maxImageUploadSize',
		'gitSyncMaxFiles',
		'gitSyncMaxTotalSizeMb',
		'gitSyncMaxBinarySizeMb'
	]}
>
	{#snippet content({ formInputs, mixed })}
		<StorageTab {formInputs} {mixed} />
	{/snippet}
</EnvironmentSettingsPage>
