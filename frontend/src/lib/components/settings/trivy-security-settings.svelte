<script lang="ts">
	import FeatureDisabled from '#lib/components/features/feature-disabled.svelte';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { onMount } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import CodeEditor from '#lib/components/code-editor/editor.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { toast } from 'svelte-sonner';
	import { networkService } from '#lib/services/network-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Settings } from '#lib/types/settings.js';

	import { arcaneImageRegistryOptions, arcaneTrivyDbImages } from '#lib/utils/registry.js';

	type TrivySecurityFormValues = Pick<
		Settings,
		| 'trivyDbRegistry'
		| 'trivyNetwork'
		| 'trivySecurityOpts'
		| 'trivyPrivileged'
		| 'trivyResourceLimitsEnabled'
		| 'trivyCpuLimit'
		| 'trivyMemoryLimitMb'
		| 'trivyConcurrentScanContainers'
		| 'trivyServerEnabled'
		| 'trivyServerUrl'
		| 'trivyServerToken'
		| 'trivyIgnoreUnfixed'
		| 'vulnerabilityThreatIntelEnabled'
		| 'trivyConfig'
		| 'trivyIgnore'
	>;

	type FormField<T> = {
		value: T;
		error: string | null;
	};

	type TrivySecurityFormInputs = Record<string, FormField<unknown>> & {
		[K in keyof TrivySecurityFormValues]: FormField<TrivySecurityFormValues[K]>;
	};

	let {
		formInputs = $bindable(),
		environmentId = undefined
	}: {
		formInputs: TrivySecurityFormInputs;
		environmentId?: string;
	} = $props();

	type TrivyNetworkOption = {
		value: string;
		label: string;
		description?: string;
	};

	const baseTrivyNetworkOptions: TrivyNetworkOption[] = [
		{
			value: '',
			label: m.auto(),
			description: m.security_trivy_network_auto_description()
		},
		{ value: 'bridge', label: m.security_trivy_network_bridge() },
		{ value: 'host', label: m.security_trivy_network_host() },
		{ value: 'none', label: m.security_trivy_network_none() }
	];

	const queryClient = useQueryClient();
	const networkRequest: SearchPaginationSortRequest = {
		pagination: { page: 1, limit: 1000 },
		sort: { column: 'name', direction: 'asc' }
	};
	const targetEnvironmentId = $derived(environmentId ?? environmentStore.selected?.id);
	const vulnerabilityManagementEnabled = $derived(
		!!targetEnvironmentId && featureStore.isEnabled('vulnerabilityManagement', targetEnvironmentId)
	);
	const networksQuery = createQuery(() => {
		const requestedEnvironmentId = targetEnvironmentId;
		userStore.current;
		return {
			queryKey: queryKeys.networks.list(requestedEnvironmentId ?? '', networkRequest),
			queryFn: async () => {
				await environmentStore.ready;
				return networkService.getNetworksForEnvironment(requestedEnvironmentId!, networkRequest);
			},
			enabled:
				vulnerabilityManagementEnabled && !!requestedEnvironmentId && hasPermission('networks:read', requestedEnvironmentId)
		};
	});
	const customTrivyNetworkOptions = $derived(
		[...new Set((networksQuery.data?.data ?? []).map((network) => network.name).filter(Boolean))]
			.sort((a, b) => a.localeCompare(b))
			.map((name) => ({ value: name, label: name }))
	);

	const trivyNetworkOptions = $derived.by(() => {
		const options = [...baseTrivyNetworkOptions];

		for (const option of customTrivyNetworkOptions) {
			if (!options.some((existing) => existing.value === option.value)) {
				options.push(option);
			}
		}

		const selectedNetwork = (formInputs.trivyNetwork.value || '').trim();
		if (selectedNetwork && !options.some((option) => option.value === selectedNetwork)) {
			options.push({
				value: selectedNetwork,
				label: selectedNetwork,
				description: m.security_trivy_network_current_value_note()
			});
		}

		return options;
	});

	function handleTrivyResourceLimitsChange(checked: boolean) {
		formInputs.trivyResourceLimitsEnabled.value = checked;
		if (!checked) {
			formInputs.trivyCpuLimit.value = 0;
			formInputs.trivyMemoryLimitMb.value = 0;
		}
	}

	onMount(() => {
		const cache = queryClient.getQueryCache();
		let lastError: unknown;
		function reportNetworkError(error: unknown) {
			if (!error || error === lastError || !targetEnvironmentId || !hasPermission('networks:read', targetEnvironmentId)) return;
			lastError = error;
			toast.info(m.security_trivy_network_fetch_failed());
		}
		reportNetworkError(networksQuery.error);
		return cache.subscribe((event) => {
			if (event.type !== 'updated' && event.type !== 'observerResultsUpdated') return;
			if (
				event.query === cache.find({ queryKey: queryKeys.networks.list(targetEnvironmentId ?? '', networkRequest), exact: true })
			) {
				reportNetworkError(event.query.state.error);
			}
		});
	});
