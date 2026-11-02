<script lang="ts">
	import ActivityTab from '#lib/components/settings/environment/activity-tab.svelte';
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import { ActivityIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.activity()}
	description={m.activity_settings_description()}
	icon={ActivityIcon}
	keys={['activityHistoryRetentionDays', 'upgradeLogRetentionDays', 'activityHistoryMaxEntries', 'maxConcurrentActivities']}
>
	{#snippet content({ formInputs, mixed, lock })}
		<ActivityTab {formInputs} {mixed} {lock} />
	{/snippet}
</EnvironmentSettingsPage>
