<script lang="ts">
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { BuildsTabProps } from './tab-props';

	let { formInputs = $bindable(), settings, lock }: BuildsTabProps = $props();

	const supportsBuildHistoryRetention = $derived(settings.values.buildHistoryRetentionDays !== undefined);
	const existingDepotProjectId = $derived((settings.values.depotProjectId ?? '').trim());
	const existingDepotToken = $derived((settings.values.depotToken ?? '').trim());
	const depotConfigured = $derived(Boolean(settings.runtime.depotConfigured));

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

<div class="space-y-8">
	<SettingsSection title={m.build_workspace()}>
		<SettingsRow
			for="builds-directory"
			label={m.build_settings_directory_label()}
			description={m.build_settings_directory_description()}
			helpText={m.build_settings_directory_help()}
			error={formInputs.buildsDirectory.error}
			lock={lock('buildsDirectory')}
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
			lock={lock('buildProvider')}
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
			lock={lock('buildTimeout')}
		>
			<Input
				id="build-timeout"
				type="number"
				placeholder={m.build_settings_timeout_placeholder()}
				bind:value={formInputs.buildTimeout.value}
				aria-invalid={!!formInputs.buildTimeout.error}
			/>
		</SettingsRow>
		{#if supportsBuildHistoryRetention}
			<SettingsRow
				for="build-history-retention-days"
				label={m.build_history_retention_days()}
				description={m.build_history_retention_days_description()}
				helpText={m.build_history_retention_days_help()}
				error={formInputs.buildHistoryRetentionDays.error}
				lock={lock('buildHistoryRetentionDays')}
			>
				<Input
					id="build-history-retention-days"
					type="number"
					placeholder={m.build_history_retention_days_placeholder()}
					bind:value={formInputs.buildHistoryRetentionDays.value}
					aria-invalid={!!formInputs.buildHistoryRetentionDays.error}
				/>
			</SettingsRow>
		{/if}
	</SettingsSection>

	<SettingsSection title={m.depot()}>
		<SettingsRow
			for="depot-project-id"
			label={m.build_settings_depot_project_id_label()}
			description={m.build_settings_depot_project_id_description()}
			error={formInputs.depotProjectId.error}
			lock={lock('depotProjectId')}
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
			lock={lock('depotToken')}
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
</div>
