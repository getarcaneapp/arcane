<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { SettingsPageLayout } from '$lib/layouts';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import { ClockIcon } from '$lib/icons';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { createSettingsForm } from '$lib/utils/settings-form.util';

	let { data } = $props();

	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		dockerApiTimeout: z.coerce.number().int().min(1).max(3600),
		dockerImagePullTimeout: z.coerce.number().int().min(30).max(7200),
		trivyScanTimeout: z.coerce.number().int().min(60).max(14400),
		gitOperationTimeout: z.coerce.number().int().min(30).max(3600),
		httpClientTimeout: z.coerce.number().int().min(5).max(300),
		registryTimeout: z.coerce.number().int().min(5).max(300),
		proxyRequestTimeout: z.coerce.number().int().min(10).max(600)
	});

	const getFormDefaults = () => {
		const settings = $settingsStore || data.settings!;
		return {
			dockerApiTimeout: settings.dockerApiTimeout,
			dockerImagePullTimeout: settings.dockerImagePullTimeout,
			trivyScanTimeout: settings.trivyScanTimeout,
			gitOperationTimeout: settings.gitOperationTimeout,
			httpClientTimeout: settings.httpClientTimeout,
			registryTimeout: settings.registryTimeout,
			proxyRequestTimeout: settings.proxyRequestTimeout
		};
	};

	const { formInputs, registerOnMount } = createSettingsForm({
		schema: formSchema,
		currentSettings: getFormDefaults(),
		getCurrentSettings: getFormDefaults,
		successMessage: m.timeouts_save()
	});

	onMount(() => registerOnMount());
</script>

<SettingsPageLayout
	title={m.timeouts_settings()}
	description={m.timeouts_settings_description()}
	icon={ClockIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<!-- Docker Operations -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.timeouts_docker_operations()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<SettingsRow label={m.docker_api_timeout()} description={m.docker_api_timeout_description()} contentClass="max-w-xs">
							<TextInputWithLabel
								bind:value={$formInputs.dockerApiTimeout.value}
								error={$formInputs.dockerApiTimeout.error}
								label={m.docker_api_timeout()}
								placeholder="30"
								helpText="Timeout in seconds (1-3600)"
								type="number"
							/>
						</SettingsRow>

						<div class="border-t pt-6">
							<SettingsRow
								label={m.docker_image_pull_timeout()}
								description={m.docker_image_pull_timeout_description()}
								contentClass="max-w-xs"
							>
								<TextInputWithLabel
									bind:value={$formInputs.dockerImagePullTimeout.value}
									error={$formInputs.dockerImagePullTimeout.error}
									label={m.docker_image_pull_timeout()}
									placeholder="600"
									helpText="Timeout in seconds (30-7200)"
									type="number"
								/>
							</SettingsRow>
						</div>

						<div class="border-t pt-6">
							<SettingsRow
								label={m.trivy_scan_timeout()}
								description={m.trivy_scan_timeout_description()}
								contentClass="max-w-xs"
							>
								<TextInputWithLabel
									bind:value={$formInputs.trivyScanTimeout.value}
									error={$formInputs.trivyScanTimeout.error}
									label={m.trivy_scan_timeout()}
									placeholder="900"
									helpText="Timeout in seconds (60-14400)"
									type="number"
								/>
							</SettingsRow>
						</div>
					</div>
				</div>
			</div>

			<!-- Git Operations -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.timeouts_git_operations()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<SettingsRow
							label={m.git_operation_timeout()}
							description={m.git_operation_timeout_description()}
							contentClass="max-w-xs"
						>
							<TextInputWithLabel
								bind:value={$formInputs.gitOperationTimeout.value}
								error={$formInputs.gitOperationTimeout.error}
								label={m.git_operation_timeout()}
								placeholder="300"
								helpText="Timeout in seconds (30-3600)"
								type="number"
							/>
						</SettingsRow>
					</div>
				</div>
			</div>

			<!-- Network Operations -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.timeouts_network_operations()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<SettingsRow
							label={m.http_client_timeout()}
							description={m.http_client_timeout_description()}
							contentClass="max-w-xs"
						>
							<TextInputWithLabel
								bind:value={$formInputs.httpClientTimeout.value}
								error={$formInputs.httpClientTimeout.error}
								label={m.http_client_timeout()}
								placeholder="30"
								helpText="Timeout in seconds (5-300)"
								type="number"
							/>
						</SettingsRow>

						<div class="border-t pt-6">
							<SettingsRow label={m.registry_timeout()} description={m.registry_timeout_description()} contentClass="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.registryTimeout.value}
									error={$formInputs.registryTimeout.error}
									label={m.registry_timeout()}
									placeholder="30"
									helpText="Timeout in seconds (5-300)"
									type="number"
								/>
							</SettingsRow>
						</div>

						<div class="border-t pt-6">
							<SettingsRow
								label={m.proxy_request_timeout()}
								description={m.proxy_request_timeout_description()}
								contentClass="max-w-xs"
							>
								<TextInputWithLabel
									bind:value={$formInputs.proxyRequestTimeout.value}
									error={$formInputs.proxyRequestTimeout.error}
									label={m.proxy_request_timeout()}
									placeholder="60"
									helpText="Timeout in seconds (10-600)"
									type="number"
								/>
							</SettingsRow>
						</div>
					</div>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
