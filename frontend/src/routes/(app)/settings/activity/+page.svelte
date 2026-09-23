<script lang="ts">
	import { z } from 'zod/v4';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { ActivityIcon } from '#lib/icons/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	let { data } = $props();

	const isReadOnly = $derived.by(() => settingsStore.current?.uiConfigDisabled);

	const formSchema = z.object({
		activityHistoryRetentionDays: z.coerce.number().int().min(0).max(3650),
		activityHistoryMaxEntries: z.coerce.number().int().min(0).max(100000),
		maxConcurrentActivities: z.coerce.number().int().min(0).max(1000)
	});

	const getFormDefaults = () => {
		const settings = settingsStore.current || data.settings!;
		return {
			activityHistoryRetentionDays: settings.activityHistoryRetentionDays,
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
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.activity_history_section_title()}</h3>
				<div class="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
					<TextInputWithLabel
						bind:value={formInputs.activityHistoryRetentionDays.value}
						error={formInputs.activityHistoryRetentionDays.error}
						label={m.activity_history_retention_days()}
						description={m.activity_history_retention_days_description()}
						placeholder={m.activity_history_retention_days_placeholder()}
						helpText={m.activity_history_retention_days_help()}
						type="number"
					/>
					<TextInputWithLabel
						bind:value={formInputs.activityHistoryMaxEntries.value}
						error={formInputs.activityHistoryMaxEntries.error}
						label={m.activity_history_max_entries()}
						description={m.activity_history_max_entries_description()}
						placeholder={m.activity_history_max_entries_placeholder()}
						helpText={m.activity_history_max_entries_help()}
						type="number"
					/>
					<TextInputWithLabel
						bind:value={formInputs.maxConcurrentActivities.value}
						error={formInputs.maxConcurrentActivities.error}
						label={m.activity_max_concurrent()}
						description={m.activity_max_concurrent_description()}
						placeholder={m.activity_max_concurrent_placeholder()}
						helpText={m.activity_max_concurrent_help()}
						type="number"
					/>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
