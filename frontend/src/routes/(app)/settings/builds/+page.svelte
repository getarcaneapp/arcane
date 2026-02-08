<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Label } from '$lib/components/ui/label';
	import { CodeIcon } from '$lib/icons';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
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
			if (!updated.depotToken) {
				delete updated.depotToken;
			}
			await settingsService.updateSettings(updated);
		},
		onSuccess: () => {
			$formInputs.depotToken.value = '';
		},
		onReset: () => {
			$formInputs.depotToken.value = '';
		},
		successMessage: 'Build settings saved'
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
		const options = [{ label: 'Local BuildKit', value: 'local', description: 'Use the local BuildKit daemon' }];
		if (depotCredentialsPresent) {
			options.push({ label: 'Depot', value: 'depot', description: 'Use Depot hosted BuildKit' });
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
	title="Build"
	description="Configure BuildKit and Depot build settings."
	icon={CodeIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<!-- Build Workspace -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Build Workspace</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">Builds Directory</Label>
								<p class="text-muted-foreground mt-1 text-sm">Root directory for manual build workspaces.</p>
							</div>
							<div class="max-w-xl">
								<TextInputWithLabel
									bind:value={$formInputs.buildsDirectory.value}
									error={$formInputs.buildsDirectory.error}
									label="Builds Directory"
									placeholder="/builds"
									helpText="Absolute path inside the Arcane container"
								/>
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Build Provider -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Build Provider</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">Default Provider</Label>
								<p class="text-muted-foreground mt-1 text-sm">Select the default BuildKit provider.</p>
							</div>
							<div class="max-w-xs">
								<SelectWithLabel
									id="build-provider"
									name="buildProvider"
									bind:value={$formInputs.buildProvider.value}
									error={$formInputs.buildProvider.error}
									label="Build Provider"
									options={providerOptions}
								/>
								{#if !depotCredentialsPresent && !depotConfigured}
									<p class="text-muted-foreground mt-2 text-xs">
										Add a Depot Project ID and Token below to enable the Depot provider option.
									</p>
								{/if}
							</div>
						</div>

						<div class="border-t pt-6">
							<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
								<div>
									<Label class="text-base">Build Timeout</Label>
									<p class="text-muted-foreground mt-1 text-sm">Timeout for image builds in seconds.</p>
								</div>
								<div class="max-w-xs">
									<TextInputWithLabel
										bind:value={$formInputs.buildTimeout.value}
										error={$formInputs.buildTimeout.error}
										label="Build Timeout"
										placeholder="1800"
										helpText="Timeout in seconds (60-14400)"
										type="number"
									/>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Depot Credentials -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Depot</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">Depot Project ID</Label>
								<p class="text-muted-foreground mt-1 text-sm">Depot project identifier for hosted builds.</p>
							</div>
							<div class="max-w-xl">
								<TextInputWithLabel
									bind:value={$formInputs.depotProjectId.value}
									error={$formInputs.depotProjectId.error}
									label="Depot Project ID"
									placeholder="proj_123456"
								/>
							</div>
						</div>

						<div class="border-t pt-6">
							<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
								<div>
									<Label class="text-base">Depot Token</Label>
									<p class="text-muted-foreground mt-1 text-sm">
										Personal access token for Depot (leave blank to keep existing).
									</p>
								</div>
								<div class="max-w-xl">
									<TextInputWithLabel
										bind:value={$formInputs.depotToken.value}
										error={$formInputs.depotToken.error}
										label="Depot Token"
										placeholder="******"
										type="password"
										helpText="Leave blank to preserve the existing token"
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
