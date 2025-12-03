<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { m } from '$lib/paraglide/messages';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import UserIcon from '@lucide/svelte/icons/user';
	import PaletteIcon from '@lucide/svelte/icons/palette';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import { SettingsPageLayout } from '$lib/layouts';
	import AccentColorPicker from '$lib/components/accent-color/accent-color-picker.svelte';
	import { applyAccentColor } from '$lib/utils/accent-color-util';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { createSettingsForm } from '$lib/utils/settings-form.util';

	let { data } = $props();

	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		projectsDirectory: z.string().min(1, m.general_projects_directory_required()),
		baseServerUrl: z.string().min(1, m.general_base_url_required()),
		enableGravatar: z.boolean(),
		environmentHealthInterval: z.number().int().min(1).max(60),
		accentColor: z.string(),
		glassEffectEnabled: z.boolean()
	});

	let { formInputs, registerOnMount } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			successMessage: m.general_settings_saved(),
			onReset: () => applyAccentColor(currentSettings.accentColor)
		})
	);

	onMount(() => registerOnMount());
</script>

<SettingsPageLayout
	title={m.general_title()}
	description={m.general_description()}
	icon={SettingsIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative">
			<div class="space-y-4 sm:space-y-6">
				<Card.Root>
					<Card.Header icon={FolderIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.general_projects_heading()}</Card.Title>
							<Card.Description>{m.general_projects_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="space-y-3">
							<TextInputWithLabel
								bind:value={$formInputs.projectsDirectory.value}
								error={$formInputs.projectsDirectory.error}
								label={m.general_projects_directory_label()}
								placeholder={m.general_projects_directory_placeholder()}
								helpText={m.general_projects_directory_help()}
								type="text"
							/>

							<TextInputWithLabel
								bind:value={$formInputs.baseServerUrl.value}
								error={$formInputs.baseServerUrl.error}
								label={m.general_base_url_label()}
								placeholder={m.general_base_url_placeholder()}
								helpText={m.general_base_url_help()}
								type="text"
							/>
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header icon={UserIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.general_user_avatars_heading()}</Card.Title>
							<Card.Description>{m.general_user_avatars_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<SwitchWithLabel
							id="enableGravatar"
							label={m.general_enable_gravatar_label()}
							description={m.general_enable_gravatar_description()}
							error={$formInputs.enableGravatar.error}
							bind:checked={$formInputs.enableGravatar.value}
						/>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header icon={SettingsIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.environments_health_check_title()}</Card.Title>
							<Card.Description>{m.environments_health_check_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<TextInputWithLabel
							bind:value={$formInputs.environmentHealthInterval.value}
							error={$formInputs.environmentHealthInterval.error}
							label={m.environments_health_check_interval_label()}
							placeholder="2"
							helpText={m.environments_health_check_interval_description()}
							type="number"
						/>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header icon={PaletteIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.accent_color()}</Card.Title>
							<Card.Description>{m.accent_color_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="space-y-5">
							<AccentColorPicker
								previousColor={currentSettings.accentColor}
								bind:selectedColor={$formInputs.accentColor.value}
								disabled={isReadOnly}
							/>
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header icon={SparklesIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.glass_effect_title()}</Card.Title>
							<Card.Description>{m.glass_effect_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="flex items-start gap-3 rounded-lg border p-3 sm:p-4">
							<div
								class="bg-primary/10 text-primary ring-primary/20 flex size-7 flex-shrink-0 items-center justify-center rounded-lg ring-1 sm:size-8"
							>
								<SparklesIcon class="size-3 sm:size-4" />
							</div>
							<div class="flex flex-1 flex-col gap-3">
								<div>
									<h4 class="mb-1 text-sm leading-tight font-medium">{m.glass_effect_label()}</h4>
									<p class="text-muted-foreground text-xs leading-relaxed">
										{m.glass_effect_description_long()}
									</p>
								</div>
								<div class="flex items-center gap-2">
									<Switch
										id="glassEffectEnabled"
										bind:checked={$formInputs.glassEffectEnabled.value}
										disabled={isReadOnly}
										onCheckedChange={(checked) => {
											$formInputs.glassEffectEnabled.value = checked;
										}}
									/>
									<label for="glassEffectEnabled" class="text-xs font-medium">
										{$formInputs.glassEffectEnabled.value ? m.glass_effect_enabled() : m.glass_effect_disabled()}
									</label>
								</div>
							</div>
						</div>
					</Card.Content>
				</Card.Root>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
