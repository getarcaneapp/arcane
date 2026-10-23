<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { LockableFormTabProps } from './tab-props';

	let { formInputs = $bindable(), lock }: LockableFormTabProps = $props();
</script>

<div class="space-y-8">
	<SettingsSection title={m.timeouts_docker_operations()}>
		<SettingsRow
			for="docker-api-timeout"
			label={m.docker_api_timeout()}
			description={m.docker_api_timeout_description()}
			helpText={m.timeouts_seconds_help({ range: '1-3600' })}
			error={formInputs.dockerApiTimeout.error}
			lock={lock('dockerApiTimeout')}
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
			lock={lock('dockerImagePullTimeout')}
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
			lock={lock('deployWaitTimeout')}
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
			lock={lock('trivyScanTimeout')}
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
			lock={lock('gitOperationTimeout')}
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
			lock={lock('httpClientTimeout')}
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
			lock={lock('registryTimeout')}
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
			lock={lock('registryTagTimeout')}
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
			lock={lock('proxyRequestTimeout')}
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
</div>
