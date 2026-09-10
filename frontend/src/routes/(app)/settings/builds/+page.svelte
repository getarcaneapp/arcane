<script lang="ts">
	import { z } from 'zod/v4';
	import settingsStore from '#lib/stores/config-store.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { CodeIcon } from '#lib/icons/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';
	import { settingsService } from '#lib/services/settings-service.js';

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
		let buildProvider = settings.buildProvider;
		if (!settings.depotConfigured && !((settings.depotProjectId ?? '').trim() && (settings.depotToken ?? '').trim())) {
			buildProvider = 'local';
		}
		return {
			buildProvider,
			buildsDirectory: settings.buildsDirectory,
			buildTimeout: settings.buildTimeout,
			depotProjectId: settings.depotProjectId,
			depotToken: ''
		};
	};

	const { formInputs } = createSettingsForm({
		schema: formSchema,
		currentSettings: getFormDefaults(),
		getCurrentSettings: getFormDefaults,
		onSave: async (payload) => {
			const updated = { ...payload } as Record<string, unknown>;
			if (!depotCredentialsPresent) updated['buildProvider'] = 'local';
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

	const resolvedProvider = $derived.by(() => {
		if (!depotCredentialsPresent) return 'local';
		return $formInputs.buildProvider.value;
	});
</script>

<SettingsPageLayout
	title={m.build()}
	description={m.build_settings_page_description()}
	icon={CodeIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.build_workspace()}</h3>
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
				<h3 class="text-base font-semibold">{m.build_provider()}</h3>
				<div class="grid gap-5 sm:grid-cols-2">
					<div>
						<SelectWithLabel
							id="build-provider"
							name="buildProvider"
							bind:value={
								() => resolvedProvider,
								(value) => {
									if (value === 'depot' && depotCredentialsPresent) $formInputs.buildProvider.value = 'depot';
									else $formInputs.buildProvider.value = 'local';
								}
							}
							error={$formInputs.buildProvider.error}
							label={m.build_settings_default_provider_label()}
							description={m.build_settings_default_provider_description()}
							options={providerOptions}
						/>
						{#if !depotCredentialsPresent && !depotConfigured}
							<p class="mt-2 text-xs text-muted-foreground">{m.build_settings_depot_enable_hint()}</p>
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
				<h3 class="text-base font-semibold">{m.depot()}</h3>
				<div class="grid gap-5 sm:grid-cols-2">
					<TextInputWithLabel
						bind:value={
							() => $formInputs.depotProjectId.value,
							(value) => {
								$formInputs.depotProjectId.value = value;
								if (!depotCredentialsPresent) $formInputs.buildProvider.value = 'local';
							}
						}
						error={$formInputs.depotProjectId.error}
						label={m.build_settings_depot_project_id_label()}
						description={m.build_settings_depot_project_id_description()}
						placeholder={m.build_settings_depot_project_id_placeholder()}
					/>
					<TextInputWithLabel
						bind:value={
							() => $formInputs.depotToken.value,
							(value) => {
								$formInputs.depotToken.value = value;
								if (!depotCredentialsPresent) $formInputs.buildProvider.value = 'local';
							}
						}
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
