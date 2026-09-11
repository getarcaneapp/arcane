<script lang="ts">
	import { onMount } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { SecurityIcon, InfoIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { toast } from 'svelte-sonner';
	import { networkService } from '#lib/services/network-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Settings } from '#lib/types/settings.js';

	import SectionCard from '#lib/components/section-card.svelte';
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
	const networksQuery = createQuery(() => {
		const requestedEnvironmentId = targetEnvironmentId;
		userStore.current;
		return {
			queryKey: queryKeys.networks.list(requestedEnvironmentId ?? '', networkRequest),
			queryFn: async () => {
				await environmentStore.ready;
				return networkService.getNetworksForEnvironment(requestedEnvironmentId!, networkRequest);
			},
			enabled: !!requestedEnvironmentId && hasPermission('networks:read', requestedEnvironmentId)
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

<SectionCard
	variant="transparent"
	title={m.security_vulnerability_scanning_heading()}
	icon={SecurityIcon}
	class="flex flex-col"
	contentClass="divide-y divide-border/40 lg:p-6 lg:pt-0 [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0"
>
	<div class="max-w-xl">
		<SelectWithLabel
			id="trivyDbRegistry"
			name="trivyDbRegistry"
			bind:value={formInputs.trivyDbRegistry.value}
			label={m.trivy_db_registry_label()}
			description={m.trivy_db_registry_description()}
			options={arcaneImageRegistryOptions()}
			onValueChange={(v) => (formInputs.trivyDbRegistry.value = v as 'ghcr.io' | 'docker.io')}
		/>
		<ul class="mt-2 space-y-0.5 font-mono text-xs text-muted-foreground">
			{#each arcaneTrivyDbImages(formInputs.trivyDbRegistry.value) as image (image)}
				<li>{image}</li>
			{/each}
		</ul>
	</div>

	<SettingsRow
		label={m.security_trivy_ignore_unfixed_label()}
		description={m.security_trivy_ignore_unfixed_description()}
		layout="inline"
	>
		<Switch id="trivyIgnoreUnfixedSwitch" bind:checked={formInputs.trivyIgnoreUnfixed.value} />
	</SettingsRow>

	<SettingsRow
		label={m.security_trivy_network_label()}
		description={m.security_trivy_network_description()}
		helpText={m.security_trivy_network_help()}
		contentClass="max-w-xs"
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
		{#if formInputs.trivyNetwork.error}
			<p class="mt-2 text-sm text-destructive">{formInputs.trivyNetwork.error}</p>
		{/if}
	</SettingsRow>

	<div class="space-y-4">
		<SettingsRow
			label={m.security_trivy_server_enabled_label()}
			description={m.security_trivy_server_enabled_description()}
			layout="inline"
		>
			<Switch id="trivyServerEnabledSwitch" bind:checked={formInputs.trivyServerEnabled.value} />
		</SettingsRow>
		{#if formInputs.trivyServerEnabled.value}
			<div class="space-y-4 border-l-2 border-border/60 pl-5">
				<TextInputWithLabel
					bind:value={formInputs.trivyServerUrl.value}
					error={formInputs.trivyServerUrl.error}
					label={m.security_trivy_server_url_label()}
					description={m.security_trivy_server_url_description()}
					placeholder={m.security_trivy_server_url_placeholder()}
					type="url"
				/>
				<TextInputWithLabel
					bind:value={formInputs.trivyServerToken.value}
					error={formInputs.trivyServerToken.error}
					label={m.security_trivy_server_token_label()}
					description={m.security_trivy_server_token_description()}
					type="password"
				/>
				<Alert.Root variant="default" class="border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950">
					<InfoIcon class="h-4 w-4 text-amber-900 dark:text-amber-100" />
					<Alert.Description class="text-amber-800 dark:text-amber-200">
						{m.security_trivy_server_note()}
					</Alert.Description>
				</Alert.Root>
			</div>
		{/if}
	</div>

	<SettingsRow
		label={m.security_trivy_security_opts_label()}
		description={m.security_trivy_security_opts_description()}
		helpText={m.security_trivy_security_opts_help()}
	>
		<Textarea
			bind:value={formInputs.trivySecurityOpts.value}
			aria-label={m.security_trivy_security_opts_label()}
			class="min-h-28 font-mono text-sm"
			placeholder={m.security_trivy_security_opts_placeholder()}
			rows={4}
		/>
		{#if formInputs.trivySecurityOpts.error}
			<p class="mt-2 text-sm text-destructive">{formInputs.trivySecurityOpts.error}</p>
		{/if}
	</SettingsRow>

	<SettingsRow
		label={m.security_trivy_privileged_label()}
		description={m.security_trivy_privileged_description()}
		layout="inline"
	>
		<Switch id="trivyPrivilegedSwitch" bind:checked={formInputs.trivyPrivileged.value} />
	</SettingsRow>
	{#if formInputs.trivyPrivileged.value}
		<Alert.Root variant="default" class="border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950">
			<InfoIcon class="h-4 w-4 text-amber-900 dark:text-amber-100" />
			<Alert.Description class="text-amber-800 dark:text-amber-200">
				{m.security_trivy_privileged_note()}
			</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="space-y-4">
		<SettingsRow
			label={m.security_trivy_resource_limits_label()}
			description={m.security_trivy_resource_limits_description()}
			layout="inline"
		>
			<Switch
				id="trivyResourceLimitsEnabledSwitch"
				bind:checked={formInputs.trivyResourceLimitsEnabled.value}
				onCheckedChange={handleTrivyResourceLimitsChange}
			/>
		</SettingsRow>
		{#if formInputs.trivyResourceLimitsEnabled.value}
			<div class="space-y-4 border-l-2 border-border/60 pl-5">
				<div class="grid gap-4 sm:grid-cols-2">
					<TextInputWithLabel
						bind:value={formInputs.trivyCpuLimit.value}
						error={formInputs.trivyCpuLimit.error}
						disabled={!formInputs.trivyResourceLimitsEnabled.value}
						label={m.security_trivy_cpu_limit_label()}
						helpText={m.security_trivy_cpu_limit_help()}
						type="number"
					/>
					<TextInputWithLabel
						bind:value={formInputs.trivyMemoryLimitMb.value}
						error={formInputs.trivyMemoryLimitMb.error}
						disabled={!formInputs.trivyResourceLimitsEnabled.value}
						label={m.security_trivy_memory_limit_label()}
						reserveHelpTextSpace={true}
						type="number"
					/>
				</div>
				<div class="max-w-xs">
					<TextInputWithLabel
						bind:value={formInputs.trivyConcurrentScanContainers.value}
						error={formInputs.trivyConcurrentScanContainers.error}
						label={m.security_trivy_concurrent_scan_containers_label()}
						helpText={m.security_trivy_concurrent_scan_containers_help()}
						type="number"
					/>
				</div>
			</div>
		{/if}
	</div>
</SectionCard>
