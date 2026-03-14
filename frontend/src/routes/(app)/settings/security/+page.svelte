<script lang="ts">
	import { z } from 'zod/v4';
	import { getContext, onMount } from 'svelte';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { toast } from 'svelte-sonner';
	import type { PageData } from './$types';
	import type { Settings } from '$lib/types/settings.type';
	import { m } from '$lib/paraglide/messages';
	import { SecurityIcon, InfoIcon } from '$lib/icons';
	import SearchableSelect from '$lib/components/form/searchable-select.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import * as Alert from '$lib/components/ui/alert';
	import { networkService } from '$lib/services/network-service';

	let { data }: { data: PageData } = $props();
	const currentSettings = $derived<Settings>($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);

	const formSchema = z.object({
		trivyImage: z.string(),
		trivyNetwork: z.string(),
		trivySecurityOpts: z.string(),
		trivyPrivileged: z.boolean(),
		trivyResourceLimitsEnabled: z.boolean(),
		trivyCpuLimit: z.coerce.number().int(m.security_session_timeout_integer()).nonnegative(),
		trivyMemoryLimitMb: z.coerce.number().int().nonnegative(),
		trivyConcurrentScanContainers: z.coerce.number().int().min(1, m.security_trivy_concurrent_scan_containers_min())
	});

	const formDefaults = $derived({
		trivyImage: currentSettings.trivyImage,
		trivyNetwork: currentSettings.trivyNetwork || 'bridge',
		trivySecurityOpts: currentSettings.trivySecurityOpts || '',
		trivyPrivileged: currentSettings.trivyPrivileged ?? false,
		trivyResourceLimitsEnabled: currentSettings.trivyResourceLimitsEnabled ?? true,
		trivyCpuLimit: currentSettings.trivyCpuLimit ?? 1,
		trivyMemoryLimitMb: currentSettings.trivyMemoryLimitMb ?? 0,
		trivyConcurrentScanContainers: currentSettings.trivyConcurrentScanContainers ?? 1
	});

	let { formInputs, form, settingsForm } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings: formDefaults,
			getCurrentSettings: () => ({
				trivyImage: ($settingsStore || data.settings!).trivyImage,
				trivyNetwork: ($settingsStore || data.settings!).trivyNetwork || 'bridge',
				trivySecurityOpts: ($settingsStore || data.settings!).trivySecurityOpts || '',
				trivyPrivileged: ($settingsStore || data.settings!).trivyPrivileged ?? false,
				trivyResourceLimitsEnabled: ($settingsStore || data.settings!).trivyResourceLimitsEnabled ?? true,
				trivyCpuLimit: ($settingsStore || data.settings!).trivyCpuLimit ?? 1,
				trivyMemoryLimitMb: ($settingsStore || data.settings!).trivyMemoryLimitMb ?? 0,
				trivyConcurrentScanContainers: ($settingsStore || data.settings!).trivyConcurrentScanContainers ?? 1
			}),
			successMessage: m.security_settings_saved()
		})
	);

	const hasSecurityChanges = $derived(
		$formInputs.trivyImage.value !== currentSettings.trivyImage ||
			$formInputs.trivyNetwork.value !== (currentSettings.trivyNetwork || 'bridge') ||
			$formInputs.trivySecurityOpts.value !== (currentSettings.trivySecurityOpts || '') ||
			$formInputs.trivyPrivileged.value !== (currentSettings.trivyPrivileged ?? false) ||
			$formInputs.trivyResourceLimitsEnabled.value !== (currentSettings.trivyResourceLimitsEnabled ?? true) ||
			$formInputs.trivyCpuLimit.value !== (currentSettings.trivyCpuLimit ?? 1) ||
			$formInputs.trivyMemoryLimitMb.value !== (currentSettings.trivyMemoryLimitMb ?? 0) ||
			$formInputs.trivyConcurrentScanContainers.value !== (currentSettings.trivyConcurrentScanContainers ?? 1)
	);

	const baseTrivyNetworkOptions = [
		{ value: 'bridge', label: 'bridge' },
		{ value: 'host', label: 'host' },
		{ value: 'none', label: 'none' }
	];
	let customTrivyNetworkOptions = $state<{ value: string; label: string; description?: string }[]>([]);

	const trivyNetworkOptions = $derived.by(() => {
		const options = new Map<string, { value: string; label: string; description?: string }>();
		for (const option of baseTrivyNetworkOptions) {
			options.set(option.value, option);
		}
		for (const option of customTrivyNetworkOptions) {
			options.set(option.value, option);
		}

		const selectedNetwork = ($formInputs.trivyNetwork.value || '').trim();
		if (selectedNetwork && !options.has(selectedNetwork)) {
			options.set(selectedNetwork, {
				value: selectedNetwork,
				label: selectedNetwork,
				description: m.security_trivy_network_current_value_note()
			});
		}

		return [...options.values()];
	});

	async function loadTrivyNetworkOptions() {
		try {
			const response = await networkService.getNetworks({
				pagination: {
					page: 1,
					limit: 1000
				},
				sort: {
					column: 'name',
					direction: 'asc'
				}
			});

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

	async function customSubmit() {
		const formData = form.validate();
		if (!formData) {
			toast.error(m.security_form_validation_error());
			return;
		}

		const trivyCpuLimit = formData.trivyResourceLimitsEnabled ? formData.trivyCpuLimit : 0;
		const trivyMemoryLimitMb = formData.trivyResourceLimitsEnabled ? formData.trivyMemoryLimitMb : 0;

		settingsForm.setLoading(true);

		try {
			await settingsForm.updateSettings({
				trivyImage: formData.trivyImage,
				trivyNetwork: formData.trivyNetwork,
				trivySecurityOpts: formData.trivySecurityOpts,
				trivyPrivileged: formData.trivyPrivileged,
				trivyResourceLimitsEnabled: formData.trivyResourceLimitsEnabled,
				trivyCpuLimit,
				trivyMemoryLimitMb,
				trivyConcurrentScanContainers: formData.trivyConcurrentScanContainers
			});
			toast.success(m.security_settings_saved());
		} catch (error: any) {
			console.error('Failed to save settings:', error);
			toast.error(m.security_settings_save_failed());
		} finally {
			settingsForm.setLoading(false);
		}
	}

	function customReset() {
		form.reset();
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

	$effect(() => {
		settingsForm.registerFormActions(customSubmit, customReset);
		const formState = getContext('settingsFormState') as any;
		if (formState) {
			formState.hasChanges = hasSecurityChanges;
		}
	});
</script>

<SettingsPageLayout
	title={m.security_title()}
	description={m.security_description()}
	icon={SecurityIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.security_vulnerability_scanning_heading()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
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
									label={m.security_trivy_image_label()}
									placeholder="ghcr.io/aquasecurity/trivy:latest"
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
					</div>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
