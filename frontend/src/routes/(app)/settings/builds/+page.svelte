<script lang="ts">
	import { z } from 'zod/v4';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { CodeIcon } from '#lib/icons/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';
	import { settingsService } from '#lib/services/settings-service.js';

	let { data } = $props();

	const currentSettings = $derived(settingsStore.current || data.settings!);
	const isReadOnly = $derived.by(() => settingsStore.current?.uiConfigDisabled);

	const formSchema = z.object({
		buildProvider: z.enum(['local', 'depot']).default('local'),
		buildsDirectory: z.string().default(''),
		buildTimeout: z.coerce.number().int().min(60).max(14400),
		depotProjectId: z.string().default(''),
		depotToken: z.string().optional().default('')
	});

	const getFormDefaults = () => {
		const settings = settingsStore.current || data.settings!;
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
			formInputs.depotToken.value = '';
		},
		onReset: () => {
			formInputs.depotToken.value = '';
		},
		successMessage: m.build_settings_saved()
	});

	const existingDepotProjectId = $derived((currentSettings.depotProjectId ?? '').trim());
	const existingDepotToken = $derived((currentSettings.depotToken ?? '').trim());
	const depotConfigured = $derived(Boolean(currentSettings.depotConfigured));

	const depotCredentialsPresent = $derived.by(() => {
		const projectId = (formInputs.depotProjectId.value ?? '').trim() || existingDepotProjectId;
		const token = (formInputs.depotToken.value ?? '').trim() || existingDepotToken;
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
		return formInputs.buildProvider.value;
	});
</script>

<SettingsPageLayout
	title={m.builds()}
	description={m.build_settings_page_description()}
	icon={CodeIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<SettingsSection title={m.build_workspace()}>
				<SettingsRow
					for="builds-directory"
					label={m.build_settings_directory_label()}
					description={m.build_settings_directory_description()}
					helpText={m.build_settings_directory_help()}
					error={formInputs.buildsDirectory.error}
				>
					<Input
						id="builds-directory"
						placeholder={m.build_settings_directory_placeholder()}
						bind:value={formInputs.buildsDirectory.value}
						aria-invalid={!!formInputs.buildsDirectory.error}
					/>
				</SettingsRow>
			</SettingsSection>

			<SettingsSection title={m.build_provider()}>
				<SettingsRow
					for="build-provider"
					label={m.build_settings_default_provider_label()}
					description={m.build_settings_default_provider_description()}
					error={formInputs.buildProvider.error}
					helpText={!depotCredentialsPresent && !depotConfigured ? m.build_settings_depot_enable_hint() : undefined}
				>
					<SelectWithLabel
						id="build-provider"
						name="buildProvider"
						hideLabel
						bind:value={
							() => resolvedProvider,
							(value) => {
								if (value === 'depot' && depotCredentialsPresent) formInputs.buildProvider.value = 'depot';
								else formInputs.buildProvider.value = 'local';
							}
						}
						label={m.build_settings_default_provider_label()}
						options={providerOptions}
					/>
				</SettingsRow>
				<SettingsRow
					for="build-timeout"
					label={m.build_settings_timeout_label()}
					description={m.build_settings_timeout_description()}
					helpText={m.build_settings_timeout_help()}
					error={formInputs.buildTimeout.error}
				>
					<Input
						id="build-timeout"
						type="number"
						placeholder={m.build_settings_timeout_placeholder()}
						bind:value={formInputs.buildTimeout.value}
						aria-invalid={!!formInputs.buildTimeout.error}
					/>
				</SettingsRow>
			</SettingsSection>

			<SettingsSection title={m.depot()}>
				<SettingsRow
					for="depot-project-id"
					label={m.build_settings_depot_project_id_label()}
					description={m.build_settings_depot_project_id_description()}
					error={formInputs.depotProjectId.error}
				>
					<Input
						id="depot-project-id"
						placeholder={m.build_settings_depot_project_id_placeholder()}
						bind:value={
							() => formInputs.depotProjectId.value,
							(value) => {
								formInputs.depotProjectId.value = value;
								if (!depotCredentialsPresent) formInputs.buildProvider.value = 'local';
							}
						}
						aria-invalid={!!formInputs.depotProjectId.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="depot-token"
					label={m.build_settings_depot_token_label()}
					description={m.build_settings_depot_token_description()}
					helpText={m.build_settings_depot_token_help()}
					error={formInputs.depotToken.error}
				>
					<Input
						id="depot-token"
						type="password"
						placeholder={m.build_settings_depot_token_placeholder()}
						bind:value={
							() => formInputs.depotToken.value,
							(value) => {
								formInputs.depotToken.value = value;
								if (!depotCredentialsPresent) formInputs.buildProvider.value = 'local';
							}
						}
						aria-invalid={!!formInputs.depotToken.error}
					/>
				</SettingsRow>
			</SettingsSection>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
