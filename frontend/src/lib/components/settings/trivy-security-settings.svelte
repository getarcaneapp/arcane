<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import * as Alert from '$lib/components/ui/alert';
	import SearchableSelect from '$lib/components/form/searchable-select.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
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

	async function loadTrivyNetworkOptions() {
		try {
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
			const response = environmentId
				? await networkService.getNetworksForEnvironment(environmentId, request)
				: await networkService.getNetworks(request);

			const networkNames = [
				...new Set(
					response.data
						.map((network) => network.name)
						.filter((name) => !!name && !baseTrivyNetworkOptions.some((option) => option.value === name))
				)
			].sort((a, b) => a.localeCompare(b));

			customTrivyNetworkOptions = networkNames.map((name) => ({
				value: name,
				label: name
			}));
		} catch (error) {
			console.warn('Failed to load Trivy network options:', error);
			toast.info(m.security_trivy_network_fetch_failed());
		}
	}

	function handleTrivyResourceLimitsChange(checked: boolean) {
		$formInputs.trivyResourceLimitsEnabled.value = checked;
		if (!checked) {
			$formInputs.trivyCpuLimit.value = 0;
			$formInputs.trivyMemoryLimitMb.value = 0;
		}
	}

	onMount(() => {
		void loadTrivyNetworkOptions();
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
	<Card.Content class="space-y-6 p-4">
		<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
			<div>
				<Label class="text-base">{m.security_trivy_image_label()}</Label>
				<p class="text-muted-foreground mt-1 text-sm">{m.security_trivy_image_description()}</p>
				<p class="text-muted-foreground mt-2 text-xs">{m.security_trivy_image_note()}</p>
			</div>
			<div class="max-w-xs">
				<TextInputWithLabel
					bind:value={$formInputs.trivyImage.value}
					error={$formInputs.trivyImage.error}
					disabled={true}
					label={m.security_trivy_image_label()}
					placeholder="ghcr.io/aquasecurity/trivy@sha256:bcc376de8d77cfe086a917230e818dc9f8528e3c852f7b1aff648949b6258d1c"
					type="text"
				/>
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
			<div>
				<Label class="text-base">{m.security_trivy_network_label()}</Label>
				<p class="text-muted-foreground mt-1 text-sm">{m.security_trivy_network_description()}</p>
				<p class="text-muted-foreground mt-2 text-xs">{m.security_trivy_network_help()}</p>
			</div>
			<div class="max-w-xs">
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
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
			<div>
				<Label class="text-base">{m.security_trivy_security_opts_label()}</Label>
				<p class="text-muted-foreground mt-1 text-sm">{m.security_trivy_security_opts_description()}</p>
				<p class="text-muted-foreground mt-2 text-xs">{m.security_trivy_security_opts_help()}</p>
			</div>
			<div class="space-y-2">
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
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
			<div>
				<Label class="text-base">{m.security_trivy_privileged_label()}</Label>
				<p class="text-muted-foreground mt-1 text-sm">{m.security_trivy_privileged_description()}</p>
				<p class="text-muted-foreground mt-2 text-xs">{m.security_trivy_privileged_note()}</p>
			</div>
			<div class="space-y-3">
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
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
			<div>
				<Label class="text-base">{m.security_trivy_preserve_cache_on_volume_prune_label()}</Label>
				<p class="text-muted-foreground mt-1 text-sm">{m.security_trivy_preserve_cache_on_volume_prune_description()}</p>
				<p class="text-muted-foreground mt-2 text-xs">{m.security_trivy_preserve_cache_on_volume_prune_note()}</p>
			</div>
			<div class="space-y-3">
				<div class="flex items-center gap-2">
					<Switch id="trivyPreserveCacheOnVolumePruneSwitch" bind:checked={$formInputs.trivyPreserveCacheOnVolumePrune.value} />
					<Label for="trivyPreserveCacheOnVolumePruneSwitch" class="font-normal">
						{$formInputs.trivyPreserveCacheOnVolumePrune.value ? m.common_enabled() : m.common_disabled()}
					</Label>
				</div>
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
			<div>
				<Label class="text-base">{m.security_trivy_resource_limits_label()}</Label>
				<p class="text-muted-foreground mt-1 text-sm">{m.security_trivy_resource_limits_description()}</p>
				<p class="text-muted-foreground mt-2 text-xs">{m.security_trivy_resource_limits_note()}</p>
			</div>
			<div class="space-y-4">
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
			</div>
		</div>
	</Card.Content>
</Card.Root>
