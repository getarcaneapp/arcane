<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import * as Alert from '$lib/components/ui/alert';
	import SearchableSelect from '$lib/components/form/searchable-select.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import { SecurityIcon, InfoIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { networkService } from '$lib/services/network-service';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Readable } from 'svelte/store';

	type TrivySecurityFormValues = {
		trivyImage: string;
		trivyNetwork: string;
		trivySecurityOpts: string;
		trivyPrivileged: boolean;
		trivyPreserveCacheOnVolumePrune: boolean;
		trivyResourceLimitsEnabled: boolean;
		trivyCpuLimit: number;
		trivyMemoryLimitMb: number;
		trivyConcurrentScanContainers: number;
	};

	type FormField<T> = {
		value: T;
		error: string | null;
	};

	type TrivySecurityFormInputs = Readable<
		Record<string, FormField<unknown>> & {
			[K in keyof TrivySecurityFormValues]: FormField<TrivySecurityFormValues[K]>;
		}
	>;

	let {
		formInputs,
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
			label: m.security_trivy_network_auto_label(),
			description: m.security_trivy_network_auto_description()
		},
		{ value: 'bridge', label: 'bridge' },
		{ value: 'host', label: 'host' },
		{ value: 'none', label: 'none' }
	];

	let customTrivyNetworkOptions = $state<TrivyNetworkOption[]>([]);

	const trivyNetworkOptions = $derived.by(() => {
		const options = [...baseTrivyNetworkOptions];

		for (const option of customTrivyNetworkOptions) {
			if (!options.some((existing) => existing.value === option.value)) {
				options.push(option);
			}
		}

		const selectedNetwork = ($formInputs.trivyNetwork.value || '').trim();
		if (selectedNetwork && !options.some((option) => option.value === selectedNetwork)) {
			options.push({
				value: selectedNetwork,
				label: selectedNetwork,
				description: m.security_trivy_network_current_value_note()
			});
		}

		return options;
	});

	async function fetchTrivyNetworkOptions(targetEnvironmentId: string | undefined): Promise<TrivyNetworkOption[]> {
		const request: SearchPaginationSortRequest = {
			pagination: {
				page: 1,
				limit: 1000
			},
			sort: {
				column: 'name',
				direction: 'asc'
			}
		};
		const response = targetEnvironmentId
			? await networkService.getNetworksForEnvironment(targetEnvironmentId, request)
			: await networkService.getNetworks(request);

		const networkNames = [
			...new Set(
				response.data
					.map((network) => network.name)
					.filter((name) => !!name && !baseTrivyNetworkOptions.some((option) => option.value === name))
			)
		].sort((a, b) => a.localeCompare(b));

		return networkNames.map((name) => ({
			value: name,
			label: name
		}));
	}

	function handleTrivyResourceLimitsChange(checked: boolean) {
		$formInputs.trivyResourceLimitsEnabled.value = checked;
		if (!checked) {
			$formInputs.trivyCpuLimit.value = 0;
			$formInputs.trivyMemoryLimitMb.value = 0;
		}
	}

	$effect(() => {
		const targetEnvironmentId = environmentId;
		let cancelled = false;

		void fetchTrivyNetworkOptions(targetEnvironmentId)
			.then((options) => {
				if (!cancelled) {
					customTrivyNetworkOptions = options;
				}
			})
			.catch((error) => {
				if (!cancelled) {
					console.warn('Failed to load Trivy network options:', error);
					toast.info(m.security_trivy_network_fetch_failed());
				}
			});

		return () => {
			cancelled = true;
		};
	});
</script>