</script>

{#if vulnerabilityManagementEnabled}
	<div class="space-y-8">
		<SettingsSection title={m.security_vulnerability_scanning_heading()}>
			<SettingsRow for="trivyDbRegistry" label={m.trivy_db_registry_label()} description={m.trivy_db_registry_description()}>
				<SelectWithLabel
					id="trivyDbRegistry"
					name="trivyDbRegistry"
					hideLabel
					bind:value={formInputs.trivyDbRegistry.value}
					label={m.trivy_db_registry_label()}
					options={arcaneImageRegistryOptions()}
					onValueChange={(v) => (formInputs.trivyDbRegistry.value = v as 'ghcr.io' | 'docker.io')}
				/>
				{#snippet helpText()}
					<ul class="space-y-0.5 font-mono">
						{#each arcaneTrivyDbImages(formInputs.trivyDbRegistry.value) as image (image)}
							<li>{image}</li>
						{/each}
					</ul>
				{/snippet}
			</SettingsRow>

			<SettingsRow
				for="trivyIgnoreUnfixedSwitch"
				label={m.security_trivy_ignore_unfixed_label()}
				description={m.security_trivy_ignore_unfixed_description()}
				layout="switch"
			>
				<Switch id="trivyIgnoreUnfixedSwitch" bind:checked={formInputs.trivyIgnoreUnfixed.value} />
			</SettingsRow>

			<SettingsRow
				for="vulnerabilityThreatIntelEnabledSwitch"
				label={m.security_threat_intel_enabled_label()}
				description={m.security_threat_intel_enabled_description()}
				layout="switch"
			>
				<Switch id="vulnerabilityThreatIntelEnabledSwitch" bind:checked={formInputs.vulnerabilityThreatIntelEnabled.value} />
			</SettingsRow>

			<SettingsRow label={m.security_trivy_config_label()} description={m.security_trivy_config_description()} layout="wide">
				<div class="overflow-hidden rounded-md border border-border/60">
					<CodeEditor
						bind:value={formInputs.trivyConfig.value}
						language="yaml"
						validationMode="none"
						autoHeight={true}
						statusBar={false}
						fontSize="13px"
						placeholder={m.security_trivy_config_placeholder()}
					/>
				</div>
			</SettingsRow>

			<SettingsRow label={m.security_trivy_ignore_label()} description={m.security_trivy_ignore_description()} layout="wide">
				<div class="overflow-hidden rounded-md border border-border/60">
					<CodeEditor
						bind:value={formInputs.trivyIgnore.value}
						language="plaintext"
						validationMode="none"
						autoHeight={true}
						statusBar={false}
						fontSize="13px"
						placeholder={m.security_trivy_ignore_placeholder()}
					/>
				</div>
			</SettingsRow>

			<SettingsRow
				for="trivyNetwork"
				label={m.security_trivy_network_label()}
				description={m.security_trivy_network_description()}
				helpText={m.security_trivy_network_help()}
				error={formInputs.trivyNetwork.error}
			>
				<SearchableSelect
					triggerId="trivyNetwork"
					items={trivyNetworkOptions.map((option) => ({
						value: option.value,
						label: option.label,
						hint: option.description
					}))}
					bind:value={formInputs.trivyNetwork.value}
					onSelect={(value) => (formInputs.trivyNetwork.value = value)}
					placeholder={false}
					class="w-full justify-between"
				/>
			</SettingsRow>

			<SettingsRow
				label={m.security_trivy_security_opts_label()}
				description={m.security_trivy_security_opts_description()}
				helpText={m.security_trivy_security_opts_help()}
				error={formInputs.trivySecurityOpts.error}
				layout="wide"
			>
				<Textarea
					bind:value={formInputs.trivySecurityOpts.value}
					aria-label={m.security_trivy_security_opts_label()}
					mono
					class="min-h-28"
					placeholder={m.security_trivy_security_opts_placeholder()}
					rows={4}
				/>
			</SettingsRow>

			<SettingsRow
				for="trivyPrivilegedSwitch"
				label={m.security_trivy_privileged_label()}
				description={m.security_trivy_privileged_description()}
				warningText={formInputs.trivyPrivileged.value ? m.security_trivy_privileged_note() : undefined}
				layout="switch"
			>
				<Switch id="trivyPrivilegedSwitch" bind:checked={formInputs.trivyPrivileged.value} />
			</SettingsRow>
		</SettingsSection>

		<SettingsSection title={m.security_trivy_server_section()}>
			<SettingsRow
				for="trivyServerEnabledSwitch"
				label={m.security_trivy_server_enabled_label()}
				description={m.security_trivy_server_enabled_description()}
				layout="switch"
			>
				<Switch id="trivyServerEnabledSwitch" bind:checked={formInputs.trivyServerEnabled.value} />
			</SettingsRow>
			{#if formInputs.trivyServerEnabled.value}
				<SettingsRow
					for="trivyServerUrl"
					label={m.security_trivy_server_url_label()}
					description={m.security_trivy_server_url_description()}
					error={formInputs.trivyServerUrl.error}
				>
					<Input
						id="trivyServerUrl"
						type="url"
						bind:value={formInputs.trivyServerUrl.value}
						placeholder={m.security_trivy_server_url_placeholder()}
						aria-invalid={!!formInputs.trivyServerUrl.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="trivyServerToken"
					label={m.security_trivy_server_token_label()}
					description={m.security_trivy_server_token_description()}
					error={formInputs.trivyServerToken.error}
					helpText={m.security_trivy_server_note()}
				>
					<Input
						id="trivyServerToken"
						type="password"
						autocomplete="off"
						bind:value={formInputs.trivyServerToken.value}
						aria-invalid={!!formInputs.trivyServerToken.error}
					/>
				</SettingsRow>
			{/if}
		</SettingsSection>

		<SettingsSection title={m.security_trivy_resource_section()}>
			<SettingsRow
				for="trivyResourceLimitsEnabledSwitch"
				label={m.security_trivy_resource_limits_label()}
				description={m.security_trivy_resource_limits_description()}
				layout="switch"
			>
				<Switch
					id="trivyResourceLimitsEnabledSwitch"
					bind:checked={formInputs.trivyResourceLimitsEnabled.value}
					onCheckedChange={handleTrivyResourceLimitsChange}
				/>
			</SettingsRow>
			{#if formInputs.trivyResourceLimitsEnabled.value}
				<SettingsRow
					for="trivyCpuLimit"
					label={m.security_trivy_cpu_limit_label()}
					description={m.security_trivy_cpu_limit_help()}
					error={formInputs.trivyCpuLimit.error}
				>
					<Input
						id="trivyCpuLimit"
						type="number"
						min={0}
						step="any"
						bind:value={formInputs.trivyCpuLimit.value}
						aria-invalid={!!formInputs.trivyCpuLimit.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="trivyMemoryLimitMb"
					label={m.security_trivy_memory_limit_label()}
					error={formInputs.trivyMemoryLimitMb.error}
				>
					<Input
						id="trivyMemoryLimitMb"
						type="number"
						bind:value={formInputs.trivyMemoryLimitMb.value}
						aria-invalid={!!formInputs.trivyMemoryLimitMb.error}
					/>
				</SettingsRow>
				<SettingsRow
					for="trivyConcurrentScanContainers"
					label={m.security_trivy_concurrent_scan_containers_label()}
					description={m.security_trivy_concurrent_scan_containers_help()}
					error={formInputs.trivyConcurrentScanContainers.error}
				>
					<Input
						id="trivyConcurrentScanContainers"
						type="number"
						bind:value={formInputs.trivyConcurrentScanContainers.value}
						aria-invalid={!!formInputs.trivyConcurrentScanContainers.error}
					/>
				</SettingsRow>
			{/if}
		</SettingsSection>
	</div>
{:else}
	<FeatureDisabled environmentId={targetEnvironmentId} />
{/if}
