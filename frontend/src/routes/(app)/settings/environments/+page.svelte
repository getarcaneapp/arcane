<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { z } from 'zod/v4';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { toast } from 'svelte-sonner';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { SettingsPageLayout } from '$lib/layouts';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service.js';
	import { settingsService } from '$lib/services/settings-service';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import GeneralTab from './components/GeneralTab.svelte';
	import DockerTab from './components/DockerTab.svelte';
	import JobsTab from './components/JobsTab.svelte';
	import AgentTab from './components/AgentTab.svelte';
	import TrivySecuritySettings from '$lib/components/settings/trivy-security-settings.svelte';
	import {
		AlertIcon,
		ApiKeyIcon,
		DockerBrandIcon,
		EnvironmentsIcon,
		ExternalLinkIcon,
		GitBranchIcon,
		JobsIcon,
		SecurityIcon,
		SettingsIcon
	} from '$lib/icons';
	import type { Environment, EnvironmentStatus } from '$lib/types/environment.type';
	import { isEnvironmentOnline, resolveEnvironmentStatus } from '$lib/utils/environment-status';

	let { data } = $props();

	let selectedEnvironmentId = $derived(data.selectedEnvironmentId as string | null);
	let environment = $derived(data.environment as Environment | null);
	let settings = $derived(data.settings);
	let environments = $derived(data.environments?.data ?? []);
	let currentEnvironment = $derived(environmentStore.selected);
	let isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled ?? false);

	let activeTab = $state('general');
	let hasNormalizedUrl = $state(false);
	let refreshedEnvironment: Environment | null = $state(null);
	let runtimeEnvironment: Environment | null = $derived.by(() => {
		if (!environment) return null;
		const refreshed = refreshedEnvironment;
		return refreshed && refreshed.id === environment.id ? refreshed : environment;
	});

	const tabItems = $derived.by((): TabItem[] => {
		if (!environment) return [];

		const items: TabItem[] = [
			{
				value: 'general',
				label: m.general_title(),
				icon: SettingsIcon
			},
			{
				value: 'docker',
				label: m.environments_docker_settings_title(),
				icon: DockerBrandIcon
			},
			{
				value: 'security',
				label: m.security_title(),
				icon: SecurityIcon
			},
			{
				value: 'jobs',
				label: m.jobs_title(),
				icon: JobsIcon
			}
		];

		if (environment.id !== '0') {
			items.push({
				value: 'agent',
				label: m.environments_agent_config_title(),
				icon: ApiKeyIcon
			});
		}

		items.push({
			value: 'gitops',
			label: m.git_syncs_title(),
			icon: GitBranchIcon
		});

		return items;
	});

	const tabValues = $derived(new Set(tabItems.map((tab) => tab.value)));

	const formSchema = z.object({
		name: z.string().min(1),
		enabled: z.boolean(),
		apiUrl: z.string(),
		pollingEnabled: z.boolean(),
		autoUpdate: z.boolean(),
		autoInjectEnv: z.boolean(),
		followProjectSymlinks: z.boolean(),
		dockerPruneMode: z.enum(['all', 'dangling']),
		defaultDeployPullPolicy: z.enum(['missing', 'always', 'never']),
		defaultShell: z.string(),
		projectsDirectory: z.string(),
		swarmStackSourcesDirectory: z.string(),
		diskUsagePath: z.string(),
		maxImageUploadSize: z.coerce.number(),
		gitSyncMaxFiles: z.coerce.number().int().nonnegative(),
		gitSyncMaxTotalSizeMb: z.coerce.number().int().nonnegative(),
		gitSyncMaxBinarySizeMb: z.coerce.number().int().nonnegative(),
		baseServerUrl: z.string(),
		scheduledPruneEnabled: z.boolean(),
		scheduledPruneContainers: z.boolean(),
		scheduledPruneImages: z.boolean(),
		scheduledPruneVolumes: z.boolean(),
		scheduledPruneNetworks: z.boolean(),
		scheduledPruneBuildCache: z.boolean(),
		vulnerabilityScanEnabled: z.boolean(),
		trivyImage: z.string(),
		trivyNetwork: z.string(),
		trivySecurityOpts: z.string(),
		trivyPrivileged: z.boolean(),
		trivyPreserveCacheOnVolumePrune: z.boolean(),
		trivyResourceLimitsEnabled: z.boolean(),
		trivyCpuLimit: z.coerce.number().int(m.security_session_timeout_integer()).nonnegative(),
		trivyMemoryLimitMb: z.coerce.number().int().nonnegative(),
		trivyConcurrentScanContainers: z.coerce.number().int().min(1, m.security_trivy_concurrent_scan_containers_min()),
		autoUpdateExcludedContainers: z.string().optional(),
		autoHealEnabled: z.boolean(),
		autoHealExcludedContainers: z.string(),
		autoHealMaxRestarts: z.coerce.number().int().min(1),
		autoHealRestartWindow: z.coerce.number().int().min(1)
	});

	const currentSettings = $derived.by(() => ({
		name: environment?.name ?? '',
		enabled: environment?.enabled ?? false,
		apiUrl: environment?.apiUrl ?? '',
		pollingEnabled: settings?.pollingEnabled ?? false,
		autoUpdate: settings?.autoUpdate ?? false,
		autoInjectEnv: settings?.autoInjectEnv ?? false,
		followProjectSymlinks: settings?.followProjectSymlinks ?? false,
		dockerPruneMode: (settings?.dockerPruneMode as 'all' | 'dangling') || 'dangling',
		defaultDeployPullPolicy: (settings?.defaultDeployPullPolicy as 'missing' | 'always' | 'never') || 'missing',
		defaultShell: settings?.defaultShell || '/bin/sh',
		projectsDirectory: settings?.projectsDirectory || '/app/data/projects',
		swarmStackSourcesDirectory: settings?.swarmStackSourcesDirectory || '/app/data/swarm/sources',
		diskUsagePath: settings?.diskUsagePath || '/app/data/projects',
		maxImageUploadSize: settings?.maxImageUploadSize || 500,
		gitSyncMaxFiles: settings?.gitSyncMaxFiles ?? 500,
		gitSyncMaxTotalSizeMb: settings?.gitSyncMaxTotalSizeMb ?? 50,
		gitSyncMaxBinarySizeMb: settings?.gitSyncMaxBinarySizeMb ?? 10,
		baseServerUrl: settings?.baseServerUrl || 'http://localhost',
		scheduledPruneEnabled: settings?.scheduledPruneEnabled ?? false,
		scheduledPruneContainers: settings?.scheduledPruneContainers ?? true,
		scheduledPruneImages: settings?.scheduledPruneImages ?? true,
		scheduledPruneVolumes: settings?.scheduledPruneVolumes ?? false,
		scheduledPruneNetworks: settings?.scheduledPruneNetworks ?? true,
		scheduledPruneBuildCache: settings?.scheduledPruneBuildCache ?? false,
		vulnerabilityScanEnabled: settings?.vulnerabilityScanEnabled ?? false,
		trivyImage: settings?.trivyImage || '',
		trivyNetwork: settings?.trivyNetwork || '',
		trivySecurityOpts: settings?.trivySecurityOpts || '',
		trivyPrivileged: settings?.trivyPrivileged ?? false,
		trivyPreserveCacheOnVolumePrune: settings?.trivyPreserveCacheOnVolumePrune ?? true,
		trivyResourceLimitsEnabled: settings?.trivyResourceLimitsEnabled ?? true,
		trivyCpuLimit: settings?.trivyCpuLimit ?? 1,
		trivyMemoryLimitMb: settings?.trivyMemoryLimitMb ?? 0,
		trivyConcurrentScanContainers: settings?.trivyConcurrentScanContainers ?? 1,
		autoUpdateExcludedContainers: settings?.autoUpdateExcludedContainers || '',
		autoHealEnabled: settings?.autoHealEnabled ?? false,
		autoHealExcludedContainers: settings?.autoHealExcludedContainers || '',
		autoHealMaxRestarts: settings?.autoHealMaxRestarts ?? 5,
		autoHealRestartWindow: settings?.autoHealRestartWindow ?? 30
	}));

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);
	let statusOverride = $state<EnvironmentStatus | null>(null);
	let currentStatus = $derived(resolveEnvironmentStatus(runtimeEnvironment, statusOverride));
	let isCurrentlyOnline = $derived(isEnvironmentOnline(runtimeEnvironment, statusOverride));
	let isCurrentlyStandby = $derived(currentStatus === 'standby');

	async function saveEnvironmentSettings(formData: z.infer<typeof formSchema>) {
		if (!environment) return;

		await environmentManagementService.update(environment.id, {
			name: formData.name,
			enabled: formData.enabled,
			apiUrl: formData.apiUrl
		});

		if (settings) {
			await settingsService.updateSettingsForEnvironment(environment.id, {
				pollingEnabled: formData.pollingEnabled,
				autoUpdate: formData.autoUpdate,
				autoInjectEnv: formData.autoInjectEnv,
				followProjectSymlinks: formData.followProjectSymlinks,
				dockerPruneMode: formData.dockerPruneMode,
				defaultDeployPullPolicy: formData.defaultDeployPullPolicy,
				defaultShell: formData.defaultShell,
				projectsDirectory: formData.projectsDirectory,
				swarmStackSourcesDirectory: formData.swarmStackSourcesDirectory,
				diskUsagePath: formData.diskUsagePath,
				maxImageUploadSize: formData.maxImageUploadSize,
				gitSyncMaxFiles: formData.gitSyncMaxFiles,
				gitSyncMaxTotalSizeMb: formData.gitSyncMaxTotalSizeMb,
				gitSyncMaxBinarySizeMb: formData.gitSyncMaxBinarySizeMb,
				baseServerUrl: formData.baseServerUrl,
				scheduledPruneEnabled: formData.scheduledPruneEnabled,
				scheduledPruneContainers: formData.scheduledPruneContainers,
				scheduledPruneImages: formData.scheduledPruneImages,
				scheduledPruneVolumes: formData.scheduledPruneVolumes,
				scheduledPruneNetworks: formData.scheduledPruneNetworks,
				scheduledPruneBuildCache: formData.scheduledPruneBuildCache,
				vulnerabilityScanEnabled: formData.vulnerabilityScanEnabled,
				trivyImage: formData.trivyImage,
				trivyNetwork: formData.trivyNetwork,
				trivySecurityOpts: formData.trivySecurityOpts,
				trivyPrivileged: formData.trivyPrivileged,
				trivyPreserveCacheOnVolumePrune: formData.trivyPreserveCacheOnVolumePrune,
				trivyResourceLimitsEnabled: formData.trivyResourceLimitsEnabled,
				trivyCpuLimit: formData.trivyResourceLimitsEnabled ? formData.trivyCpuLimit : 0,
				trivyMemoryLimitMb: formData.trivyResourceLimitsEnabled ? formData.trivyMemoryLimitMb : 0,
				trivyConcurrentScanContainers: formData.trivyConcurrentScanContainers,
				autoUpdateExcludedContainers: formData.autoUpdateExcludedContainers,
				autoHealEnabled: formData.autoHealEnabled,
				autoHealExcludedContainers: formData.autoHealExcludedContainers,
				autoHealMaxRestarts: formData.autoHealMaxRestarts,
				autoHealRestartWindow: formData.autoHealRestartWindow
			});
		}

		await refreshEnvironment();

		if (currentEnvironment?.id === environment.id) {
			await environmentStore.initialize(
				(
					await environmentManagementService.getEnvironments({
						pagination: { page: 1, limit: 1000 }
					})
				).data
			);
		}
	}

	let { formInputs, settingsForm, resetForm, onSubmit, registerOnMount } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			getCurrentSettings: () => currentSettings,
			onSave: saveEnvironmentSettings,
			successMessage: m.common_update_success({ resource: m.resource_environment_cap() }),
			errorMessage: m.common_update_failed({ resource: m.resource_environment() }),
			onReset: () => toast.info(m.environments_changes_reset())
		})
	);

	const pruneModeOptions = [
		{ value: 'all', label: m.docker_prune_all(), description: m.docker_prune_all_description() },
		{ value: 'dangling', label: m.docker_prune_dangling(), description: m.docker_prune_dangling_description() }
	];

	let pruneModeDescription = $derived(
		pruneModeOptions.find((option) => option.value === $formInputs.dockerPruneMode.value)?.description ??
			m.docker_prune_mode_description()
	);

	const shellOptions = [
		{ value: '/bin/sh', label: '/bin/sh', description: m.docker_shell_sh_description() },
		{ value: '/bin/bash', label: '/bin/bash', description: m.docker_shell_bash_description() },
		{ value: '/bin/ash', label: '/bin/ash', description: m.docker_shell_ash_description() },
		{ value: '/bin/zsh', label: '/bin/zsh', description: m.docker_shell_zsh_description() }
	];

	let shellSelectValue = $derived.by((): string => {
		const currentShell = $formInputs.defaultShell.value;
		return shellOptions.find((option) => option.value === currentShell)?.value ?? 'custom';
	});

	function handleShellSelectChange(value: string) {
		if (value !== 'custom') {
			$formInputs.defaultShell.value = value;
		}
	}

	function buildSettingsUrl(environmentId: string, tab: string = activeTab): string {
		const url = new URL(page.url);
		url.pathname = '/settings/environments';
		url.searchParams.set('environment', environmentId);

		if (!tab || tab === 'general') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', tab);
		}

		return url.toString();
	}

	function getNormalizedTab(tab: string | null): string {
		if (!tab || tab === 'details') return 'general';
		if (tab === 'gitops') return 'gitops';
		if (tab === 'agent' && environment?.id === '0') return 'general';
		return tabValues.has(tab) ? tab : 'general';
	}

	$effect(() => {
		if (!environment || hasNormalizedUrl) return;

		const queryEnvironmentId = page.url.searchParams.get('environment');
		const preferredEnvironmentId =
			queryEnvironmentId ??
			environments.find((item: Environment) => item.id === environmentStore.selected?.id)?.id ??
			environment.id;
		const normalizedTab = getNormalizedTab(page.url.searchParams.get('tab'));

		if (normalizedTab === 'gitops') {
			hasNormalizedUrl = true;
			goto(`/environments/${preferredEnvironmentId}/gitops`, { replaceState: true });
			return;
		}

		activeTab = normalizedTab;

		if (
			queryEnvironmentId !== preferredEnvironmentId ||
			page.url.searchParams.get('tab') !== (normalizedTab === 'general' ? null : normalizedTab)
		) {
			hasNormalizedUrl = true;
			goto(buildSettingsUrl(preferredEnvironmentId, normalizedTab), {
				replaceState: true,
				keepFocus: true,
				noScroll: true
			});
			return;
		}

		hasNormalizedUrl = true;
	});

	$effect(() => {
		const tabFromUrl = getNormalizedTab(page.url.searchParams.get('tab'));
		if (tabFromUrl !== 'gitops' && tabFromUrl !== activeTab) {
			activeTab = tabFromUrl;
		}
	});

	onMount(() => {
		registerOnMount();

		if (environment?.isEdge) {
			void refreshRuntimeEnvironment();
		}

		const interval = window.setInterval(() => {
			if (!environment?.isEdge) return;
			void refreshRuntimeEnvironment();
		}, 5000);

		return () => window.clearInterval(interval);
	});

	async function refreshRuntimeEnvironment() {
		if (!environment) return;

		try {
			const latestEnvironment = await environmentManagementService.get(environment.id);
			if (latestEnvironment.id === environment.id) {
				refreshedEnvironment = latestEnvironment;
			}
		} catch (error) {
			console.debug('Failed to refresh environment runtime state:', error);
		}
	}

	async function refreshEnvironment() {
		if (isRefreshing) return;

		try {
			isRefreshing = true;
			statusOverride = null;
			await invalidateAll();
		} catch (error) {
			console.error('Failed to refresh environment:', error);
			toast.error(m.common_refresh_failed({ resource: m.resource_environment() }));
		} finally {
			isRefreshing = false;
		}
	}

	async function testConnection() {
		if (!environment || isTestingConnection) return;

		try {
			isTestingConnection = true;
			const customUrl = $formInputs.apiUrl.value !== environment.apiUrl ? $formInputs.apiUrl.value : undefined;
			const result = await environmentManagementService.testConnection(environment.id, customUrl);

			const nextStatus = result.status as EnvironmentStatus;
			statusOverride = customUrl && !environment.isEdge ? nextStatus : null;

			if (result.status === 'online') {
				toast.success(m.environments_test_connection_success());
			} else {
				toast.error(m.environments_test_connection_error());
			}

			if (!customUrl) {
				await invalidateAll();
			}
		} catch (error) {
			statusOverride = environment.isEdge ? null : 'offline';
			toast.error(m.environments_test_connection_failed());
			console.error(error);
		} finally {
			isTestingConnection = false;
		}
	}

	async function handleRegenerateApiKey() {
		if (!environment) return;

		try {
			isRegeneratingKey = true;

			const result = await environmentManagementService.update(environment.id, {
				regenerateApiKey: true
			});

			if (result.apiKey) {
				regeneratedApiKey = result.apiKey;
				toast.success(m.environments_regenerate_key_success());
				await invalidateAll();
			} else {
				toast.error(m.environments_regenerate_key_failed());
			}
		} catch (error) {
			console.error('Failed to regenerate API key:', error);
			toast.error(m.environments_regenerate_key_failed());
		} finally {
			isRegeneratingKey = false;
			showRegenerateDialog = false;
		}
	}

	function handleEnvironmentChange(nextEnvironmentId: string) {
		const nextTab = nextEnvironmentId === '0' && activeTab === 'agent' ? 'general' : activeTab;
		goto(buildSettingsUrl(nextEnvironmentId, nextTab), {
			keepFocus: true,
			noScroll: true
		});
	}

	function handleTabChange(value: string) {
		if (!environment) return;
		if (value === 'gitops') {
			goto(`/environments/${environment.id}/gitops`);
			return;
		}

		activeTab = value;
		goto(buildSettingsUrl(environment.id, value), {
			replaceState: true,
			keepFocus: true,
			noScroll: true
		});
	}

	const environmentOptions = $derived.by(() =>
		environments.map((item: Environment) => ({
			label: item.name,
			value: item.id,
			description: item.apiUrl
		}))
	);
