<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { CodeIcon } from '$lib/icons';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { createSettingsForm } from '$lib/utils/settings';
	import { settingsService } from '$lib/services/settings-service';

	let { data } = $props();

	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		buildProvider: z.enum(['local', 'depot']).default('local'),
		buildsDirectory: z.string().default(''),
		buildTimeout: z.coerce.number().int().min(60).max(14400),
		depotProjectId: z.string().default(''),
		depotToken: z.string().optional().default('')
	});

	const getFormDefaults = () => {
		const settings = $settingsStore || data.settings!;
		return {
			buildProvider: settings.buildProvider,
			buildsDirectory: settings.buildsDirectory,
			buildTimeout: settings.buildTimeout,
			depotProjectId: settings.depotProjectId,
			depotToken: ''
		};
	};

	const { formInputs, registerOnMount } = createSettingsForm({
		schema: formSchema,
		currentSettings: getFormDefaults(),
		getCurrentSettings: getFormDefaults,
		onSave: async (payload) => {
			const updated = { ...payload } as Record<string, unknown>;
			if (!updated['depotToken']) {
				delete updated['depotToken'];
			}
			await settingsService.updateSettings(updated);
		},
		onSuccess: () => {
			$formInputs.depotToken.value = '';
		},
		onReset: () => {
			$formInputs.depotToken.value = '';
		},
		successMessage: m.build_settings_saved()
	});

	const existingDepotProjectId = $derived((currentSettings.depotProjectId ?? '').trim());
	const existingDepotToken = $derived((currentSettings.depotToken ?? '').trim());
	const depotConfigured = $derived(Boolean(currentSettings.depotConfigured));

	const depotCredentialsPresent = $derived.by(() => {
		const projectId = ($formInputs.depotProjectId.value ?? '').trim() || existingDepotProjectId;
		const token = ($formInputs.depotToken.value ?? '').trim() || existingDepotToken;
		return (Boolean(projectId) && Boolean(token)) || depotConfigured;
	});

	const providerOptions = $derived.by(() => {
		const options = [{ label: m.local_docker(), value: 'local', description: m.local_docker_description() }];
		if (depotCredentialsPresent) {
			options.push({ label: m.depot(), value: 'depot', description: m.depot_description() });
		}
		return options;
	});

	$effect(() => {
		if (!depotCredentialsPresent && $formInputs.buildProvider.value === 'depot') {
			$formInputs.buildProvider.value = 'local';
		}
	});

	onMount(() => registerOnMount());
</script>

<SettingsPageLayout
	title={m.build_settings_page_title()}
	description={m.build_settings_page_description()}
	icon={CodeIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.build_settings_workspace_section_title()}</h3>
				<div class="max-w-xl">
					<TextInputWithLabel
						bind:value={$formInputs.buildsDirectory.value}
						error={$formInputs.buildsDirectory.error}
						label={m.build_settings_directory_label()}
						description={m.build_settings_directory_description()}
						placeholder={m.build_settings_directory_placeholder()}
						helpText={m.build_settings_directory_help()}
					/>
				</div>
			</div>

			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.build_settings_provider_section_title()}</h3>
				<div class="grid gap-5 sm:grid-cols-2">
					<div>
						<SelectWithLabel
							id="build-provider"
							name="buildProvider"
							bind:value={$formInputs.buildProvider.value}
							error={$formInputs.buildProvider.error}
							label={m.build_settings_default_provider_label()}
							description={m.build_settings_default_provider_description()}
							options={providerOptions}
						/>
						{#if !depotCredentialsPresent && !depotConfigured}
							<p class="text-muted-foreground mt-2 text-xs">{m.build_settings_depot_enable_hint()}</p>
						{/if}
					</div>
					<TextInputWithLabel
						bind:value={$formInputs.buildTimeout.value}
						error={$formInputs.buildTimeout.error}
						label={m.build_settings_timeout_label()}
						description={m.build_settings_timeout_description()}
						placeholder={m.build_settings_timeout_placeholder()}
						helpText={m.build_settings_timeout_help()}
						type="number"
					/>
				</div>
			</div>

			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.build_settings_depot_section_title()}</h3>
				<div class="grid gap-5 sm:grid-cols-2">
					<TextInputWithLabel
						bind:value={$formInputs.depotProjectId.value}
						error={$formInputs.depotProjectId.error}
						label={m.build_settings_depot_project_id_label()}
						description={m.build_settings_depot_project_id_description()}
						placeholder={m.build_settings_depot_project_id_placeholder()}
					/>
					<TextInputWithLabel
						bind:value={$formInputs.depotToken.value}
						error={$formInputs.depotToken.error}
						label={m.build_settings_depot_token_label()}
						description={m.build_settings_depot_token_description()}
						placeholder={m.build_settings_depot_token_placeholder()}
						type="password"
						helpText={m.build_settings_depot_token_help()}
					/>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
