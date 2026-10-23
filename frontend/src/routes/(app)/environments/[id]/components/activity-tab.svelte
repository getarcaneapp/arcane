<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { LockableFormTabProps } from './tab-props';

	let { formInputs = $bindable(), lock }: LockableFormTabProps = $props();
</script>

<div class="space-y-8">
	<SettingsSection title={m.activity_history_section_title()}>
		<SettingsRow
			for="activity-history-retention-days"
			label={m.activity_history_retention_days()}
			description={m.activity_history_retention_days_description()}
			helpText={m.activity_history_retention_days_help()}
			error={formInputs.activityHistoryRetentionDays.error}
			lock={lock('activityHistoryRetentionDays')}
		>
			<Input
				id="activity-history-retention-days"
				type="number"
				placeholder={m.activity_history_retention_days_placeholder()}
				bind:value={formInputs.activityHistoryRetentionDays.value}
				aria-invalid={!!formInputs.activityHistoryRetentionDays.error}
			/>
		</SettingsRow>
		<SettingsRow
			for="activity-history-max-entries"
			label={m.activity_history_max_entries()}
			description={m.activity_history_max_entries_description()}
			helpText={m.activity_history_max_entries_help()}
			error={formInputs.activityHistoryMaxEntries.error}
			lock={lock('activityHistoryMaxEntries')}
		>
			<Input
				id="activity-history-max-entries"
				type="number"
				placeholder={m.activity_history_max_entries_placeholder()}
				bind:value={formInputs.activityHistoryMaxEntries.value}
				aria-invalid={!!formInputs.activityHistoryMaxEntries.error}
			/>
		</SettingsRow>
		<SettingsRow
			for="activity-max-concurrent"
			label={m.activity_max_concurrent()}
			description={m.activity_max_concurrent_description()}
			helpText={m.activity_max_concurrent_help()}
			error={formInputs.maxConcurrentActivities.error}
			lock={lock('maxConcurrentActivities')}
		>
			<Input
				id="activity-max-concurrent"
				type="number"
				placeholder={m.activity_max_concurrent_placeholder()}
				bind:value={formInputs.maxConcurrentActivities.value}
				aria-invalid={!!formInputs.maxConcurrentActivities.error}
			/>
		</SettingsRow>
	</SettingsSection>
	<SettingsSection title={m.upgrade_logs_section_title()}>
		<SettingsRow
			for="upgrade-log-retention-days"
			label={m.upgrade_log_retention_days()}
			description={m.upgrade_log_retention_days_description()}
			helpText={m.upgrade_log_retention_days_help()}
			error={formInputs.upgradeLogRetentionDays.error}
			lock={lock('upgradeLogRetentionDays')}
		>
			<Input
				id="upgrade-log-retention-days"
				type="number"
				bind:value={formInputs.upgradeLogRetentionDays.value}
				aria-invalid={!!formInputs.upgradeLogRetentionDays.error}
			/>
		</SettingsRow>
	</SettingsSection>
</div>
