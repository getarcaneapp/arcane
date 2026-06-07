<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Label } from '$lib/components/ui/label';
	import { ActivityIcon } from '$lib/icons';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { createSettingsForm } from '$lib/utils/settings-form';

	let { data } = $props();

	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		activityHistoryRetentionDays: z.coerce.number().int().min(0).max(3650),
		activityHistoryMaxEntries: z.coerce.number().int().min(0).max(100000)
	});

	const getFormDefaults = () => {
		const settings = $settingsStore || data.settings!;
		return {
			activityHistoryRetentionDays: settings.activityHistoryRetentionDays,
			activityHistoryMaxEntries: settings.activityHistoryMaxEntries
		};
	};

	const { formInputs, registerOnMount } = createSettingsForm({
		schema: formSchema,
		currentSettings: getFormDefaults(),
		getCurrentSettings: getFormDefaults,
		successMessage: m.activity_settings_saved()
	});

	onMount(() => registerOnMount());
</script>

<SettingsPageLayout
	title={m.activity_settings_title()}
	description={m.activity_settings_description()}
	icon={ActivityIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.activity_history_section_title()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.activity_history_retention_days()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.activity_history_retention_days_description()}</p>
							</div>
							<div class="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.activityHistoryRetentionDays.value}
									error={$formInputs.activityHistoryRetentionDays.error}
									label={m.activity_history_retention_days()}
									placeholder={m.activity_history_retention_days_placeholder()}
									helpText={m.activity_history_retention_days_help()}
									type="number"
								/>
							</div>
						</div>

						<div class="border-t pt-6">
							<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
								<div>
									<Label class="text-base">{m.activity_history_max_entries()}</Label>
									<p class="text-muted-foreground mt-1 text-sm">{m.activity_history_max_entries_description()}</p>
								</div>
								<div class="max-w-xs">
									<TextInputWithLabel
										bind:value={$formInputs.activityHistoryMaxEntries.value}
										error={$formInputs.activityHistoryMaxEntries.error}
										label={m.activity_history_max_entries()}
										placeholder={m.activity_history_max_entries_placeholder()}
										helpText={m.activity_history_max_entries_help()}
										type="number"
									/>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