</script>

<SettingsPageLayout
	title={m.environments_title()}
	description={m.environments_page_subtitle()}
	icon={EnvironmentsIcon}
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		{#if !environment}
			<div class="rounded-lg border p-6 text-center">
				<h2 class="text-lg font-semibold">{m.environments_title()}</h2>
				<p class="text-muted-foreground mt-2 text-sm">{m.common_no_results_found()}</p>
			</div>
		{:else}
			<div class="space-y-6">
				<div class="space-y-2 rounded-xl border p-4">
					<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-end">
						<div>
							<SelectWithLabel
								id="settings-environment-selector"
								label={m.environments_title()}
								value={selectedEnvironmentId ?? environment.id}
								options={environmentOptions}
								onValueChange={handleEnvironmentChange}
							/>
						</div>
						<div class="flex flex-wrap items-center gap-2">
							<ArcaneButton
								action="base"
								tone="outline"
								icon={ExternalLinkIcon}
								customLabel={m.common_view_details()}
								onclick={() => goto(`/environments/${environment.id}`)}
							/>
							<ArcaneButton
								action="base"
								tone="outline"
								icon={GitBranchIcon}
								customLabel={m.git_syncs_title()}
								onclick={() => goto(`/environments/${environment.id}/gitops`)}
							/>
						</div>
					</div>
					<p class="text-muted-foreground text-[0.8rem]">{m.sidebar_select_environment()}</p>
				</div>

				{#if environment.enabled && settings && isCurrentlyStandby}
					<div
						class="flex items-start gap-3 rounded-lg border border-blue-500/30 bg-blue-500/10 p-4 text-blue-900 dark:text-blue-200"
					>
						<AlertIcon class="mt-0.5 size-5 shrink-0 text-blue-600 dark:text-blue-400" />
						<div class="flex-1 space-y-1">
							<p class="text-sm font-medium">{m.common_status()}: {m.common_standby()}</p>
						</div>
					</div>
				{:else if !environment.enabled || !isCurrentlyOnline || !settings}
					<div
						class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-amber-900 dark:text-amber-200"
					>
						<AlertIcon class="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
						<div class="flex-1 space-y-1">
							<p class="text-sm font-medium">
								{#if !environment.enabled}
									{m.environments_warning_disabled()}
								{:else if !isCurrentlyOnline}
									{m.common_status()}: {currentStatus === 'pending'
										? m.common_pending()
										: currentStatus === 'error'
											? m.common_error()
											: m.common_offline()}
								{:else if !settings}
									{m.environments_warning_no_settings()}
								{/if}
							</p>
						</div>
					</div>
				{/if}

				<Tabs.Root bind:value={activeTab} class="w-full">
					<div class="my-4">
						<TabBar items={tabItems} value={activeTab} onValueChange={handleTabChange} class="w-full" />
					</div>

					<Tabs.Content value="general">
						<GeneralTab
							{formInputs}
							{environment}
							{isTestingConnection}
							{testConnection}
							showSettingsSections={Boolean(settings)}
						/>
					</Tabs.Content>

					{#if settings}
						<Tabs.Content value="docker">
							<DockerTab
								{formInputs}
								{shellSelectValue}
								{handleShellSelectChange}
								{shellOptions}
								{pruneModeDescription}
								{pruneModeOptions}
							/>
						</Tabs.Content>

						<Tabs.Content value="security">
							<TrivySecuritySettings {formInputs} environmentId={environment.id} />
						</Tabs.Content>

						<Tabs.Content value="jobs">
							<JobsTab {formInputs} environmentId={environment.id} />
						</Tabs.Content>
					{/if}

					{#if environment.id !== '0'}
						<Tabs.Content value="agent">
							<AgentTab bind:regeneratedApiKey {isRegeneratingKey} bind:showRegenerateDialog />
						</Tabs.Content>
					{/if}

					<Tabs.Content value="gitops" />
				</Tabs.Root>
			</div>
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<AlertDialog.Root bind:open={showRegenerateDialog}>
			<AlertDialog.Content>
				<AlertDialog.Header>
					<AlertDialog.Title>{m.environments_regenerate_dialog_title()}</AlertDialog.Title>
					<AlertDialog.Description>{m.environments_regenerate_dialog_message()}</AlertDialog.Description>
				</AlertDialog.Header>
				<AlertDialog.Footer>
					<AlertDialog.Cancel>{m.common_cancel()}</AlertDialog.Cancel>
					<AlertDialog.Action onclick={handleRegenerateApiKey}>
						{m.environments_regenerate_api_key()}
					</AlertDialog.Action>
				</AlertDialog.Footer>
			</AlertDialog.Content>
		</AlertDialog.Root>
	{/snippet}
</SettingsPageLayout>
