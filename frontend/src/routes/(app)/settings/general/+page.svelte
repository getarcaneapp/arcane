<script lang="ts">
	import { z } from 'zod/v4';
	import { m } from '$lib/paraglide/messages';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsIcon } from '$lib/icons';
	import { SettingsPageLayout } from '$lib/layouts';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import { Separator } from '$lib/components/ui/separator';
	import { Label } from '$lib/components/ui/label';

	let { data } = $props();

	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		environmentHealthInterval: z.coerce.number().int().min(1).max(60)
	});

	let { formInputs } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			getCurrentSettings: () => $settingsStore || data.settings!,
			successMessage: m.general_settings_saved()
		})
	);
</script>

<SettingsPageLayout
	title={m.general_title()}
	description={m.general_description()}
	icon={SettingsIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<!-- System Configuration Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">System Configuration</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<!-- Health Check -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.environments_health_check_title()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.environments_health_check_description()}</p>
							</div>
							<div class="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.environmentHealthInterval.value}
									error={$formInputs.environmentHealthInterval.error}
									label={m.environments_health_check_interval_label()}
									placeholder="2"
									helpText={m.environments_health_check_interval_description()}
									type="number"
								/>
							</div>
						</div>
					</div>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
