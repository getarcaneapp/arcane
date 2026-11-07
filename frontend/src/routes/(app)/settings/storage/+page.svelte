<script lang="ts">
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import StorageTab from '#lib/components/settings/environment/storage-tab.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.storage_limits()}
	description={m.settings_category_storage_description()}
	keys={[
		'projectsDirectory',
		'templatesDirectory',
		'swarmStackSourcesDirectory',
		'diskUsagePath',
		'followProjectSymlinks',
		'maxImageUploadSize',
		'gitSyncMaxFiles',
		'gitSyncMaxTotalSizeMb',
		'gitSyncMaxBinarySizeMb',
		'unmanagedBackupRetentionDays'
	]}
>
	{#snippet content({ formInputs, mixed, settings })}
		<StorageTab {formInputs} {mixed} {settings} />
	{/snippet}
</EnvironmentSettingsPage>
