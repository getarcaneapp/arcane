<script lang="ts">
	import { z } from 'zod/v4';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { ActivityIcon } from '#lib/icons/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	let { data } = $props();

	const isReadOnly = $derived.by(() => settingsStore.current?.uiConfigDisabled);

	const formSchema = z.object({
		activityHistoryRetentionDays: z.coerce.number().int().min(0).max(3650),
		upgradeLogRetentionDays: z.coerce.number().int().min(0).max(3650),
		activityHistoryMaxEntries: z.coerce.number().int().min(0).max(100000),
		maxConcurrentActivities: z.coerce.number().int().min(0).max(1000)
	});

	const getFormDefaults = () => {
		const settings = settingsStore.current || data.settings!;
		return {
			activityHistoryRetentionDays: settings.activityHistoryRetentionDays,
			upgradeLogRetentionDays: settings.upgradeLogRetentionDays ?? 3,
			activityHistoryMaxEntries: settings.activityHistoryMaxEntries,
			maxConcurrentActivities: settings.maxConcurrentActivities
		};
	};

	const { formInputs } = createSettingsForm({
		schema: formSchema,
		currentSettings: getFormDefaults(),
		getCurrentSettings: getFormDefaults,
		successMessage: m.activity_settings_saved()
	});
</script>

<SettingsPageLayout
	title={m.activity()}
	description={m.activity_settings_description()}
	icon={ActivityIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<SettingsSection title={m.activity_history_section_title()}>
				<SettingsRow
					for="activity-history-retention-days"
					label={m.activity_history_retention_days()}
					description={m.activity_history_retention_days_description()}
					helpText={m.activity_history_retention_days_help()}
					error={formInputs.activityHistoryRetentionDays.error}
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
				>
					<Input
						id="upgrade-log-retention-days"
						type="number"
						bind:value={formInputs.upgradeLogRetentionDays.value}
						aria-invalid={!!formInputs.upgradeLogRetentionDays.error}
					/>
				</SettingsRow>
			</SettingsSection>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
