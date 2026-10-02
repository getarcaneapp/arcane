<script lang="ts">
	import { z } from 'zod/v4';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { ClockIcon } from '#lib/icons/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	let { data } = $props();

	const isReadOnly = $derived.by(() => settingsStore.current?.uiConfigDisabled);

	const formSchema = z.object({
		dockerApiTimeout: z.coerce.number().int().min(1).max(3600),
		dockerImagePullTimeout: z.coerce.number().int().min(30).max(7200),
		deployWaitTimeout: z.coerce.number().int().min(30).max(14400),
		trivyScanTimeout: z.coerce.number().int().min(60).max(14400),
		gitOperationTimeout: z.coerce.number().int().min(30).max(3600),
		httpClientTimeout: z.coerce.number().int().min(5).max(300),
		registryTimeout: z.coerce.number().int().min(5).max(300),
		registryTagTimeout: z.coerce.number().int().min(5).max(3600),
		proxyRequestTimeout: z.coerce.number().int().min(10).max(600)
	});

	const getFormDefaults = () => {
		const settings = settingsStore.current || data.settings!;
		return {
			dockerApiTimeout: settings.dockerApiTimeout,
			dockerImagePullTimeout: settings.dockerImagePullTimeout,
			deployWaitTimeout: settings.deployWaitTimeout,
			trivyScanTimeout: settings.trivyScanTimeout,
			gitOperationTimeout: settings.gitOperationTimeout,
			httpClientTimeout: settings.httpClientTimeout,
			registryTimeout: settings.registryTimeout,
			registryTagTimeout: settings.registryTagTimeout,
			proxyRequestTimeout: settings.proxyRequestTimeout
		};
	};

	const { formInputs } = createSettingsForm({
		schema: formSchema,
		currentSettings: getFormDefaults(),
		getCurrentSettings: getFormDefaults,
		successMessage: m.timeouts_save()
	});
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
			<SettingsSection title={m.timeouts_docker_operations()}>
				<SettingsRow
					for="docker-api-timeout"
					label={m.docker_api_timeout()}
					description={m.docker_api_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '1-3600' })}
					error={formInputs.dockerApiTimeout.error}
				>
					<Input
						id="docker-api-timeout"
						type="number"
						placeholder="30"
						bind:value={formInputs.dockerApiTimeout.value}
						aria-invalid={!!formInputs.dockerApiTimeout.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="docker-image-pull-timeout"
					label={m.docker_image_pull_timeout()}
					description={m.docker_image_pull_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '30-7200' })}
					error={formInputs.dockerImagePullTimeout.error}
				>
					<Input
						id="docker-image-pull-timeout"
						type="number"
						placeholder="600"
						bind:value={formInputs.dockerImagePullTimeout.value}
						aria-invalid={!!formInputs.dockerImagePullTimeout.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="deploy-wait-timeout"
					label={m.deploy_wait_timeout()}
					description={m.deploy_wait_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '30-14400' })}
					error={formInputs.deployWaitTimeout.error}
				>
					<Input
						id="deploy-wait-timeout"
						type="number"
						placeholder="600"
						bind:value={formInputs.deployWaitTimeout.value}
						aria-invalid={!!formInputs.deployWaitTimeout.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="trivy-scan-timeout"
					label={m.trivy_scan_timeout()}
					description={m.trivy_scan_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '60-14400' })}
					error={formInputs.trivyScanTimeout.error}
				>
					<Input
						id="trivy-scan-timeout"
						type="number"
						placeholder="900"
						bind:value={formInputs.trivyScanTimeout.value}
						aria-invalid={!!formInputs.trivyScanTimeout.error}
					/>
				</SettingsRow>
			</SettingsSection>
			<SettingsSection title={m.timeouts_git_operations()}>
				<SettingsRow
					for="git-operation-timeout"
					label={m.git_operation_timeout()}
					description={m.git_operation_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '30-3600' })}
					error={formInputs.gitOperationTimeout.error}
				>
					<Input
						id="git-operation-timeout"
						type="number"
						placeholder="300"
						bind:value={formInputs.gitOperationTimeout.value}
						aria-invalid={!!formInputs.gitOperationTimeout.error}
					/>
				</SettingsRow>
			</SettingsSection>
			<SettingsSection title={m.timeouts_network_operations()}>
				<SettingsRow
					for="http-client-timeout"
					label={m.http_client_timeout()}
					description={m.http_client_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '5-300' })}
					error={formInputs.httpClientTimeout.error}
				>
					<Input
						id="http-client-timeout"
						type="number"
						placeholder="30"
						bind:value={formInputs.httpClientTimeout.value}
						aria-invalid={!!formInputs.httpClientTimeout.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="registry-timeout"
					label={m.registry_timeout()}
					description={m.registry_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '5-300' })}
					error={formInputs.registryTimeout.error}
				>
					<Input
						id="registry-timeout"
						type="number"
						placeholder="30"
						bind:value={formInputs.registryTimeout.value}
						aria-invalid={!!formInputs.registryTimeout.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="registry-tag-timeout"
					label={m.registry_tag_timeout()}
					description={m.registry_tag_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '5-3600' })}
					error={formInputs.registryTagTimeout.error}
				>
					<Input
						id="registry-tag-timeout"
						type="number"
						placeholder="120"
						bind:value={formInputs.registryTagTimeout.value}
						aria-invalid={!!formInputs.registryTagTimeout.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="proxy-request-timeout"
					label={m.proxy_request_timeout()}
					description={m.proxy_request_timeout_description()}
					helpText={m.timeouts_seconds_help({ range: '10-600' })}
					error={formInputs.proxyRequestTimeout.error}
				>
					<Input
						id="proxy-request-timeout"
						type="number"
						placeholder="60"
						bind:value={formInputs.proxyRequestTimeout.value}
						aria-invalid={!!formInputs.proxyRequestTimeout.error}
					/>
				</SettingsRow>
			</SettingsSection>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