<Card.Root class="flex flex-col">
	<Card.Header icon={SecurityIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.security_vulnerability_scanning_heading()}</h2>
			</Card.Title>
		</div>
	</Card.Header>
	<Card.Content class="space-y-6 lg:p-6 lg:pt-0">
		<SettingsRow
			label={m.security_trivy_image_label()}
			description={m.security_trivy_image_description()}
			helpText={m.security_trivy_image_note()}
			contentClass="max-w-xs"
		>
			<TextInputWithLabel
				bind:value={$formInputs.trivyImage.value}
				error={$formInputs.trivyImage.error}
				disabled={true}
				label={m.security_trivy_image_label()}
				placeholder="ghcr.io/getarcaneapp/tools:latest"
				type="text"
			/>
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
				bind:value={$formInputs.trivyNetwork.value}
				onSelect={(value) => ($formInputs.trivyNetwork.value = value)}
				placeholder={false}
				class="w-full justify-between"
			/>
			{#if $formInputs.trivyNetwork.error}
				<p class="text-destructive mt-2 text-sm">{$formInputs.trivyNetwork.error}</p>
			{/if}
		</SettingsRow>

		<SettingsRow
			label={m.security_trivy_security_opts_label()}
			description={m.security_trivy_security_opts_description()}
			helpText={m.security_trivy_security_opts_help()}
			contentClass="space-y-2"
		>
			<Textarea
				bind:value={$formInputs.trivySecurityOpts.value}
				aria-label={m.security_trivy_security_opts_label()}
				class="min-h-28 font-mono text-sm"
				placeholder={m.security_trivy_security_opts_placeholder()}
				rows={4}
			/>
			{#if $formInputs.trivySecurityOpts.error}
				<p class="text-destructive text-sm">{$formInputs.trivySecurityOpts.error}</p>
			{/if}
		</SettingsRow>

		<SettingsRow
			label={m.security_trivy_privileged_label()}
			description={m.security_trivy_privileged_description()}
			helpText={m.security_trivy_privileged_note()}
			contentClass="space-y-3"
		>
			<div class="flex items-center gap-2">
				<Switch id="trivyPrivilegedSwitch" bind:checked={$formInputs.trivyPrivileged.value} />
				<Label for="trivyPrivilegedSwitch" class="font-normal">
					{$formInputs.trivyPrivileged.value ? m.common_enabled() : m.common_disabled()}
				</Label>
			</div>
			{#if $formInputs.trivyPrivileged.value}
				<Alert.Root variant="default" class="border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950">
					<InfoIcon class="h-4 w-4 text-amber-900 dark:text-amber-100" />
					<Alert.Description class="text-amber-800 dark:text-amber-200">
						{m.security_trivy_privileged_note()}
					</Alert.Description>
				</Alert.Root>
			{/if}
		</SettingsRow>

		<SettingsRow
			label={m.security_trivy_preserve_cache_on_volume_prune_label()}
			description={m.security_trivy_preserve_cache_on_volume_prune_description()}
			helpText={m.security_trivy_preserve_cache_on_volume_prune_note()}
			contentClass="space-y-3"
		>
			<div class="flex items-center gap-2">
				<Switch id="trivyPreserveCacheOnVolumePruneSwitch" bind:checked={$formInputs.trivyPreserveCacheOnVolumePrune.value} />
				<Label for="trivyPreserveCacheOnVolumePruneSwitch" class="font-normal">
					{$formInputs.trivyPreserveCacheOnVolumePrune.value ? m.common_enabled() : m.common_disabled()}
				</Label>
			</div>
		</SettingsRow>

		<SettingsRow
			label={m.security_trivy_resource_limits_label()}
			description={m.security_trivy_resource_limits_description()}
			helpText={m.security_trivy_resource_limits_note()}
			contentClass="space-y-4"
		>
			<div class="flex items-center gap-2">
				<Switch
					id="trivyResourceLimitsEnabledSwitch"
					bind:checked={$formInputs.trivyResourceLimitsEnabled.value}
					onCheckedChange={handleTrivyResourceLimitsChange}
				/>
				<Label for="trivyResourceLimitsEnabledSwitch" class="font-normal">
					{$formInputs.trivyResourceLimitsEnabled.value ? m.common_enabled() : m.common_disabled()}
				</Label>
			</div>
			<div class="grid gap-4 sm:grid-cols-2">
				<TextInputWithLabel
					bind:value={$formInputs.trivyCpuLimit.value}
					error={$formInputs.trivyCpuLimit.error}
					disabled={!$formInputs.trivyResourceLimitsEnabled.value}
					label={m.security_trivy_cpu_limit_label()}
					helpText={m.security_trivy_cpu_limit_help()}
					type="number"
				/>
				<TextInputWithLabel
					bind:value={$formInputs.trivyMemoryLimitMb.value}
					error={$formInputs.trivyMemoryLimitMb.error}
					disabled={!$formInputs.trivyResourceLimitsEnabled.value}
					label={m.security_trivy_memory_limit_label()}
					reserveHelpTextSpace={true}
					type="number"
				/>
			</div>
			<div class="max-w-xs pt-2">
				<TextInputWithLabel
					bind:value={$formInputs.trivyConcurrentScanContainers.value}
					error={$formInputs.trivyConcurrentScanContainers.error}
					label={m.security_trivy_concurrent_scan_containers_label()}
					helpText={m.security_trivy_concurrent_scan_containers_help()}
					type="number"
				/>
			</div>
		</SettingsRow>
	</Card.Content>
</Card.Root>
