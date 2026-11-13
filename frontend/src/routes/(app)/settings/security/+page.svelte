<script lang="ts">
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import ImagePatchSettings from '#lib/components/settings/image-patch-settings.svelte';
	import LifecycleSecuritySettings from '#lib/components/settings/lifecycle-security-settings.svelte';
	import TrivySecuritySettings from '#lib/components/settings/trivy-security-settings.svelte';
	import VulnerabilityUpdateGateSettings from '#lib/components/settings/vulnerability-update-gate-settings.svelte';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { CodeIcon, SearchIcon, ShieldCheckIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';

	let securitySubTab = $state('trivy');

	function securityTabItems(vulnerabilityManagementEnabled: boolean): TabItem[] {
		return [
			...(vulnerabilityManagementEnabled
				? [
						{
							value: 'trivy',
							label: m.security_vulnerability_scanning_heading(),
							icon: SearchIcon
						}
					]
				: []),
			{
				value: 'patching',
				label: m.security_image_patching_heading(),
				icon: ShieldCheckIcon
			},
			{
				value: 'lifecycle',
				label: m.security_lifecycle_hooks_heading(),
				icon: CodeIcon
			}
		];
	}
</script>

<EnvironmentSettingsPage
	title={m.security()}
	description={m.settings_category_security_description()}
	keys={[
		'trivyDbRegistry',
		'trivyNetwork',
		'trivySecurityOpts',
		'trivyPrivileged',
		'trivyResourceLimitsEnabled',
		'trivyCpuLimit',
		'trivyMemoryLimitMb',
		'trivyConcurrentScanContainers',
		'trivyServerEnabled',
		'trivyServerUrl',
		'trivyServerToken',
		'trivyIgnoreUnfixed',
		'vulnerabilityThreatIntelEnabled',
		'trivyConfig',
		'trivyIgnore',
		'vulnerabilityUpdateGateEnabled',
		'vulnerabilityUpdateGateMaxCritical',
		'vulnerabilityUpdateGateMaxHigh',
		'vulnerabilityUpdateGateMaxMedium',
		'vulnerabilityUpdateGateMaxLow',
		'imagePatchSuffix',
		'imagePatchTimeoutSec',
		'imagePatchAllPlatforms',
		'imageAutoPatchEnabled',
		'lifecycleEnabled',
		'lifecycleDefaultRunnerImage',
		'lifecycleMaxTimeoutSec'
	]}
>
	{#snippet content({ formInputs, mixed, environmentId }, scope)}
		{@const trivyEnvironmentId = scope.entries.find(({ environment }) =>
			featureStore.isEnabled('vulnerabilityManagement', environment.id)
		)?.environment.id}
		{@const activeTab = !trivyEnvironmentId && securitySubTab === 'trivy' ? 'patching' : securitySubTab}
		<Tabs.Root value={activeTab} class="w-full">
			<div class="mb-4">
				<TabBar
					items={securityTabItems(trivyEnvironmentId !== undefined)}
					onValueChange={(value) => {
						securitySubTab = value;
					}}
				/>
			</div>
			{#if trivyEnvironmentId}
				<Tabs.Content value="trivy" class="space-y-8">
					<TrivySecuritySettings {formInputs} {mixed} environmentId={trivyEnvironmentId} />
					<VulnerabilityUpdateGateSettings {formInputs} {mixed} />
				</Tabs.Content>
			{/if}
			<Tabs.Content value="patching">
				<ImagePatchSettings {formInputs} {mixed} environmentId={trivyEnvironmentId ?? environmentId} />
			</Tabs.Content>
			<Tabs.Content value="lifecycle">
				<LifecycleSecuritySettings {formInputs} {mixed} />
			</Tabs.Content>
		</Tabs.Root>
	{/snippet}
</EnvironmentSettingsPage>
