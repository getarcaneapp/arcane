<script lang="ts">
	import { z } from 'zod/v4';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service.js';
	import { settingsService } from '$lib/services/settings-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import MobileFloatingFormActions from '$lib/components/form/mobile-floating-form-actions.svelte';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import DetailsTab from './components/DetailsTab.svelte';
	import GeneralTab from './components/GeneralTab.svelte';
	import DockerTab from './components/DockerTab.svelte';
	import JobsTab from './components/JobsTab.svelte';
	import AgentTab from './components/AgentTab.svelte';
	import {
		ArrowLeftIcon,
		EnvironmentsIcon,
		AlertIcon,
		RefreshIcon,
		ApiKeyIcon,
		DockerBrandIcon,
		SettingsIcon,
		GitBranchIcon,
		JobsIcon
	} from '$lib/icons';

	let { data } = $props();
	let { environment, settings, versionInformation } = $derived(data);

	let currentEnvironment = $derived(environmentStore.selected);

	let activeTab = $state('details');

	const tabItems: TabItem[] = [
		{
			value: 'details',
			label: m.environments_overview_title(),
			icon: EnvironmentsIcon
		},
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
			value: 'jobs',
			label: m.jobs_title(),
			icon: JobsIcon
		},
		{
			value: 'agent',
			label: m.environments_agent_config_title(),
			icon: ApiKeyIcon
		},
		{
			value: 'gitops',
			label: m.git_syncs_title(),
			icon: GitBranchIcon
		}
	];

	const tabValues = new Set(tabItems.map((tab) => tab.value));

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

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSyncing = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);

	// Version state
	let remoteVersion = $state<AppVersionInformation | null>(null);
	let isLoadingVersion = $state(false);

	// Track current status separately from environment data
	let currentStatus = $state<'online' | 'offline' | 'error' | 'pending'>('offline');

	// Initialize status from environment
	$effect(() => {
		currentStatus = environment.status;
	});

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
		dockerPruneMode: z.enum(['all', 'dangling']),
		defaultShell: z.string(),
		projectsDirectory: z.string(),
		diskUsagePath: z.string(),
		maxImageUploadSize: z.coerce.number(),
		baseServerUrl: z.string(),
		scheduledPruneEnabled: z.boolean(),
		scheduledPruneContainers: z.boolean(),
		scheduledPruneImages: z.boolean(),
		scheduledPruneVolumes: z.boolean(),
		scheduledPruneNetworks: z.boolean(),
		scheduledPruneBuildCache: z.boolean()
	});

	// Build current settings object from environment and settings data
	const currentSettings = $derived({
		name: environment.name,
		enabled: environment.enabled,
		apiUrl: environment.apiUrl,
		pollingEnabled: settings?.pollingEnabled ?? false,
		autoUpdate: settings?.autoUpdate ?? false,
		autoInjectEnv: settings?.autoInjectEnv ?? false,
		dockerPruneMode: (settings?.dockerPruneMode as 'all' | 'dangling') || 'dangling',
		defaultShell: settings?.defaultShell || '/bin/sh',
		projectsDirectory: settings?.projectsDirectory || '/app/data/projects',
		diskUsagePath: settings?.diskUsagePath || '/app/data/projects',
		maxImageUploadSize: settings?.maxImageUploadSize || 500,
		baseServerUrl: settings?.baseServerUrl || 'http://localhost',
		scheduledPruneEnabled: settings?.scheduledPruneEnabled ?? false,
		scheduledPruneContainers: settings?.scheduledPruneContainers ?? true,
		scheduledPruneImages: settings?.scheduledPruneImages ?? true,
		scheduledPruneVolumes: settings?.scheduledPruneVolumes ?? false,
		scheduledPruneNetworks: settings?.scheduledPruneNetworks ?? true,
		scheduledPruneBuildCache: settings?.scheduledPruneBuildCache ?? false
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
				dockerPruneMode: formData.dockerPruneMode,
				defaultShell: formData.defaultShell,
				projectsDirectory: formData.projectsDirectory,
				diskUsagePath: formData.diskUsagePath,
				maxImageUploadSize: formData.maxImageUploadSize,
				baseServerUrl: formData.baseServerUrl,
				scheduledPruneEnabled: formData.scheduledPruneEnabled,
				scheduledPruneContainers: formData.scheduledPruneContainers,
				scheduledPruneImages: formData.scheduledPruneImages,
				scheduledPruneVolumes: formData.scheduledPruneVolumes,
				scheduledPruneNetworks: formData.scheduledPruneNetworks,
				scheduledPruneBuildCache: formData.scheduledPruneBuildCache
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
		if (environment.id !== '0' && currentStatus === 'online' && !remoteVersion && !isLoadingVersion) {
			fetchVersion();
		}
	});

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
			await invalidateAll();
			currentStatus = environment.status;
			// Reset version to trigger re-fetch if online
			remoteVersion = null;
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

			// Update current status based on test result
			currentStatus = result.status;

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
			// Update status to offline on error
			currentStatus = 'offline';
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

			<div class="flex flex-wrap items-center gap-2">
				<div class="hidden items-center gap-2 sm:flex">
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

				{#if environment.id !== '0'}
					<ArcaneButton
						action="base"
						tone="outline"
						onclick={syncEnvironment}
						disabled={isSyncing}
						loading={isSyncing}
						icon={RefreshIcon}
						customLabel={m.sync_environment()}
					/>
				{/if}

				<ArcaneButton
					action="refresh"
					tone="outline"
					onclick={refreshEnvironment}
					disabled={isRefreshing}
					loading={isRefreshing}
				/>
			</div>
		</div>

		{#if !environment.enabled || currentStatus === 'offline' || !settings}
			<div
				class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-amber-900 dark:text-amber-200"
			>
				<AlertIcon class="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
				<div class="flex-1 space-y-1">
					<p class="text-sm font-medium">
						{#if !environment.enabled}
							{m.environments_warning_disabled()}
						{:else if currentStatus === 'offline'}
							{m.environments_warning_offline()}
						{:else if !settings}
							{m.environments_warning_no_settings()}
						{/if}
					</p>
				</div>
			</div>
		{/if}
	</div>

	<Tabs.Root bind:value={activeTab} class="w-full">
		<div class="my-4">
			<TabBar items={tabItems} value={activeTab} onValueChange={handleTabChange} class="w-full" />
		</div>

		<Tabs.Content value="details">
			<DetailsTab
				{environment}
				{formInputs}
				{currentStatus}
				{isLoadingVersion}
				{remoteVersion}
				{versionInformation}
				{isTestingConnection}
				{testConnection}
			/>
		</Tabs.Content>

		{#if settings}
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
