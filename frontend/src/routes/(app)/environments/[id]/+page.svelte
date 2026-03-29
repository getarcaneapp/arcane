<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import { ActionButtonGroup, type ActionButton } from '$lib/components/action-button-group/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service.js';
	import { settingsService } from '$lib/services/settings-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import type { Environment, EnvironmentStatus } from '$lib/types/environment.type';
	import { isEnvironmentOnline, resolveEnvironmentStatus } from '$lib/utils/environment-status';
	import MobileFloatingFormActions from '$lib/components/form/mobile-floating-form-actions.svelte';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import DetailsTab from './components/DetailsTab.svelte';
	import GeneralTab from './components/GeneralTab.svelte';
	import DockerTab from './components/DockerTab.svelte';
	import JobsTab from './components/JobsTab.svelte';
	import TrivySecuritySettings from '$lib/components/settings/trivy-security-settings.svelte';
	import {
		ArrowLeftIcon,
		EnvironmentsIcon,
		AlertIcon,
		DownloadIcon,
		RefreshIcon,
		DockerBrandIcon,
		SecurityIcon,
		SettingsIcon,
		GitBranchIcon,
		JobsIcon,
		ResetIcon
	} from '$lib/icons';

	let { data } = $props();
	let { environment, settings, versionInformation } = $derived(data);
	let refreshedEnvironment: Environment | null = $state(null);
	let runtimeEnvironment: Environment = $derived.by(() => {
		const refreshed = refreshedEnvironment;
		return refreshed && refreshed.id === environment.id ? refreshed : environment;
	});

	let currentEnvironment = $derived(environmentStore.selected);

	let activeTab = $state('details');

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSyncing = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);

	// Version state
	let remoteVersion = $state<AppVersionInformation | null>(null);
	let isLoadingVersion = $state(false);

	// Only non-edge custom URL tests should temporarily override the displayed status.
	let statusOverride = $state<EnvironmentStatus | null>(null);
	let currentStatus = $derived(resolveEnvironmentStatus(runtimeEnvironment, statusOverride));
	let isCurrentlyOnline = $derived(isEnvironmentOnline(runtimeEnvironment, statusOverride));
	let isCurrentlyStandby = $derived(currentStatus === 'standby');
	let showSettingsTabs = $derived(runtimeEnvironment.enabled && isCurrentlyOnline && settings !== null);
	let hasMTLSAssets = $derived(Boolean(runtimeEnvironment.edgeMTLSCertificate));
	let showMTLSDownloads = $derived(
		runtimeEnvironment.id !== '0' &&
			runtimeEnvironment.isEdge &&
			(runtimeEnvironment.edgeSecurityMode === 'mtls' || hasMTLSAssets)
	);
	let mtlsBundleDownloadHref = $derived(`/api/environments/${runtimeEnvironment.id}/deployment/mtls/bundle`);
	let mtlsCertificateDownloadHref = $derived(`/api/environments/${runtimeEnvironment.id}/deployment/mtls/agent.crt`);
	let mtlsKeyDownloadHref = $derived(`/api/environments/${runtimeEnvironment.id}/deployment/mtls/agent.key`);
	let headerActions = $derived.by((): ActionButton[] => {
		const actions: ActionButton[] = [];

		if (environment.id !== '0') {
			actions.push({
				id: 'sync',
				action: 'base',
				label: m.sync_environment(),
				onclick: syncEnvironment,
				disabled: isSyncing,
				loading: isSyncing,
				icon: RefreshIcon
			});

			if (showMTLSDownloads) {
				actions.push({
					id: 'mtls-downloads',
					action: 'base',
					label: m.environments_agent_mtls_download_bundle(),
					href: mtlsBundleDownloadHref,
					rel: 'external',
					icon: DownloadIcon,
					menuItems: [
						{
							id: 'mtls-certificate',
							label: m.environments_agent_mtls_download_certificate(),
							href: mtlsCertificateDownloadHref
						},
						{
							id: 'mtls-key',
							label: m.environments_agent_mtls_download_key(),
							href: mtlsKeyDownloadHref
						}
					]
				});
			}

			actions.push({
				id: 'regenerate-api-key',
				action: 'base',
				label: m.environments_regenerate_api_key(),
				onclick: () => {
					showRegenerateDialog = true;
				},
				disabled: isRegeneratingKey,
				loading: isRegeneratingKey,
				icon: ResetIcon
			});
		}

		actions.push({
			id: 'refresh',
			action: 'refresh',
			label: m.common_refresh(),
			onclick: refreshEnvironment,
			disabled: isRefreshing,
			loading: isRefreshing
		});

		return actions;
	});

	const tabItems = $derived.by((): TabItem[] => {
		const items: TabItem[] = [
			{
				value: 'details',
				label: m.environments_overview_title(),
				icon: EnvironmentsIcon
			}
		];

		if (showSettingsTabs) {
			items.push(
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
			);
		}

		items.push({
			value: 'gitops',
			label: m.git_syncs_title(),
			icon: GitBranchIcon
		});

		return items;
	});

	const tabValues = $derived(new Set(tabItems.map((tab) => tab.value)));

	$effect(() => {
		if (!tabValues.has(activeTab)) {
			activeTab = 'details';
		}
	});

	$effect(() => {
		const tabFromUrl = page.url.searchParams.get('tab');
		if (!tabFromUrl || !tabValues.has(tabFromUrl) || tabFromUrl === activeTab) {
			return;
		}
		if (tabFromUrl === 'gitops') {
			goto(`/environments/${environment.id}/gitops`);
			return;
		}
		activeTab = tabFromUrl;
	});

	function handleTabChange(value: string) {
		if (value === 'gitops') {
			goto(`/environments/${environment.id}/gitops`);
			return;
		}
		activeTab = value;
	}

	// Form schema combining environment info and settings
	const formSchema = z.object({
		// Environment basic info
		name: z.string().min(1),
		enabled: z.boolean(),
		apiUrl: z.string(),
		// Settings
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

	// Build current settings object from environment and settings data
	const currentSettings = $derived({
		name: environment.name,
		enabled: environment.enabled,
		apiUrl: environment.apiUrl,
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
	});

	// Custom save handler for environment-specific settings
	async function saveEnvironmentSettings(formData: z.infer<typeof formSchema>) {
		// Update environment basic info
		await environmentManagementService.update(environment.id, {
			name: formData.name,
			enabled: formData.enabled,
			apiUrl: formData.apiUrl
		});

		// Update environment settings if they exist
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

		// Update environment store if this is the current environment
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
		pruneModeOptions.find((o) => o.value === $formInputs.dockerPruneMode.value)?.description ?? m.docker_prune_mode_description()
	);

	const shellOptions = [
		{ value: '/bin/sh', label: '/bin/sh', description: m.docker_shell_sh_description() },
		{ value: '/bin/bash', label: '/bin/bash', description: m.docker_shell_bash_description() },
		{ value: '/bin/ash', label: '/bin/ash', description: m.docker_shell_ash_description() },
		{ value: '/bin/zsh', label: '/bin/zsh', description: m.docker_shell_zsh_description() }
	];

	let shellSelectValue = $derived.by((): string => {
		if (!settings) return 'custom';
		return shellOptions.find((o) => o.value === settings.defaultShell)?.value ?? 'custom';
	});

	function handleShellSelectChange(value: string) {
		if (value !== 'custom') {
			$formInputs.defaultShell.value = value;
		}
	}

	// Fetch version when environment is online
	$effect(() => {
		if (environment.id !== '0' && isCurrentlyOnline && !remoteVersion && !isLoadingVersion) {
			fetchVersion();
		}
	});

	onMount(() => {
		if (environment.isEdge) {
			void refreshRuntimeEnvironment();
		}

		const interval = window.setInterval(() => {
			if (!environment.isEdge) return;
			void refreshRuntimeEnvironment();
		}, 5000);

		return () => window.clearInterval(interval);
	});

	async function refreshRuntimeEnvironment() {
		try {
			const latestEnvironment = await environmentManagementService.get(environment.id);
			if (latestEnvironment.id === environment.id) {
				refreshedEnvironment = latestEnvironment;
			}
		} catch (error) {
			console.debug('Failed to refresh environment runtime state:', error);
		}
	}

	async function fetchVersion() {
		try {
			isLoadingVersion = true;
			remoteVersion = await environmentManagementService.getVersion(environment.id);
		} catch (err) {
			console.error('Failed to fetch environment version:', err);
		} finally {
			isLoadingVersion = false;
		}
	}

	async function refreshEnvironment() {
		if (isRefreshing) return;
		try {
			isRefreshing = true;
			statusOverride = null;
			remoteVersion = null;
			await invalidateAll();
		} catch (err) {
			console.error('Failed to refresh environment:', err);
			toast.error(m.common_refresh_failed({ resource: m.resource_environment() }));
		} finally {
			isRefreshing = false;
		}
	}

	async function syncEnvironment() {
		if (isSyncing) return;
		try {
			isSyncing = true;
			await environmentManagementService.sync(environment.id);
			toast.success(m.sync_environment_success());
		} catch (error) {
			console.error('Failed to sync environment:', error);
			toast.error(m.sync_environment_failed());
		} finally {
			isSyncing = false;
		}
	}

	async function testConnection() {
		if (isTestingConnection) return;
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

			// If testing with saved URL (not custom), refresh to get backend's updated status
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
		try {
			isRegeneratingKey = true;

			// Delete the old API key and create a new one
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
</script>

<div class="container mx-auto max-w-full space-y-6 overflow-hidden p-2 sm:p-6">
	<div class="space-y-3 sm:space-y-4">
		<ArcaneButton
			action="base"
			tone="ghost"
			onclick={() => goto('/environments')}
			class="w-fit gap-2"
			icon={ArrowLeftIcon}
			customLabel={m.common_back_to({ resource: m.environments_title() })}
		/>

		<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex-1">
				<h1 class="text-xl font-bold wrap-break-word sm:text-2xl">{environment.name}</h1>
				<p class="text-muted-foreground mt-1.5 text-sm wrap-break-word sm:text-base">{m.environments_page_subtitle()}</p>
			</div>

			<div class="flex min-w-0 flex-1 flex-col items-start gap-3 sm:items-end">
				<div class="hidden flex-wrap items-center gap-2 self-start sm:flex sm:self-end">
					{#if settingsForm.hasChanges}
						<span class="text-xs text-orange-600 dark:text-orange-400">{m.environments_unsaved_changes()}</span>
					{:else}
						<span class="text-xs text-green-600 dark:text-green-400">{m.environments_all_changes_saved()}</span>
					{/if}

					{#if settingsForm.hasChanges}
						<ArcaneButton
							action="restart"
							tone="outline"
							onclick={resetForm}
							disabled={settingsForm.isLoading}
							customLabel={m.common_reset()}
						/>
					{/if}

					<ArcaneButton
						action="save"
						onclick={onSubmit}
						disabled={!settingsForm.hasChanges || settingsForm.isLoading}
						loading={settingsForm.isLoading}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
					/>
				</div>
			</div>
		</div>

		<ActionButtonGroup buttons={headerActions} class="justify-end" />

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
	</div>

	{#if regeneratedApiKey}
		<div class="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-4 text-emerald-950 dark:text-emerald-100">
			<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
				<div class="space-y-2">
					<p class="text-sm font-medium">{m.environments_new_api_key()}</p>
					<div class="flex items-center gap-2">
						<code class="bg-background/70 flex-1 rounded-md px-3 py-2 font-mono text-sm break-all">
							{regeneratedApiKey}
						</code>
						<CopyButton text={regeneratedApiKey} size="icon" class="size-8 shrink-0" />
					</div>
					<p class="text-sm">{m.environments_api_key_save_warning()}</p>
				</div>

				<ArcaneButton
					action="base"
					tone="outline"
					onclick={() => (regeneratedApiKey = null)}
					customLabel={m.common_dismiss()}
					class="shrink-0"
				/>
			</div>
		</div>
	{/if}

	<Tabs.Root bind:value={activeTab} class="w-full">
		<div class="my-4">
			<TabBar items={tabItems} value={activeTab} onValueChange={handleTabChange} class="w-full" />
		</div>

		<Tabs.Content value="details">
			<DetailsTab
				environment={runtimeEnvironment}
				{formInputs}
				{currentStatus}
				{isLoadingVersion}
				{remoteVersion}
				{versionInformation}
				{isTestingConnection}
				{testConnection}
			/>
		</Tabs.Content>

		{#if showSettingsTabs}
			<Tabs.Content value="general">
				<GeneralTab {formInputs} />
			</Tabs.Content>

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

		<Tabs.Content value="gitops" />
	</Tabs.Root>

	<AlertDialog.Root bind:open={showRegenerateDialog}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>{m.environments_regenerate_dialog_title()}</AlertDialog.Title>
				<AlertDialog.Description>
					{m.environments_regenerate_dialog_message()}
				</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel>{m.common_cancel()}</AlertDialog.Cancel>
				<AlertDialog.Action onclick={handleRegenerateApiKey}>
					{m.environments_regenerate_api_key()}
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
</div>

<MobileFloatingFormActions
	hasChanges={settingsForm.hasChanges}
	isLoading={settingsForm.isLoading}
	onSave={onSubmit}
	onReset={resetForm}
/>
